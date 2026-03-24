package llm

import "context"

// ChatMessage represents a single message in a chat conversation.
type ChatMessage struct {
	Role    string
	Content string
}

// ChatRequest holds parameters for a chat completion call.
type ChatRequest struct {
	Messages    []ChatMessage
	MaxTokens   int
	Temperature float32
	JSONOutput  bool // enforces response_format: json_object
}

// ChatResponse holds the result of a chat completion call.
type ChatResponse struct {
	Content  string
	Provider string
	Model    string
}

// Client is the interface for LLM chat completion providers.
type Client interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
}
