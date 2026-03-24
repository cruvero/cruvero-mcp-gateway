package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Embedder converts text into vector embeddings.
type Embedder interface {
	// Embed returns a vector embedding for each input text.
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// Ready reports whether the embedding service is reachable.
	Ready(ctx context.Context) bool
}

// HTTPEmbedder calls an external embedding service over HTTP.
type HTTPEmbedder struct {
	baseURL    string
	client     *http.Client
	maxRetries int
}

// NewHTTPEmbedder creates an embedder client targeting the given base URL.
func NewHTTPEmbedder(baseURL string) *HTTPEmbedder {
	return &HTTPEmbedder{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		maxRetries: 2,
	}
}

type embedRequest struct {
	Texts []string `json:"texts"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed sends texts to the sidecar /embed endpoint and returns embeddings.
func (e *HTTPEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(embedRequest{Texts: texts})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	var lastErr error
	for attempt := range e.maxRetries + 1 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt*100) * time.Millisecond):
			}
		}

		result, reqErr := e.doEmbed(ctx, body)
		if reqErr == nil {
			return result, nil
		}
		lastErr = reqErr
	}

	return nil, fmt.Errorf("embed after %d attempts: %w", e.maxRetries+1, lastErr)
}

func (e *HTTPEmbedder) doEmbed(ctx context.Context, body []byte) ([][]float32, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req) // #nosec G704 -- baseURL is from server config, not user input
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("embed returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}

	return result.Embeddings, nil
}

// Ready checks the sidecar /healthz endpoint.
func (e *HTTPEmbedder) Ready(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+"/healthz", nil)
	if err != nil {
		return false
	}

	resp, err := e.client.Do(req) // #nosec G704 -- baseURL is from server config, not user input
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode == http.StatusOK
}
