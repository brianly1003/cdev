// Package message defines JSON-RPC 2.0 message types.
package message

import (
	"encoding/json"
	"fmt"
)

// Version is the JSON-RPC protocol version.
const Version = "2.0"

// Request represents a JSON-RPC 2.0 request.
// If ID is nil, this is a notification (no response expected).
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *ID             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IsNotification returns true if this request is a notification (no ID).
func (r *Request) IsNotification() bool {
	return r.ID == nil
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *ID             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// IsError returns true if this response contains an error.
func (r *Response) IsError() bool {
	return r.Error != nil
}

// Notification represents a server-to-client notification (no ID, no response expected).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// ID represents a JSON-RPC ID which can be string, number, or null.
// Per spec, ID can be a String, Number, or Null. We support string and int64.
type ID struct {
	value interface{} // string or int64
}

// StringID creates an ID from a string.
func StringID(s string) *ID {
	return &ID{value: s}
}

// NumberID creates an ID from an integer.
func NumberID(n int64) *ID {
	return &ID{value: n}
}

// IsString returns true if the ID is a string.
func (id *ID) IsString() bool {
	_, ok := id.value.(string)
	return ok
}

// IsNumber returns true if the ID is a number.
func (id *ID) IsNumber() bool {
	_, ok := id.value.(int64)
	return ok
}

// String returns the ID as a string (for logging/debugging).
func (id *ID) String() string {
	if id == nil {
		return "<nil>"
	}
	switch v := id.value.(type) {
	case string:
		return v
	case int64:
		return fmt.Sprintf("%d", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// MarshalJSON implements json.Marshaler.
func (id *ID) MarshalJSON() ([]byte, error) {
	if id == nil {
		return []byte("null"), nil
	}
	return json.Marshal(id.value)
}

// UnmarshalJSON implements json.Unmarshaler.
func (id *ID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		id.value = nil
		return nil
	}

	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		id.value = s
		return nil
	}

	// Try number
	var n int64
	if err := json.Unmarshal(data, &n); err == nil {
		id.value = n
		return nil
	}

	// Try float (JSON numbers may be floats)
	var f float64
	if err := json.Unmarshal(data, &f); err == nil {
		id.value = int64(f)
		return nil
	}

	return fmt.Errorf("invalid ID type: %s", string(data))
}

// NewRequest creates a new JSON-RPC request.
func NewRequest(id *ID, method string, params interface{}) (*Request, error) {
	req := &Request{
		JSONRPC: Version,
		ID:      id,
		Method:  method,
	}

	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		req.Params = data
	}

	return req, nil
}

// NewNotification creates a new JSON-RPC notification.
func NewNotification(method string, params interface{}) (*Notification, error) {
	notif := &Notification{
		JSONRPC: Version,
		Method:  method,
	}

	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		notif.Params = data
	}

	return notif, nil
}

// NewSuccessResponse creates a successful JSON-RPC response.
func NewSuccessResponse(id *ID, result interface{}) (*Response, error) {
	resp := &Response{
		JSONRPC: Version,
		ID:      id,
	}

	if result != nil {
		data, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal result: %w", err)
		}
		resp.Result = data
	}

	return resp, nil
}

// NewErrorResponse creates an error JSON-RPC response.
func NewErrorResponse(id *ID, err *Error) *Response {
	return &Response{
		JSONRPC: Version,
		ID:      id,
		Error:   err,
	}
}

// ParseRequest parses a JSON-RPC request from bytes.
func ParseRequest(data []byte) (*Request, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}

	// Validate
	if req.JSONRPC != Version {
		return nil, fmt.Errorf("invalid jsonrpc version: %s", req.JSONRPC)
	}
	if req.Method == "" {
		return nil, fmt.Errorf("missing method")
	}

	return &req, nil
}

// ParseResponse parses a JSON-RPC response from bytes.
func ParseResponse(data []byte) (*Response, error) {
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	// Validate
	if resp.JSONRPC != Version {
		return nil, fmt.Errorf("invalid jsonrpc version: %s", resp.JSONRPC)
	}

	return &resp, nil
}

// IsJSONRPC checks if the given data looks like a JSON-RPC message.
// This is a quick heuristic check, not a full validation.
func IsJSONRPC(data []byte) bool {
	// Quick check: must start with { and contain "jsonrpc"
	if len(data) < 20 {
		return false
	}
	if data[0] != '{' {
		return false
	}

	// Check for "jsonrpc" field
	var msg struct {
		JSONRPC string `json:"jsonrpc"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return false
	}

	return msg.JSONRPC == Version
}
