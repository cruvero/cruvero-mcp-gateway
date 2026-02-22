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

		tokens, err := ensureValidToken()
		if err != nil {
			return fmt.Errorf("mcp-proxy: %w", err)
		}

		respBody, err := sendMCPRequest(ctx, baseURL+"/mcp", tokens.AccessToken, line)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "mcp-proxy: request error: %v\n", err)
			continue
		}

		_, _ = output.Write(respBody)
		_, _ = output.Write([]byte("\n"))
	}

	return scanner.Err()
}

func ensureValidToken() (*auth.CachedTokens, error) {
	tokens, err := auth.LoadTokens()
	if err != nil {
		return nil, fmt.Errorf("load tokens: %w (run 'mcpgw auth login' first)", err)
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

func sendMCPRequest(ctx context.Context, endpoint, accessToken string, body []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := httpClientForProxy.Do(req) // #nosec G704 -- endpoint is the user-configured gateway URL
		if err != nil {
			lastErr = err
			continue
		}

		result, newToken, retryErr, fatalErr := handleMCPResponse(ctx, resp)
		if fatalErr != nil {
			return nil, fatalErr
		}
		if retryErr != nil {
			lastErr = retryErr
			if newToken != "" {
				accessToken = newToken
			}
			continue
		}
		return result, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("after retries: %w", lastErr)
	}
	return nil, fmt.Errorf("request failed after retries")
}

func handleMCPResponse(ctx context.Context, resp *http.Response) (result []byte, newToken string, retryErr error, fatalErr error) {
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
	case http.StatusUnauthorized:
		tokens, refreshErr := ensureValidToken()
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
