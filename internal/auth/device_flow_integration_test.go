//go:build integration

package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
)

func TestDeviceFlowIntegration(t *testing.T) {
	// Mock IdP with device code lifecycle.
	authorized := make(chan struct{})
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		vals, _ := url.ParseQuery(string(body))

		switch r.URL.Path {
		case "/device/authorize":
			if vals.Get("client_id") == "" {
				t.Errorf("missing client_id in device authorize request")
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":               "integration-device-code",
				"user_code":                 "INT-1234",
				"verification_uri":          "https://idp.test/verify",
				"verification_uri_complete": "https://idp.test/verify?user_code=INT-1234",
				"expires_in":                300,
				"interval":                  1,
			})
		case "/token":
			select {
			case <-authorized:
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token":  "integration-access-token",
					"refresh_token": "integration-refresh-token",
					"id_token":      "eyJ.test.token",
					"token_type":    "Bearer",
					"expires_in":    2,
				})
			default:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "authorization_pending",
				})
			}
		case "/refresh":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "refreshed-access-token",
				"refresh_token": "refreshed-refresh-token",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		}
	}))
	defer idp.Close()

	cfg := &config.Config{
		DeviceFlowEnabled:      true,
		DeviceFlowIDPDeviceURL: idp.URL + "/device/authorize",
		DeviceFlowIDPTokenURL:  idp.URL + "/token",
		DeviceFlowClientID:     "integration-client",
	}

	handler := NewDeviceFlowHandler(cfg, nil)
	gwServer := httptest.NewServer(handler.Routes())
	defer gwServer.Close()

	// Step 1: Request device code.
	form := url.Values{"scope": {"openid profile"}}
	resp, err := http.PostForm(gwServer.URL+"/code", form)
	if err != nil {
		t.Fatalf("device code request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var codeResp struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&codeResp); err != nil {
		t.Fatalf("decode device code response: %v", err)
	}
	if codeResp.DeviceCode == "" || codeResp.UserCode == "" {
		t.Fatalf("incomplete device code response: %+v", codeResp)
	}

	// Step 2: Poll before authorization — expect pending.
	tokenForm := url.Values{
		"grant_type":  {deviceGrantType},
		"device_code": {codeResp.DeviceCode},
	}
	pendingResp, err := http.PostForm(gwServer.URL+"/token", tokenForm)
	if err != nil {
		t.Fatalf("pending token request failed: %v", err)
	}
	_ = pendingResp.Body.Close()
	if pendingResp.StatusCode != http.StatusTooEarly {
		t.Fatalf("expected 425, got %d", pendingResp.StatusCode)
	}

	// Step 3: Simulate user authorization.
	close(authorized)

	// Step 4: Poll again — expect success.
	tokenResp, err := http.PostForm(gwServer.URL+"/token", tokenForm)
	if err != nil {
		t.Fatalf("token request failed: %v", err)
	}
	defer func() { _ = tokenResp.Body.Close() }()

	if tokenResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(tokenResp.Body)
		t.Fatalf("expected 200, got %d: %s", tokenResp.StatusCode, string(body))
	}

	var tokenData struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenData); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if tokenData.AccessToken == "" {
		t.Fatalf("empty access token in response")
	}

	// Step 5: Verify page renders.
	verifyResp, err := http.Get(gwServer.URL + "/verify?user_code=" + codeResp.UserCode)
	if err != nil {
		t.Fatalf("verify request failed: %v", err)
	}
	defer func() { _ = verifyResp.Body.Close() }()

	if verifyResp.StatusCode != http.StatusOK {
		t.Fatalf("verify expected 200, got %d", verifyResp.StatusCode)
	}
	verifyBody, _ := io.ReadAll(verifyResp.Body)
	if !strings.Contains(string(verifyBody), codeResp.UserCode) {
		t.Fatalf("verify page does not contain user code")
	}

	// Step 6: Token refresh.
	t.Setenv("HOME", t.TempDir())
	tokens := &CachedTokens{
		AccessToken:  tokenData.AccessToken,
		RefreshToken: tokenData.RefreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(-time.Second),
		GatewayURL:   gwServer.URL,
	}

	refreshed, err := RefreshAccessToken(tokens, idp.URL+"/refresh", cfg.DeviceFlowClientID)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if refreshed.AccessToken != "refreshed-access-token" {
		t.Fatalf("expected refreshed access token, got %q", refreshed.AccessToken)
	}
}
