package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var httpClientForHealth = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13},
	},
}

func healthCommand(args []string) error {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	baseURL := fs.String("url", "https://localhost:8443", "gateway base URL")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse health flags: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("health command does not accept positional arguments")
	}

	status, err := fetchHealth(context.Background(), strings.TrimSpace(*baseURL))
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Health: %s (%d)\n", status.HealthState, status.HealthCode)
	_, _ = fmt.Fprintf(stdout, "Ready: %s (%d)\n", status.ReadyState, status.ReadyCode)
	if status.NATS != "" {
		_, _ = fmt.Fprintf(stdout, "NATS: %s\n", status.NATS)
	}
	if status.Version != "" {
		_, _ = fmt.Fprintf(stdout, "Version: %s\n", status.Version)
	}
	if status.Uptime != "" {
		_, _ = fmt.Fprintf(stdout, "Uptime: %s\n", status.Uptime)
	}
	if status.RegisteredServers >= 0 {
		_, _ = fmt.Fprintf(stdout, "Registered Servers: %d\n", status.RegisteredServers)
	}

	if status.HealthCode != http.StatusOK || status.ReadyCode != http.StatusOK {
		return fmt.Errorf("gateway is unhealthy")
	}
	return nil
}

type healthStatusResult struct {
	HealthCode        int
	HealthState       string
	ReadyCode         int
	ReadyState        string
	NATS              string
	Version           string
	Uptime            string
	RegisteredServers int
}

func fetchHealth(ctx context.Context, baseURL string) (*healthStatusResult, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("health check URL is required")
	}

	parsedBase, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse health URL: %w", err)
	}
	if parsedBase.Scheme == "" {
		parsedBase.Scheme = "https"
	}
	if err := validateHealthBaseURL(parsedBase); err != nil {
		return nil, fmt.Errorf("validate health URL: %w", err)
	}

	healthURL := parsedBase.ResolveReference(&url.URL{Path: "/healthz"}).String()
	readyURL := parsedBase.ResolveReference(&url.URL{Path: "/readyz"}).String()

	healthCode, healthBody, err := getJSON(ctx, healthURL)
	if err != nil {
		return nil, fmt.Errorf("healthz request: %w", err)
	}
	readyCode, readyBody, err := getJSON(ctx, readyURL)
	if err != nil {
		return nil, fmt.Errorf("readyz request: %w", err)
	}

	result := &healthStatusResult{
		HealthCode:        healthCode,
		ReadyCode:         readyCode,
		RegisteredServers: -1,
	}
	if status, ok := healthBody["status"].(string); ok {
		result.HealthState = status
	}
	if status, ok := readyBody["status"].(string); ok {
		result.ReadyState = status
	}
	if nats, ok := readyBody["nats"].(string); ok {
		result.NATS = nats
	}
	if version, ok := readyBody["version"].(string); ok {
		result.Version = version
	}
	if uptime, ok := readyBody["uptime"].(string); ok {
		result.Uptime = uptime
	}
	if count, ok := readyBody["registered_servers"].(float64); ok {
		result.RegisteredServers = int(count)
	}

	return result, nil
}

func getJSON(ctx context.Context, endpoint string) (int, map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}

	// #nosec G704 -- endpoint is explicit operator-provided CLI input.
	resp, err := httpClientForHealth.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := map[string]any{}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&body); err != nil {
		return resp.StatusCode, nil, fmt.Errorf("decode response: %w", err)
	}

	return resp.StatusCode, body, nil
}

func validateHealthBaseURL(baseURL *url.URL) error {
	if baseURL == nil {
		return fmt.Errorf("URL is required")
	}
	scheme := strings.ToLower(strings.TrimSpace(baseURL.Scheme))
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported scheme %q", baseURL.Scheme)
	}
	if strings.TrimSpace(baseURL.Host) == "" {
		return fmt.Errorf("host is required")
	}
	if baseURL.User != nil {
		return fmt.Errorf("user info is not allowed")
	}
	return nil
}
