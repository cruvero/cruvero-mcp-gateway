package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPEmbedder_Embed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		texts      []string
		wantCount  int
		wantErr    bool
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				var req embedRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				embeddings := make([][]float32, len(req.Texts))
				for i := range embeddings {
					embeddings[i] = []float32{0.1, 0.2, 0.3}
				}
				resp := embedResponse{Embeddings: embeddings}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
			},
			texts:     []string{"hello", "world"},
			wantCount: 2,
		},
		{
			name:      "empty texts returns nil",
			handler:   func(w http.ResponseWriter, r *http.Request) {},
			texts:     nil,
			wantCount: 0,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "model not loaded", http.StatusInternalServerError)
			},
			texts:   []string{"test"},
			wantErr: true,
		},
		{
			name: "invalid json response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("not json"))
			},
			texts:   []string{"test"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			embedder := NewHTTPEmbedder(srv.URL)
			embedder.maxRetries = 0 // no retries for fast tests

			result, err := embedder.Embed(context.Background(), tt.texts)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result) != tt.wantCount {
				t.Fatalf("expected %d embeddings, got %d", tt.wantCount, len(result))
			}
		})
	}
}

func TestHTTPEmbedder_EmbedRetry(t *testing.T) {
	t.Parallel()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			http.Error(w, "temporary error", http.StatusServiceUnavailable)
			return
		}
		resp := embedResponse{Embeddings: [][]float32{{0.1}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	embedder := NewHTTPEmbedder(srv.URL)
	embedder.maxRetries = 2

	result, err := embedder.Embed(context.Background(), []string{"test"})
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 embedding, got %d", len(result))
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls (1 + 2 retries), got %d", calls)
	}
}

func TestHTTPEmbedder_EmbedTimeout(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		resp := embedResponse{Embeddings: [][]float32{{0.1}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	embedder := NewHTTPEmbedder(srv.URL)
	embedder.client.Timeout = 50 * time.Millisecond
	embedder.maxRetries = 0

	_, err := embedder.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestHTTPEmbedder_EmbedContextCanceled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	embedder := NewHTTPEmbedder(srv.URL)
	embedder.maxRetries = 0

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := embedder.Embed(ctx, []string{"test"})
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
}

func TestHTTPEmbedder_Ready(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    bool
	}{
		{
			name: "healthy",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			want: true,
		},
		{
			name: "unhealthy",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			embedder := NewHTTPEmbedder(srv.URL)
			if got := embedder.Ready(context.Background()); got != tt.want {
				t.Fatalf("Ready() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHTTPEmbedder_ReadyUnreachable(t *testing.T) {
	t.Parallel()

	embedder := NewHTTPEmbedder("http://127.0.0.1:1") // unreachable port
	embedder.client.Timeout = 100 * time.Millisecond

	if embedder.Ready(context.Background()) {
		t.Fatal("expected Ready() = false for unreachable server")
	}
}
