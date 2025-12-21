// Package handler provides OpenRPC spec generation from the method registry.
package handler

import (
	"encoding/json"
)

// OpenRPCSpec represents the OpenRPC specification.
type OpenRPCSpec struct {
	OpenRPC    string            `json:"openrpc"`
	Info       OpenRPCInfo       `json:"info"`
	Servers    []OpenRPCServer   `json:"servers"`
	Methods    []OpenRPCMethod   `json:"methods"`
	Components OpenRPCComponents `json:"components"`
}

// OpenRPCInfo contains API metadata.
type OpenRPCInfo struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

// OpenRPCServer represents a server endpoint.
type OpenRPCServer struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// OpenRPCMethod represents a JSON-RPC method.
type OpenRPCMethod struct {
	Name        string            `json:"name"`
	Summary     string            `json:"summary"`
	Description string            `json:"description,omitempty"`
	Params      []OpenRPCParam    `json:"params"`
	Result      *OpenRPCResult    `json:"result,omitempty"`
	Errors      []OpenRPCErrorRef `json:"errors,omitempty"`
	Examples    []OpenRPCExample  `json:"examples,omitempty"`
}

// OpenRPCParam represents a method parameter.
type OpenRPCParam struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Required    bool                   `json:"required"`
	Schema      map[string]interface{} `json:"schema"`
}

// OpenRPCResult represents a method result.
type OpenRPCResult struct {
	Name   string                 `json:"name"`
	Schema map[string]interface{} `json:"schema"`
}

// OpenRPCErrorRef references an error definition.
type OpenRPCErrorRef struct {
	Ref string `json:"$ref,omitempty"`
}

// OpenRPCExample represents a method example.
type OpenRPCExample struct {
	Name   string                   `json:"name"`
	Params []map[string]interface{} `json:"params"`
	Result map[string]interface{}   `json:"result,omitempty"`
}

// OpenRPCComponents contains reusable components.
type OpenRPCComponents struct {
	Schemas map[string]interface{} `json:"schemas,omitempty"`
	Errors  map[string]interface{} `json:"errors,omitempty"`
}

// MethodMeta contains metadata for a registered method.
type MethodMeta struct {
	Summary     string
	Description string
	Params      []OpenRPCParam
	Result      *OpenRPCResult
	Errors      []string // Error names to reference
}

// GenerateOpenRPC generates an OpenRPC spec from the registry.
func (r *Registry) GenerateOpenRPC(info OpenRPCInfo, serverURL string) *OpenRPCSpec {
	spec := &OpenRPCSpec{
		OpenRPC: "1.2.6",
		Info:    info,
		Servers: []OpenRPCServer{
			{Name: "Default", URL: serverURL},
		},
		Methods: make([]OpenRPCMethod, 0),
		Components: OpenRPCComponents{
			Schemas: defaultSchemas(),
			Errors:  defaultErrors(),
		},
	}

	// Get all registered methods
	methods := r.Methods()
	for _, name := range methods {
		meta := r.GetMeta(name)
		method := OpenRPCMethod{
			Name:    name,
			Summary: meta.Summary,
			Params:  meta.Params,
			Result:  meta.Result,
		}
		if meta.Description != "" {
			method.Description = meta.Description
		}
		for _, errName := range meta.Errors {
			method.Errors = append(method.Errors, OpenRPCErrorRef{
				Ref: "#/components/errors/" + errName,
			})
		}
		spec.Methods = append(spec.Methods, method)
	}

	return spec
}

// ToJSON returns the OpenRPC spec as JSON.
func (spec *OpenRPCSpec) ToJSON() ([]byte, error) {
	return json.MarshalIndent(spec, "", "  ")
}

// defaultSchemas returns common schema definitions.
func defaultSchemas() map[string]interface{} {
	return map[string]interface{}{
		"AgentRunResult": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"status":     map[string]interface{}{"type": "string"},
				"session_id": map[string]interface{}{"type": "string"},
				"agent_type": map[string]interface{}{"type": "string"},
			},
		},
		"StatusResult": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id":        map[string]interface{}{"type": "string"},
				"agent_session_id":  map[string]interface{}{"type": "string"},
				"agent_state":       map[string]interface{}{"type": "string"},
				"connected_clients": map[string]interface{}{"type": "integer"},
				"repo_path":         map[string]interface{}{"type": "string"},
				"repo_name":         map[string]interface{}{"type": "string"},
				"uptime_seconds":    map[string]interface{}{"type": "integer"},
				"version":           map[string]interface{}{"type": "string"},
			},
		},
		"GitStatusResult": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"branch":    map[string]interface{}{"type": "string"},
				"staged":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"unstaged":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"untracked": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"is_clean":  map[string]interface{}{"type": "boolean"},
			},
		},
		"FileGetResult": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":      map[string]interface{}{"type": "string"},
				"content":   map[string]interface{}{"type": "string"},
				"size":      map[string]interface{}{"type": "integer"},
				"truncated": map[string]interface{}{"type": "boolean"},
			},
		},
	}
}

// defaultErrors returns common error definitions.
func defaultErrors() map[string]interface{} {
	return map[string]interface{}{
		"AgentAlreadyRunning": map[string]interface{}{
			"code":    -32001,
			"message": "An agent is already running",
		},
		"AgentNotRunning": map[string]interface{}{
			"code":    -32002,
			"message": "No agent is currently running",
		},
		"AgentError": map[string]interface{}{
			"code":    -32003,
			"message": "Agent execution error",
		},
		"AgentNotConfigured": map[string]interface{}{
			"code":    -32004,
			"message": "Agent type not configured",
		},
		"FileNotFound": map[string]interface{}{
			"code":    -32010,
			"message": "Requested file not found",
		},
		"GitError": map[string]interface{}{
			"code":    -32011,
			"message": "Git operation failed",
		},
		"SessionNotFound": map[string]interface{}{
			"code":    -32012,
			"message": "Session not found",
		},
	}
}
