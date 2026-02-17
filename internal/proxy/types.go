package proxy

import "encoding/json"

// ToolDefinition describes a tool exposed by a backend MCP server.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ContentBlock is a normalized tool result content entry.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolResult is a normalized result returned from a tool call.
type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"is_error"`
}

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
