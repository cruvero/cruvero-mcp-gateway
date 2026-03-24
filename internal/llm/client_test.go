package llm

import (
	"testing"
)

func TestNewProviderClient_MissingAPIKey(t *testing.T) {
	t.Parallel()
	_, err := NewProviderClient("openai", "", "", "gpt-4o")
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

func TestNewProviderClient_MissingModel(t *testing.T) {
	t.Parallel()
	_, err := NewProviderClient("openai", "sk-test", "", "")
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}

func TestNewProviderClient_AzureMissingBaseURL(t *testing.T) {
	t.Parallel()
	_, err := NewProviderClient("azure", "key", "", "gpt-4o")
	if err == nil {
		t.Fatal("expected error for azure without base_url")
	}
}

func TestNewProviderClient_Success(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		provider string
		apiKey  string
		baseURL string
		model   string
	}{
		{"openai default", "openai", "sk-test", "", "gpt-4o"},
		{"openai custom base", "openai", "sk-test", "https://api.example.com/v1", "gpt-4o"},
		{"azure", "azure", "key", "https://myazure.openai.azure.com", "gpt-4o"},
		{"xai", "xai", "key", "https://api.x.ai/v1", "grok-3-mini"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, err := NewProviderClient(tt.provider, tt.apiKey, tt.baseURL, tt.model)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if client == nil {
				t.Fatal("expected non-nil client")
			}
		})
	}
}
