// Package jsonl provides streaming JSONL readers with optional line size limits.
package jsonl

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// ErrLineTooLong is returned when a JSONL line exceeds the configured max size.
var ErrLineTooLong = errors.New("jsonl line exceeds max size")

// Line represents a single JSONL line read from a stream.
// Data excludes trailing newline characters. BytesRead includes any newline bytes consumed.
type Line struct {
	Data      []byte
	BytesRead int
	TooLong   bool
}

// Reader streams JSONL lines from an io.Reader.
type Reader struct {
	br           *bufio.Reader
	maxLineBytes int
}

// NewReader creates a new JSONL streaming reader.
// maxLineBytes of 0 disables the line size limit.
func NewReader(r io.Reader, maxLineBytes int) *Reader {
	return &Reader{
		br:           bufio.NewReader(r),
		maxLineBytes: maxLineBytes,
	}
}

// Next reads the next JSONL line. It returns io.EOF when no more data remains.
// If the line exceeds maxLineBytes, TooLong is set and Data is nil.
func (r *Reader) Next() (Line, error) {
	var (
		buf       []byte
		bytesRead int
		tooLong   bool
	)

	for {
		part, err := r.br.ReadSlice('\n')
		bytesRead += len(part)

		if err == bufio.ErrBufferFull {
			if !tooLong {
				if r.maxLineBytes > 0 && len(buf)+len(part) > r.maxLineBytes {
					tooLong = true
				} else {
					buf = append(buf, part...)
				}
			}
			continue
		}

		if err != nil {
			if err == io.EOF {
				if len(part) == 0 {
					return Line{}, io.EOF
				}
				if !tooLong {
					if r.maxLineBytes > 0 && len(buf)+len(part) > r.maxLineBytes {
						tooLong = true
					} else {
						buf = append(buf, part...)
					}
				}
				if tooLong {
					return Line{BytesRead: bytesRead, TooLong: true}, nil
				}
				return Line{Data: trimLine(buf), BytesRead: bytesRead}, nil
			}
			return Line{}, err
		}

		if !tooLong {
			if r.maxLineBytes > 0 && len(buf)+len(part) > r.maxLineBytes {
				tooLong = true
			} else {
				buf = append(buf, part...)
			}
		}

		if tooLong {
			return Line{BytesRead: bytesRead, TooLong: true}, nil
		}

		return Line{Data: trimLine(buf), BytesRead: bytesRead}, nil
	}
}

func trimLine(b []byte) []byte {
	b = bytes.TrimSuffix(b, []byte{'\n'})
	b = bytes.TrimSuffix(b, []byte{'\r'})
	return b
}
