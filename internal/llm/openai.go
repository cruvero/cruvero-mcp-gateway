package llm

import (
	"context"
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// openaiClient wraps the go-openai client to implement the Client interface.
type openaiClient struct {
	client       *openai.Client
	model        string
	providerName string
}

// NewProviderClient creates a Client backed by the go-openai library.
// For Azure, pass name="azure" and baseURL as the Azure endpoint.
// For other providers with OpenAI-compatible APIs, set baseURL accordingly.
func NewProviderClient(name, apiKey, baseURL, model string) (Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("llm: api key is required for provider %q", name)
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("llm: model is required for provider %q", name)
	}

	var client *openai.Client

	switch strings.ToLower(strings.TrimSpace(name)) {
	case "azure":
		if strings.TrimSpace(baseURL) == "" {
			return nil, fmt.Errorf("llm: base_url is required for azure provider")
		}
		cfg := openai.DefaultAzureConfig(apiKey, baseURL)
		client = openai.NewClientWithConfig(cfg)
	default:
		cfg := openai.DefaultConfig(apiKey)
		if u := strings.TrimSpace(baseURL); u != "" {
			cfg.BaseURL = u
		}
		client = openai.NewClientWithConfig(cfg)
	}

	return &openaiClient{
		client:       client,
		model:        model,
		providerName: name,
	}, nil
}

func (c *openaiClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	messages := make([]openai.ChatCompletionMessage, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = openai.ChatCompletionMessage{
			Role:    m.Role,
			Content: m.Content,
		}
	}

	completionReq := openai.ChatCompletionRequest{
		Model:       c.model,
		Messages:    messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	if req.JSONOutput {
		completionReq.ResponseFormat = &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		}
	}

	resp, err := c.client.CreateChatCompletion(ctx, completionReq)
	if err != nil {
		return nil, fmt.Errorf("llm %s: chat completion: %w", c.providerName, err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("llm %s: no choices in response", c.providerName)
	}

	return &ChatResponse{
		Content:  resp.Choices[0].Message.Content,
		Provider: c.providerName,
		Model:    c.model,
	}, nil
}
