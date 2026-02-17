package proxy

import (
	"encoding/json"

	coretypes "github.com/cruvero/mcp-gateway/internal/types"
)

// ToolDefinition describes a tool exposed by a backend MCP server.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ContentBlock is a normalized tool result content entry.
type ContentBlock = coretypes.ContentBlock

// ToolResult is a normalized result returned from a tool call.
type ToolResult = coretypes.ToolResult

// ResourceDefinition describes a resource exposed by a backend MCP server.
type ResourceDefinition struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mime_type"`
}

// ResourceContent represents the textual content returned by resources/read.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mime_type"`
	Text     string `json:"text"`
}
