package auth

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/go-chi/chi/v5"
)

const (
	deviceFlowMaxBodyBytes        = 4096      // incoming client request bodies (form data)
	deviceFlowMaxIdPResponseBytes = 1 << 20   // 1MB ceiling for IdP response bodies
	deviceGrantType               = "urn:ietf:params:oauth:grant-type:device_code"
	contentTypeJSON               = "application/json"
	headerContentType             = "Content-Type"
)

var verifyPageTemplate = template.Must(template.New("verify").Parse(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>Device Verification</title></head>
<body>
<h1>Device Verification</h1>
<p>Enter this code on your device:</p>
<pre style="font-size:2em;letter-spacing:0.3em">{{.UserCode}}</pre>
{{if .VerificationURI}}<p><a href="{{.VerificationURI}}">Open verification page</a></p>{{end}}
</body>
</html>`))

// DeviceFlowHandler handles OAuth2 Device Authorization Grant endpoints.
type DeviceFlowHandler struct {
	cfg    *config.Config
	logger *slog.Logger
	client *http.Client
}

// NewDeviceFlowHandler creates a handler for device code flow endpoints.
func NewDeviceFlowHandler(cfg *config.Config, logger *slog.Logger) *DeviceFlowHandler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &DeviceFlowHandler{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{},
	}
}

// Routes returns a chi router with device flow endpoints.
func (h *DeviceFlowHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/code", h.handleDeviceCode)
	r.Post("/token", h.handleDeviceToken)
	r.Get("/verify", h.handleVerify)
	return r
}

func (h *DeviceFlowHandler) handleDeviceCode(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, deviceFlowMaxBodyBytes)

	if err := r.ParseForm(); err != nil {
		writeDeviceError(w, http.StatusBadRequest, "invalid_request", "failed to parse request body")
		return
	}

	scope := strings.TrimSpace(r.FormValue("scope"))
	if scope == "" || !strings.Contains(scope, "openid") {
		writeDeviceError(w, http.StatusBadRequest, "invalid_scope", "scope must include openid")
		return
	}

	form := url.Values{}
	form.Set("client_id", h.cfg.DeviceFlowClientID)
	form.Set("scope", scope)

	resp, err := h.client.PostForm(h.cfg.DeviceFlowIDPDeviceURL, form)
	if err != nil {
		h.logger.Error("device code idp request failed", slog.String("error", err.Error()))
		writeDeviceError(w, http.StatusBadGateway, "server_error", "failed to contact identity provider")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeDeviceError(w, http.StatusBadGateway, "server_error", "failed to read identity provider response")
		return
	}

	h.logger.Debug("idp device code response", slog.Int("bytes", len(body)), slog.Int("status", resp.StatusCode))

	if len(body) > deviceFlowMaxIdPResponseBytes {
		h.logger.Error("idp response too large", slog.Int("bytes", len(body)),
			slog.Int("max", deviceFlowMaxIdPResponseBytes))
		writeDeviceError(w, http.StatusBadGateway, "server_error", "identity provider response too large")
		return
	}

	w.Header().Set(headerContentType, contentTypeJSON)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

func (h *DeviceFlowHandler) handleDeviceToken(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, deviceFlowMaxBodyBytes)

	if err := r.ParseForm(); err != nil {
		writeDeviceError(w, http.StatusBadRequest, "invalid_request", "failed to parse request body")
		return
	}

	grantType := strings.TrimSpace(r.FormValue("grant_type"))

	var form url.Values
	switch grantType {
	case deviceGrantType:
		deviceCode := strings.TrimSpace(r.FormValue("device_code"))
		if deviceCode == "" {
			writeDeviceError(w, http.StatusBadRequest, "invalid_request", "device_code is required")
			return
		}
		form = url.Values{
			"client_id":   {h.cfg.DeviceFlowClientID},
			"grant_type":  {deviceGrantType},
			"device_code": {deviceCode},
		}
	case "refresh_token":
		refreshToken := strings.TrimSpace(r.FormValue("refresh_token"))
		if refreshToken == "" {
			writeDeviceError(w, http.StatusBadRequest, "invalid_request", "refresh_token is required")
			return
		}
		form = url.Values{
			"client_id":     {h.cfg.DeviceFlowClientID},
			"grant_type":    {"refresh_token"},
			"refresh_token": {refreshToken},
		}
	default:
		writeDeviceError(w, http.StatusBadRequest, "unsupported_grant_type",
			fmt.Sprintf("grant_type must be %s or refresh_token", deviceGrantType))
		return
	}

	if strings.TrimSpace(h.cfg.DeviceFlowClientSecret) != "" {
		form.Set("client_secret", h.cfg.DeviceFlowClientSecret)
	}

	resp, err := h.client.PostForm(h.cfg.DeviceFlowIDPTokenURL, form)
	if err != nil {
		h.logger.Error("device token idp request failed", slog.String("error", err.Error()))
		writeDeviceError(w, http.StatusBadGateway, "server_error", "failed to contact identity provider")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeDeviceError(w, http.StatusBadGateway, "server_error", "failed to read identity provider response")
		return
	}

	h.logger.Debug("idp token response", slog.Int("bytes", len(body)), slog.Int("status", resp.StatusCode))

	if len(body) > deviceFlowMaxIdPResponseBytes {
		h.logger.Error("idp response too large", slog.Int("bytes", len(body)),
			slog.Int("max", deviceFlowMaxIdPResponseBytes))
		writeDeviceError(w, http.StatusBadGateway, "server_error", "identity provider response too large")
		return
	}

	// Map IdP status codes to appropriate gateway responses.
	statusCode := resp.StatusCode
	if statusCode == http.StatusBadRequest {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil {
			switch errResp.Error {
			case "authorization_pending", "slow_down":
				statusCode = http.StatusTooEarly
			case "expired_token":
				statusCode = http.StatusGone
			case "access_denied":
				statusCode = http.StatusForbidden
			}
		}
	}

	if statusCode == http.StatusOK {
		h.logger.Info("device flow token issued", slog.String("grant_type", grantType))
	}

	w.Header().Set(headerContentType, contentTypeJSON)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}

func (h *DeviceFlowHandler) handleVerify(w http.ResponseWriter, r *http.Request) {
	userCode := strings.TrimSpace(r.URL.Query().Get("user_code"))
	if userCode == "" {
		writeDeviceError(w, http.StatusBadRequest, "invalid_request", "user_code is required")
		return
	}
	verificationURI := strings.TrimSpace(r.URL.Query().Get("verification_uri"))

	data := struct {
		UserCode        string
		VerificationURI string
	}{
		UserCode:        userCode,
		VerificationURI: verificationURI,
	}

	w.Header().Set(headerContentType, "text/html; charset=utf-8")
	if err := verifyPageTemplate.Execute(w, data); err != nil {
		h.logger.Error("render verify page failed", slog.String("error", err.Error()))
	}
}

func writeDeviceError(w http.ResponseWriter, status int, errorCode, description string) {
	w.Header().Set(headerContentType, contentTypeJSON)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             errorCode,
		"error_description": description,
	})
}
