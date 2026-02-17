package permission

import "testing"

func TestGeneratePermissionType_MCP(t *testing.T) {
	permType := GeneratePermissionType("mcp__playwright__browser_navigate")
	if permType != "mcp_tool" {
		t.Fatalf("GeneratePermissionType() = %q, want %q", permType, "mcp_tool")
	}
}

func TestGenerateReadableDescription_MCP(t *testing.T) {
	toolInput := map[string]interface{}{
		"url": "https://example.com",
	}

	description := GenerateReadableDescription("mcp__playwright__browser_navigate", toolInput)
	expected := "Use MCP playwright/browser_navigate: https://example.com"
	if description != expected {
		t.Fatalf("GenerateReadableDescription() = %q, want %q", description, expected)
	}
}

func TestGenerateReadableDescription_MCPNoTarget(t *testing.T) {
	description := GenerateReadableDescription("mcp__playwright__screenshot", map[string]interface{}{})
	expected := "Use MCP tool: playwright/screenshot"
	if description != expected {
		t.Fatalf("GenerateReadableDescription() = %q, want %q", description, expected)
	}
}

func TestExtractTarget_MCP(t *testing.T) {
	toolInput := map[string]interface{}{
		"selector": "#login-button",
	}

	target := ExtractTarget("mcp__playwright__click", toolInput)
	if target != "#login-button" {
		t.Fatalf("ExtractTarget() = %q, want %q", target, "#login-button")
	}
}

func TestExtractTarget_MCPFallback(t *testing.T) {
	target := ExtractTarget("mcp__playwright__click", map[string]interface{}{})
	if target != "playwright/click" {
		t.Fatalf("ExtractTarget() = %q, want %q", target, "playwright/click")
	}
}
