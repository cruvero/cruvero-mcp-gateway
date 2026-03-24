package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cruvero/mcp-gateway/internal/auth"
)

var httpClientForProxy = &http.Client{Timeout: 120 * time.Second}

func mcpProxyCommand(args []string) error {
	fs := flag.NewFlagSet("mcp-proxy", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	gatewayURL := fs.String("gateway-url", "", "gateway base URL (required)")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse mcp-proxy flags: %w", err)
	}
	if strings.TrimSpace(*gatewayURL) == "" {
		return fmt.Errorf("mcp-proxy: --gateway-url is required")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(*gatewayURL), "/")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return runProxy(ctx, baseURL, os.Stdin, os.Stdout)
}

func runProxy(ctx context.Context, baseURL string, input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var sessionID string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		tokens, err := ensureValidToken(baseURL)
		if err != nil {
			return fmt.Errorf("mcp-proxy: %w", err)
		}

		respBody, respSessionID, err := sendMCPRequest(ctx, baseURL, tokens.AccessToken, sessionID, line)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "mcp-proxy: request error: %v\n", err)
			continue
		}
		if respSessionID != "" {
			sessionID = respSessionID
		}

		if len(respBody) > 0 {
			_, _ = output.Write(respBody)
			_, _ = output.Write([]byte("\n"))
		}
	}

	return scanner.Err()
}

// loadAndRefreshToken loads cached tokens and refreshes if needed.
func loadAndRefreshToken() (*auth.CachedTokens, error) {
	tokens, err := auth.LoadTokens()
	if err != nil {
		return nil, fmt.Errorf("load tokens: %w", err)
	}

	if !tokens.NeedsRefresh() {
		return tokens, nil
	}

	if strings.TrimSpace(tokens.RefreshToken) == "" {
		return nil, fmt.Errorf("access token expired and no refresh token available")
	}

	refreshed, err := auth.RefreshAccessToken(tokens, tokens.GatewayURL+"/device/token", "")
	if err != nil {
		return nil, fmt.Errorf("token refresh failed: %w", err)
	}
	return refreshed, nil
}

func ensureValidToken(baseURL string) (*auth.CachedTokens, error) {
	tokens, err := loadAndRefreshToken()
	if err == nil {
		return tokens, nil
	}

	_, _ = fmt.Fprintf(stderr, "mcp-proxy: %v — starting login...\n", err)

	if loginErr := performLogin(baseURL, loginOptions{noBrowser: true, msgWriter: stderr}); loginErr != nil {
		return nil, fmt.Errorf("auto-login failed: %w", loginErr)
	}

	return auth.LoadTokens()
}

func sendMCPRequest(ctx context.Context, baseURL, accessToken, sessionID string, body []byte) ([]byte, string, error) {
	endpoint := baseURL + "/mcp"
	var lastErr error
	respSessionID := sessionID
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, respSessionID, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, respSessionID, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+accessToken)
		if respSessionID != "" {
			req.Header.Set("Mcp-Session-Id", respSessionID)
		}

		resp, err := httpClientForProxy.Do(req) // #nosec G704 -- endpoint is the user-configured gateway URL
		if err != nil {
			lastErr = err
			continue
		}

		if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
			respSessionID = sid
		}

		result, newToken, retryErr, fatalErr := handleMCPResponse(ctx, resp, baseURL)
		if fatalErr != nil {
			return nil, respSessionID, fatalErr
		}
		if retryErr != nil {
			lastErr = retryErr
			if newToken != "" {
				accessToken = newToken
			}
			continue
		}
		return result, respSessionID, nil
	}

	if lastErr != nil {
		return nil, respSessionID, fmt.Errorf("after retries: %w", lastErr)
	}
	return nil, respSessionID, fmt.Errorf("request failed after retries")
}

func handleMCPResponse(ctx context.Context, resp *http.Response, baseURL string) (result []byte, newToken string, retryErr error, fatalErr error) {
	respBody, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, "", readErr, nil
	}

	switch resp.StatusCode {
	case http.StatusOK:
		if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
			return extractSSEData(respBody), "", nil, nil
		}
		return respBody, "", nil, nil
	case http.StatusAccepted:
		return nil, "", nil, nil
	case http.StatusUnauthorized:
		tokens, refreshErr := ensureValidToken(baseURL)
		if refreshErr != nil {
			return nil, "", nil, fmt.Errorf("auth failed and refresh failed: %w", refreshErr)
		}
		return nil, tokens.AccessToken, fmt.Errorf("unauthorized"), nil
	case http.StatusTooManyRequests:
		waitForRetryAfter(ctx, resp.Header.Get("Retry-After"))
		return nil, "", fmt.Errorf("rate limited (status 429)"), nil
	default:
		return nil, "", nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
}

func waitForRetryAfter(ctx context.Context, retryAfter string) {
	if retryAfter == "" {
		return
	}
	d, parseErr := time.ParseDuration(retryAfter + "s")
	if parseErr != nil {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func extractSSEData(body []byte) []byte {
	var result bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // up to 1MB tokens
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimSpace(data)
			if data != "" {
				if result.Len() > 0 {
					result.WriteByte('\n')
				}
				result.WriteString(data)
			}
		}
	}
	return result.Bytes()
}
