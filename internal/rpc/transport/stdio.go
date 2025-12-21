package transport

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
)

// StdioMode defines how messages are framed over stdio.
type StdioMode int

const (
	// StdioModeNewline uses newline-delimited JSON (simple, compact).
	StdioModeNewline StdioMode = iota

	// StdioModeLSP uses Content-Length headers like LSP protocol.
	// Format: Content-Length: 123\r\n\r\n{"jsonrpc":"2.0",...}
	StdioModeLSP
)

// StdioTransport implements Transport over stdin/stdout.
// This is used for VS Code extension integration and MCP compatibility.
type StdioTransport struct {
	id     string
	reader *bufio.Reader
	writer io.Writer
	mode   StdioMode

	done   chan struct{}
	mu     sync.Mutex
	closed bool
}

// StdioOption configures a StdioTransport.
type StdioOption func(*StdioTransport)

// WithStdioMode sets the message framing mode.
func WithStdioMode(mode StdioMode) StdioOption {
	return func(t *StdioTransport) {
		t.mode = mode
	}
}

// WithStdioID sets a custom ID for the transport.
func WithStdioID(id string) StdioOption {
	return func(t *StdioTransport) {
		t.id = id
	}
}

// NewStdioTransport creates a new stdio transport using os.Stdin and os.Stdout.
func NewStdioTransport(opts ...StdioOption) *StdioTransport {
	return NewStdioTransportWithIO(os.Stdin, os.Stdout, opts...)
}

// NewStdioTransportWithIO creates a new stdio transport with custom reader/writer.
// This is useful for testing.
func NewStdioTransportWithIO(r io.Reader, w io.Writer, opts ...StdioOption) *StdioTransport {
	t := &StdioTransport{
		id:     "stdio",
		reader: bufio.NewReader(r),
		writer: w,
		mode:   StdioModeNewline,
		done:   make(chan struct{}),
	}

	for _, opt := range opts {
		opt(t)
	}

	return t
}

// ID returns the unique identifier for this transport.
func (t *StdioTransport) ID() string {
	return t.id
}

// Read reads the next message from stdin.
func (t *StdioTransport) Read(ctx context.Context) ([]byte, error) {
	// Check if already closed
	select {
	case <-t.done:
		return nil, ErrTransportClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	switch t.mode {
	case StdioModeLSP:
		return t.readLSP()
	default:
		return t.readNewline()
	}
}

// readNewline reads a newline-delimited JSON message.
func (t *StdioTransport) readNewline() ([]byte, error) {
	line, err := t.reader.ReadBytes('\n')
	if err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, err
	}

	// Trim newline and optional carriage return
	line = trimCRLF(line)
	if len(line) == 0 {
		// Skip empty lines
		return t.readNewline()
	}

	return line, nil
}

// readLSP reads an LSP-style message with Content-Length header.
func (t *StdioTransport) readLSP() ([]byte, error) {
	var contentLength int

	// Read headers
	for {
		line, err := t.reader.ReadString('\n')
		if err != nil {
			return nil, err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			// Empty line marks end of headers
			break
		}

		if strings.HasPrefix(line, "Content-Length:") {
			lengthStr := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			contentLength, err = strconv.Atoi(lengthStr)
			if err != nil {
				return nil, fmt.Errorf("invalid Content-Length: %w", err)
			}
		}
		// Ignore other headers (Content-Type, etc.)
	}

	if contentLength == 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	// Read body
	body := make([]byte, contentLength)
	_, err := io.ReadFull(t.reader, body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

// Write sends a message through stdout.
func (t *StdioTransport) Write(ctx context.Context, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return ErrTransportClosed
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	switch t.mode {
	case StdioModeLSP:
		return t.writeLSP(data)
	default:
		return t.writeNewline(data)
	}
}

// writeNewline writes a newline-delimited JSON message.
func (t *StdioTransport) writeNewline(data []byte) error {
	// Ensure data doesn't contain newlines (would corrupt framing)
	// JSON shouldn't have unescaped newlines, but be safe
	_, err := t.writer.Write(append(data, '\n'))
	return err
}

// writeLSP writes an LSP-style message with Content-Length header.
func (t *StdioTransport) writeLSP(data []byte) error {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	_, err := t.writer.Write([]byte(header))
	if err != nil {
		return err
	}
	_, err = t.writer.Write(data)
	return err
}

// Close closes the stdio transport.
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true
	close(t.done)

	// Don't close stdin/stdout as they may be shared
	return nil
}

// Done returns a channel that's closed when the transport is closed.
func (t *StdioTransport) Done() <-chan struct{} {
	return t.done
}

// Info returns metadata about the stdio transport.
func (t *StdioTransport) Info() TransportInfo {
	return TransportInfo{
		Type: "stdio",
	}
}

// trimCRLF removes trailing \r\n or \n from a byte slice.
func trimCRLF(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	if len(data) > 0 && data[len(data)-1] == '\r' {
		data = data[:len(data)-1]
	}
	return data
}
