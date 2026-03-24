package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/config"
)

func TestDeviceFlowHandler_Code(t *testing.T) {
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		vals, _ := url.ParseQuery(string(body))
		if vals.Get("client_id") != "test-client" {
			t.Fatalf("expected client_id test-client, got %s", vals.Get("client_id"))
		}
		if !strings.Contains(vals.Get("scope"), "openid") {
			t.Fatalf("expected scope to include openid, got %s", vals.Get("scope"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "device-abc",
			"user_code":        "ABCD-1234",
			"verification_uri": "https://idp.example.com/verify",
			"expires_in":       600,
			"interval":         5,
		})
	}))
	defer idp.Close()

	cfg := &config.Config{
		DeviceFlowEnabled:      true,
		DeviceFlowIDPDeviceURL: idp.URL,
		DeviceFlowIDPTokenURL:  idp.URL + "/token",
		DeviceFlowClientID:     "test-client",
	}

	handler := NewDeviceFlowHandler(cfg, nil)
	router := handler.Routes()

	tests := []struct {
		name       string
		form       url.Values
		wantStatus int
		wantError  string
	}{
		{
			name:       "valid request",
			form:       url.Values{"scope": {"openid profile"}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing openid scope",
			form:       url.Values{"scope": {"profile"}},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid_scope",
		},
		{
			name:       "empty scope",
			form:       url.Values{},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid_scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/code", strings.NewReader(tt.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", tt.wantStatus, w.Code, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				cl := w.Header().Get("Content-Length")
				if cl == "" {
					t.Fatal("expected Content-Length header on success response")
				}
				n, err := strconv.Atoi(cl)
				if err != nil {
					t.Fatalf("invalid Content-Length %q: %v", cl, err)
				}
				if n != w.Body.Len() {
					t.Fatalf("Content-Length %d does not match body size %d", n, w.Body.Len())
				}
			}

			if tt.wantError != "" {
				var errResp map[string]string
				if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
					t.Fatalf("unmarshal error response: %v", err)
				}
				if errResp["error"] != tt.wantError {
					t.Fatalf("expected error %q, got %q", tt.wantError, errResp["error"])
				}
			}
		})
	}
}

func TestDeviceFlowHandler_Token(t *testing.T) {
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		vals, _ := url.ParseQuery(string(body))

		if vals.Get("client_id") != "test-client" {
			t.Fatalf("expected client_id test-client, got %s", vals.Get("client_id"))
		}

		grantType := vals.Get("grant_type")
		switch grantType {
		case "refresh_token":
			refreshToken := vals.Get("refresh_token")
			if refreshToken == "valid-refresh" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token":  "access-refreshed",
					"refresh_token": "refresh-new",
					"token_type":    "Bearer",
					"expires_in":    3600,
				})
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			}
		default:
			deviceCode := vals.Get("device_code")
			switch deviceCode {
			case "pending-code":
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "authorization_pending",
				})
			case "valid-code":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token": "access-xyz",
					"token_type":   "Bearer",
					"expires_in":   3600,
				})
			default:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "invalid_grant",
				})
			}
		}
	}))
	defer idp.Close()

	cfg := &config.Config{
		DeviceFlowEnabled:     true,
		DeviceFlowIDPTokenURL: idp.URL,
		DeviceFlowClientID:    "test-client",
	}

	handler := NewDeviceFlowHandler(cfg, nil)
	router := handler.Routes()

	tests := []struct {
		name       string
		form       url.Values
		wantStatus int
	}{
		{
			name: "valid token exchange",
			form: url.Values{
				"grant_type":  {deviceGrantType},
				"device_code": {"valid-code"},
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "authorization pending",
			form: url.Values{
				"grant_type":  {deviceGrantType},
				"device_code": {"pending-code"},
			},
			wantStatus: http.StatusTooEarly,
		},
		{
			name: "wrong grant_type",
			form: url.Values{
				"grant_type":  {"authorization_code"},
				"device_code": {"valid-code"},
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "missing device_code",
			form: url.Values{
				"grant_type": {deviceGrantType},
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "valid refresh token",
			form: url.Values{
				"grant_type":    {"refresh_token"},
				"refresh_token": {"valid-refresh"},
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "missing refresh token",
			form: url.Values{
				"grant_type": {"refresh_token"},
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(tt.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", tt.wantStatus, w.Code, w.Body.String())
			}

			// Responses proxied from the IdP should include Content-Length.
			if cl := w.Header().Get("Content-Length"); cl != "" {
				n, err := strconv.Atoi(cl)
				if err != nil {
					t.Fatalf("invalid Content-Length %q: %v", cl, err)
				}
				if n != w.Body.Len() {
					t.Fatalf("Content-Length %d does not match body size %d", n, w.Body.Len())
				}
			}
		})
	}
}

func TestDeviceFlowHandler_Verify(t *testing.T) {
	cfg := &config.Config{
		DeviceFlowEnabled:  true,
		DeviceFlowClientID: "test-client",
	}
	handler := NewDeviceFlowHandler(cfg, nil)
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodGet, "/verify?user_code=ABCD-1234&verification_uri=https://idp.example.com/verify", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "ABCD-1234") {
		t.Fatalf("expected user code in response, got %s", body)
	}
	if !strings.Contains(body, "text/html") {
		ct := w.Header().Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Fatalf("expected text/html content type, got %s", ct)
		}
	}
}

func TestDeviceFlowHandler_VerifyEmptyUserCode(t *testing.T) {
	cfg := &config.Config{
		DeviceFlowEnabled:  true,
		DeviceFlowClientID: "test-client",
	}
	handler := NewDeviceFlowHandler(cfg, nil)
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodGet, "/verify", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp["error"] != "invalid_request" {
		t.Fatalf("expected error invalid_request, got %q", errResp["error"])
	}
}

func TestDeviceFlowHandler_TokenExpiredAndDenied(t *testing.T) {
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		vals, _ := url.ParseQuery(string(body))
		deviceCode := vals.Get("device_code")

		w.Header().Set("Content-Type", "application/json")
		switch deviceCode {
		case "expired-code":
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "expired_token"})
		case "denied-code":
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "access_denied"})
		default:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
		}
	}))
	defer idp.Close()

	cfg := &config.Config{
		DeviceFlowEnabled:     true,
		DeviceFlowIDPTokenURL: idp.URL,
		DeviceFlowClientID:    "test-client",
	}

	handler := NewDeviceFlowHandler(cfg, nil)
	router := handler.Routes()

	tests := []struct {
		name       string
		deviceCode string
		wantStatus int
	}{
		{
			name:       "expired token returns 410",
			deviceCode: "expired-code",
			wantStatus: http.StatusGone,
		},
		{
			name:       "access denied returns 403",
			deviceCode: "denied-code",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			form := url.Values{
				"grant_type":  {deviceGrantType},
				"device_code": {tt.deviceCode},
			}
			req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestDeviceFlowHandler_TokenWithClientSecret(t *testing.T) {
	var receivedSecret string
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		vals, _ := url.ParseQuery(string(body))
		receivedSecret = vals.Get("client_secret")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-xyz",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer idp.Close()

	cfg := &config.Config{
		DeviceFlowEnabled:      true,
		DeviceFlowIDPTokenURL:  idp.URL,
		DeviceFlowClientID:     "test-client",
		DeviceFlowClientSecret: "secret-123",
	}

	handler := NewDeviceFlowHandler(cfg, nil)
	router := handler.Routes()

	form := url.Values{
		"grant_type":  {deviceGrantType},
		"device_code": {"valid-code"},
	}
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if receivedSecret != "secret-123" {
		t.Fatalf("expected client_secret to be forwarded, got %q", receivedSecret)
	}
}

func TestDeviceFlowHandler_OversizedIdPResponse(t *testing.T) {
	tests := []struct {
		name       string
		bodySize   int
		wantStatus int
		wantError  string
	}{
		{
			name:       "large but within limit succeeds",
			bodySize:   500 * 1024, // 500KB
			wantStatus: http.StatusOK,
		},
		{
			name:       "oversized response rejected",
			bodySize:   (1 << 20) + 1, // 1MB + 1 byte
			wantStatus: http.StatusBadGateway,
			wantError:  "server_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := map[string]any{
				"access_token": strings.Repeat("a", tt.bodySize),
				"token_type":   "Bearer",
				"expires_in":   3600,
			}

			idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(payload)
			}))
			defer idp.Close()

			cfg := &config.Config{
				DeviceFlowEnabled:     true,
				DeviceFlowIDPTokenURL: idp.URL,
				DeviceFlowClientID:    "test-client",
			}

			handler := NewDeviceFlowHandler(cfg, nil)
			router := handler.Routes()

			form := url.Values{
				"grant_type":  {deviceGrantType},
				"device_code": {"any-code"},
			}
			req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", tt.wantStatus, w.Code, w.Body.String())
			}

			if tt.wantError != "" {
				var errResp map[string]string
				if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
					t.Fatalf("unmarshal error response: %v", err)
				}
				if errResp["error"] != tt.wantError {
					t.Fatalf("expected error %q, got %q", tt.wantError, errResp["error"])
				}
			}
		})
	}
}
