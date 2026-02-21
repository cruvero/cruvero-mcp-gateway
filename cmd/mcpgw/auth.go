package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/cruvero/mcp-gateway/internal/auth"
)

var httpClientForAuth = &http.Client{Timeout: 30 * time.Second}

func authCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("auth command requires a subcommand: login, status, logout")
	}

	subcommand := strings.TrimSpace(args[0])
	subArgs := args[1:]

	switch subcommand {
	case "login":
		return authLoginCommand(subArgs)
	case "status":
		return authStatusCommand(subArgs)
	case "logout":
		return authLogoutCommand(subArgs)
	default:
		return fmt.Errorf("unknown auth subcommand %q (available: login, status, logout)", subcommand)
	}
}

func authLoginCommand(args []string) error {
	fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	gatewayURL := fs.String("gateway-url", "", "gateway base URL (required)")
	noBrowser := fs.Bool("no-browser", false, "do not open browser automatically")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse auth login flags: %w", err)
	}
	if strings.TrimSpace(*gatewayURL) == "" {
		return fmt.Errorf("auth login: --gateway-url is required")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(*gatewayURL), "/")

	// Request device code.
	form := url.Values{"scope": {"openid profile email"}}
	resp, err := httpClientForAuth.PostForm(baseURL+"/device/code", form)
	if err != nil {
		return fmt.Errorf("auth login: request device code: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth login: device code request failed with status %d", resp.StatusCode)
	}

	var codeResp struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&codeResp); err != nil {
		return fmt.Errorf("auth login: decode device code response: %w", err)
	}

	_, _ = fmt.Fprintf(stdout, "Go to: %s\n", codeResp.VerificationURI)
	_, _ = fmt.Fprintf(stdout, "Enter code: %s\n", codeResp.UserCode)

	if !*noBrowser && codeResp.VerificationURI != "" {
		openBrowser(codeResp.VerificationURI)
	}

	// Poll for token.
	interval := time.Duration(codeResp.Interval) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(codeResp.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		time.Sleep(interval)

		tokenForm := url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {codeResp.DeviceCode},
		}
		tokenResp, err := httpClientForAuth.PostForm(baseURL+"/device/token", tokenForm)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "poll error: %v\n", err)
			continue
		}

		body, _ := io.ReadAll(tokenResp.Body)
		_ = tokenResp.Body.Close()

		if tokenResp.StatusCode == http.StatusTooEarly {
			continue
		}

		if tokenResp.StatusCode == http.StatusOK {
			var tokenData struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
				IDToken      string `json:"id_token"`
				TokenType    string `json:"token_type"`
				ExpiresIn    int    `json:"expires_in"`
			}
			if err := json.Unmarshal(body, &tokenData); err != nil {
				return fmt.Errorf("auth login: decode token response: %w", err)
			}

			tokens := &auth.CachedTokens{
				AccessToken:  tokenData.AccessToken,
				RefreshToken: tokenData.RefreshToken,
				IDToken:      tokenData.IDToken,
				TokenType:    tokenData.TokenType,
				ExpiresAt:    time.Now().Add(time.Duration(tokenData.ExpiresIn) * time.Second),
				GatewayURL:   baseURL,
			}
			if err := auth.SaveTokens(tokens); err != nil {
				return fmt.Errorf("auth login: save tokens: %w", err)
			}

			_, _ = fmt.Fprintln(stdout, "Login successful.")
			return nil
		}

		return fmt.Errorf("auth login: token request failed with status %d: %s", tokenResp.StatusCode, string(body))
	}

	return fmt.Errorf("auth login: device code expired")
}

func authStatusCommand(args []string) error {
	fs := flag.NewFlagSet("auth status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse auth status flags: %w", err)
	}

	tokens, err := auth.LoadTokens()
	if err != nil {
		return fmt.Errorf("auth status: %w", err)
	}

	_, _ = fmt.Fprintf(stdout, "Gateway: %s\n", tokens.GatewayURL)
	_, _ = fmt.Fprintf(stdout, "Token type: %s\n", tokens.TokenType)
	_, _ = fmt.Fprintf(stdout, "Expires at: %s\n", tokens.ExpiresAt.Format(time.RFC3339))

	if tokens.IsExpired() {
		_, _ = fmt.Fprintln(stdout, "Status: expired")
	} else if tokens.NeedsRefresh() {
		_, _ = fmt.Fprintln(stdout, "Status: needs refresh")
	} else {
		_, _ = fmt.Fprintln(stdout, "Status: valid")
	}

	return nil
}

func authLogoutCommand(args []string) error {
	fs := flag.NewFlagSet("auth logout", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse auth logout flags: %w", err)
	}

	if err := auth.DeleteTokens(); err != nil {
		return fmt.Errorf("auth logout: %w", err)
	}

	_, _ = fmt.Fprintln(stdout, "Logged out.")
	return nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return
	}
	_ = cmd.Start()
}
