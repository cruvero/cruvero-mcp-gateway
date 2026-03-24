package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIClient_Chat_Success(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"model":   "gpt-4o-mini",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": `{"summary":"test plan"}`,
				},
				"finish_reason": "stop",
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewProviderClient("openai", "sk-test", server.URL, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}

	resp, err := client.Chat(context.Background(), ChatRequest{
		Messages:    []ChatMessage{{Role: "user", Content: "hello"}},
		MaxTokens:   100,
		Temperature: 0.0,
		JSONOutput:  true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != `{"summary":"test plan"}` {
		t.Fatalf("expected JSON content, got %q", resp.Content)
	}
	if resp.Provider != "openai" {
		t.Fatalf("expected provider 'openai', got %q", resp.Provider)
	}
}

func TestOpenAIClient_Chat_EmptyChoices(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"model":   "gpt-4o-mini",
			"choices": []map[string]any{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewProviderClient("openai", "sk-test", server.URL, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}

	_, err = client.Chat(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestOpenAIClient_Chat_APIError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"server error"}}`))
	}))
	defer server.Close()

	client, err := NewProviderClient("openai", "sk-test", server.URL, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}

	_, err = client.Chat(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error for API error response")
	}
}
