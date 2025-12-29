package message

import (
	"encoding/json"
	"testing"
)

// --- ID Tests ---

func TestID_StringID(t *testing.T) {
	id := StringID("test-123")
	if !id.IsString() {
		t.Error("expected IsString() to be true")
	}
	if id.IsNumber() {
		t.Error("expected IsNumber() to be false")
	}
	if id.String() != "test-123" {
		t.Errorf("expected String() = 'test-123', got '%s'", id.String())
	}
}

func TestID_NumberID(t *testing.T) {
	id := NumberID(42)
	if id.IsString() {
		t.Error("expected IsString() to be false")
	}
	if !id.IsNumber() {
		t.Error("expected IsNumber() to be true")
	}
	if id.String() != "42" {
		t.Errorf("expected String() = '42', got '%s'", id.String())
	}
}

func TestID_NilID(t *testing.T) {
	var id *ID
	if id.String() != "<nil>" {
		t.Errorf("expected String() = '<nil>', got '%s'", id.String())
	}
}

func TestID_MarshalJSON_String(t *testing.T) {
	id := StringID("req-1")
	data, err := json.Marshal(id)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}
	if string(data) != `"req-1"` {
		t.Errorf("expected '\"req-1\"', got '%s'", string(data))
	}
}

func TestID_MarshalJSON_Number(t *testing.T) {
	id := NumberID(123)
	data, err := json.Marshal(id)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}
	if string(data) != "123" {
		t.Errorf("expected '123', got '%s'", string(data))
	}
}

func TestID_MarshalJSON_Nil(t *testing.T) {
	var id *ID
	data, err := json.Marshal(id)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}
	if string(data) != "null" {
		t.Errorf("expected 'null', got '%s'", string(data))
	}
}

func TestID_UnmarshalJSON_String(t *testing.T) {
	var id ID
	err := json.Unmarshal([]byte(`"req-abc"`), &id)
	if err != nil {
		t.Fatalf("UnmarshalJSON error: %v", err)
	}
	if !id.IsString() {
		t.Error("expected IsString() to be true")
	}
	if id.String() != "req-abc" {
		t.Errorf("expected 'req-abc', got '%s'", id.String())
	}
}

func TestID_UnmarshalJSON_Number(t *testing.T) {
	var id ID
	err := json.Unmarshal([]byte(`456`), &id)
	if err != nil {
		t.Fatalf("UnmarshalJSON error: %v", err)
	}
	if !id.IsNumber() {
		t.Error("expected IsNumber() to be true")
	}
	if id.String() != "456" {
		t.Errorf("expected '456', got '%s'", id.String())
	}
}

func TestID_UnmarshalJSON_Float(t *testing.T) {
	var id ID
	err := json.Unmarshal([]byte(`789.0`), &id)
	if err != nil {
		t.Fatalf("UnmarshalJSON error: %v", err)
	}
	if !id.IsNumber() {
		t.Error("expected IsNumber() to be true after float conversion")
	}
	if id.String() != "789" {
		t.Errorf("expected '789', got '%s'", id.String())
	}
}

func TestID_UnmarshalJSON_Null(t *testing.T) {
	var id ID
	err := json.Unmarshal([]byte(`null`), &id)
	if err != nil {
		t.Fatalf("UnmarshalJSON error: %v", err)
	}
	if id.value != nil {
		t.Error("expected value to be nil")
	}
}

func TestID_UnmarshalJSON_Invalid(t *testing.T) {
	var id ID
	err := json.Unmarshal([]byte(`[1,2,3]`), &id)
	if err == nil {
		t.Error("expected error for invalid ID type")
	}
}

// --- Request Tests ---

func TestRequest_IsNotification(t *testing.T) {
	tests := []struct {
		name     string
		id       *ID
		expected bool
	}{
		{"nil ID is notification", nil, true},
		{"string ID is not notification", StringID("1"), false},
		{"number ID is not notification", NumberID(1), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &Request{ID: tt.id}
			if req.IsNotification() != tt.expected {
				t.Errorf("IsNotification() = %v, want %v", req.IsNotification(), tt.expected)
			}
		})
	}
}

func TestNewRequest_WithParams(t *testing.T) {
	params := map[string]string{"key": "value"}
	req, err := NewRequest(StringID("1"), "test/method", params)
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	if req.JSONRPC != Version {
		t.Errorf("JSONRPC = %s, want %s", req.JSONRPC, Version)
	}
	if req.Method != "test/method" {
		t.Errorf("Method = %s, want 'test/method'", req.Method)
	}
	if req.ID.String() != "1" {
		t.Errorf("ID = %s, want '1'", req.ID.String())
	}
	if req.Params == nil {
		t.Error("Params should not be nil")
	}
}

func TestNewRequest_WithoutParams(t *testing.T) {
	req, err := NewRequest(NumberID(42), "test/method", nil)
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	if req.Params != nil {
		t.Error("Params should be nil")
	}
}

func TestNewRequest_InvalidParams(t *testing.T) {
	// Channels cannot be marshaled
	ch := make(chan int)
	_, err := NewRequest(StringID("1"), "test/method", ch)
	if err == nil {
		t.Error("expected error for unmarshalable params")
	}
}

// --- Response Tests ---

func TestResponse_IsError(t *testing.T) {
	tests := []struct {
		name     string
		err      *Error
		expected bool
	}{
		{"nil error", nil, false},
		{"with error", NewError(InternalError, "test"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &Response{Error: tt.err}
			if resp.IsError() != tt.expected {
				t.Errorf("IsError() = %v, want %v", resp.IsError(), tt.expected)
			}
		})
	}
}

func TestNewSuccessResponse(t *testing.T) {
	result := map[string]int{"count": 5}
	resp, err := NewSuccessResponse(StringID("req-1"), result)
	if err != nil {
		t.Fatalf("NewSuccessResponse error: %v", err)
	}
	if resp.JSONRPC != Version {
		t.Errorf("JSONRPC = %s, want %s", resp.JSONRPC, Version)
	}
	if resp.ID.String() != "req-1" {
		t.Errorf("ID = %s, want 'req-1'", resp.ID.String())
	}
	if resp.Error != nil {
		t.Error("Error should be nil")
	}
	if resp.Result == nil {
		t.Error("Result should not be nil")
	}
}

func TestNewSuccessResponse_NilResult(t *testing.T) {
	resp, err := NewSuccessResponse(NumberID(1), nil)
	if err != nil {
		t.Fatalf("NewSuccessResponse error: %v", err)
	}
	if resp.Result != nil {
		t.Error("Result should be nil")
	}
}

func TestNewErrorResponse(t *testing.T) {
	rpcErr := NewError(MethodNotFound, "method not found")
	resp := NewErrorResponse(StringID("1"), rpcErr)
	if resp.JSONRPC != Version {
		t.Errorf("JSONRPC = %s, want %s", resp.JSONRPC, Version)
	}
	if resp.Error == nil {
		t.Error("Error should not be nil")
	}
	if resp.Error.Code != MethodNotFound {
		t.Errorf("Error.Code = %d, want %d", resp.Error.Code, MethodNotFound)
	}
}

// --- Notification Tests ---

func TestNewNotification(t *testing.T) {
	params := map[string]string{"event": "test"}
	notif, err := NewNotification("event/notify", params)
	if err != nil {
		t.Fatalf("NewNotification error: %v", err)
	}
	if notif.JSONRPC != Version {
		t.Errorf("JSONRPC = %s, want %s", notif.JSONRPC, Version)
	}
	if notif.Method != "event/notify" {
		t.Errorf("Method = %s, want 'event/notify'", notif.Method)
	}
	if notif.Params == nil {
		t.Error("Params should not be nil")
	}
}

func TestNewNotification_NilParams(t *testing.T) {
	notif, err := NewNotification("event/ping", nil)
	if err != nil {
		t.Fatalf("NewNotification error: %v", err)
	}
	if notif.Params != nil {
		t.Error("Params should be nil")
	}
}

// --- ParseRequest Tests ---

func TestParseRequest_Valid(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":1,"method":"test/method","params":{"key":"value"}}`)
	req, err := ParseRequest(data)
	if err != nil {
		t.Fatalf("ParseRequest error: %v", err)
	}
	if req.Method != "test/method" {
		t.Errorf("Method = %s, want 'test/method'", req.Method)
	}
	if req.ID.String() != "1" {
		t.Errorf("ID = %s, want '1'", req.ID.String())
	}
}

func TestParseRequest_Notification(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","method":"event/notify"}`)
	req, err := ParseRequest(data)
	if err != nil {
		t.Fatalf("ParseRequest error: %v", err)
	}
	if !req.IsNotification() {
		t.Error("expected notification (no ID)")
	}
}

func TestParseRequest_InvalidJSON(t *testing.T) {
	data := []byte(`not json`)
	_, err := ParseRequest(data)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseRequest_WrongVersion(t *testing.T) {
	data := []byte(`{"jsonrpc":"1.0","id":1,"method":"test"}`)
	_, err := ParseRequest(data)
	if err == nil {
		t.Error("expected error for wrong version")
	}
}

func TestParseRequest_MissingMethod(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":1}`)
	_, err := ParseRequest(data)
	if err == nil {
		t.Error("expected error for missing method")
	}
}

// --- ParseResponse Tests ---

func TestParseResponse_Valid(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":1,"result":{"status":"ok"}}`)
	resp, err := ParseResponse(data)
	if err != nil {
		t.Fatalf("ParseResponse error: %v", err)
	}
	if resp.JSONRPC != Version {
		t.Errorf("JSONRPC = %s, want %s", resp.JSONRPC, Version)
	}
	if resp.IsError() {
		t.Error("expected success response, not error")
	}
}

func TestParseResponse_Error(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`)
	resp, err := ParseResponse(data)
	if err != nil {
		t.Fatalf("ParseResponse error: %v", err)
	}
	if !resp.IsError() {
		t.Error("expected error response")
	}
	if resp.Error.Code != InvalidRequest {
		t.Errorf("Error.Code = %d, want %d", resp.Error.Code, InvalidRequest)
	}
}

func TestParseResponse_InvalidJSON(t *testing.T) {
	data := []byte(`{invalid}`)
	_, err := ParseResponse(data)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseResponse_WrongVersion(t *testing.T) {
	data := []byte(`{"jsonrpc":"1.0","id":1,"result":{}}`)
	_, err := ParseResponse(data)
	if err == nil {
		t.Error("expected error for wrong version")
	}
}

// --- IsJSONRPC Tests ---

func TestIsJSONRPC(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		expected bool
	}{
		{"valid request", `{"jsonrpc":"2.0","id":1,"method":"test"}`, true},
		{"valid notification", `{"jsonrpc":"2.0","method":"event"}`, true},
		{"valid response", `{"jsonrpc":"2.0","id":1,"result":{}}`, true},
		{"wrong version", `{"jsonrpc":"1.0","method":"test"}`, false},
		{"no jsonrpc field", `{"id":1,"method":"test"}`, false},
		{"too short", `{"jsonrpc":"2.0"}`, false},
		{"not object", `["jsonrpc","2.0"]`, false},
		{"invalid json", `not json at all`, false},
		{"empty", ``, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsJSONRPC([]byte(tt.data))
			if result != tt.expected {
				t.Errorf("IsJSONRPC() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// --- Serialization Round-Trip Tests ---

func TestRequest_RoundTrip(t *testing.T) {
	original, _ := NewRequest(StringID("test-1"), "method/call", map[string]int{"x": 1})
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	parsed, err := ParseRequest(data)
	if err != nil {
		t.Fatalf("ParseRequest error: %v", err)
	}

	if parsed.Method != original.Method {
		t.Errorf("Method mismatch: got %s, want %s", parsed.Method, original.Method)
	}
	if parsed.ID.String() != original.ID.String() {
		t.Errorf("ID mismatch: got %s, want %s", parsed.ID.String(), original.ID.String())
	}
}

func TestResponse_RoundTrip(t *testing.T) {
	original, _ := NewSuccessResponse(NumberID(42), map[string]string{"result": "ok"})
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	parsed, err := ParseResponse(data)
	if err != nil {
		t.Fatalf("ParseResponse error: %v", err)
	}

	if parsed.ID.String() != original.ID.String() {
		t.Errorf("ID mismatch: got %s, want %s", parsed.ID.String(), original.ID.String())
	}
	if parsed.IsError() {
		t.Error("expected success response")
	}
}
