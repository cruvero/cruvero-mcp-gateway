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

// loginOptions configures the device code login flow.
type loginOptions struct {
	noBrowser bool
	msgWriter io.Writer
}

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

	return performLogin(baseURL, loginOptions{noBrowser: *noBrowser, msgWriter: stdout})
}

// performLogin runs the device code flow and saves tokens on success.
func performLogin(baseURL string, opts loginOptions) error {
	codeResp, err := requestDeviceCode(baseURL)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(opts.msgWriter, "Go to: %s\n", codeResp.VerificationURI)
	_, _ = fmt.Fprintf(opts.msgWriter, "Enter code: %s\n", codeResp.UserCode)

	if !opts.noBrowser && codeResp.VerificationURI != "" {
		browserURL := codeResp.VerificationURI
		if codeResp.VerificationURIComplete != "" {
			browserURL = codeResp.VerificationURIComplete
		}
		openBrowser(browserURL)
	}

	if err := pollForToken(baseURL, codeResp); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(opts.msgWriter, "Login successful.")
	return nil
}

type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

func requestDeviceCode(baseURL string) (*deviceCodeResponse, error) {
	form := url.Values{"scope": {"openid profile email"}}
	resp, err := httpClientForAuth.PostForm(baseURL+"/device/code", form)
	if err != nil {
		return nil, fmt.Errorf("auth login: request device code: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth login: device code request failed with status %d", resp.StatusCode)
	}

	var codeResp deviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&codeResp); err != nil {
		return nil, fmt.Errorf("auth login: decode device code response: %w", err)
	}
	return &codeResp, nil
}

func pollForToken(baseURL string, codeResp *deviceCodeResponse) error {
	interval := time.Duration(codeResp.Interval) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(codeResp.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		time.Sleep(interval)

		result, err := pollTokenOnce(baseURL, codeResp.DeviceCode)
		if err != nil {
			return err
		}
		if result == pollPending {
			continue
		}
		return nil
	}

	return fmt.Errorf("auth login: device code expired")
}

type pollResult int

const (
	pollPending pollResult = iota
	pollSuccess
	pollFailed
)

func pollTokenOnce(baseURL string, deviceCode string) (pollResult, error) {
	tokenForm := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
	}
	tokenResp, err := httpClientForAuth.PostForm(baseURL+"/device/token", tokenForm)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "poll error: %v\n", err)
		return pollPending, nil
	}

	body, err := io.ReadAll(tokenResp.Body)
	_ = tokenResp.Body.Close()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "poll read error: %v\n", err)
		return pollPending, nil
	}

	if tokenResp.StatusCode == http.StatusTooEarly {
		return pollPending, nil
	}
	if tokenResp.StatusCode == http.StatusGone {
		return pollFailed, fmt.Errorf("auth login: device code expired — please restart the login flow")
	}
	if tokenResp.StatusCode == http.StatusForbidden {
		return pollFailed, fmt.Errorf("auth login: access denied by the identity provider")
	}
	if tokenResp.StatusCode != http.StatusOK {
		msg := parseErrorDescription(body)
		if msg == "" {
			msg = string(body)
		}
		return pollFailed, fmt.Errorf("auth login: token request failed with status %d: %s", tokenResp.StatusCode, msg)
	}

	return pollSuccess, saveTokenResponse(body, baseURL)
}

func saveTokenResponse(body []byte, baseURL string) error {
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

	return nil
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

func parseErrorDescription(body []byte) string {
	var errResp struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.ErrorDescription != "" {
		return errResp.ErrorDescription
	}
	return ""
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
