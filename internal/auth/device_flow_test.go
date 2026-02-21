package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
