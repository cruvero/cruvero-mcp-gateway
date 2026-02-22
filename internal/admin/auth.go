package admin

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/cruvero/mcp-gateway/internal/config"
	"golang.org/x/oauth2"
)

const (
	stateCookieName   = "__mcpgw_state"
	sessionCookieName = "__mcpgw_admin"
	pkceVerifierLen   = 64
	errInternalError  = "internal error"
	adminPathPrefix   = "/admin/"
)

// AdminSession holds the decrypted session data.
type AdminSession struct {
	Subject   string    `json:"sub"`
	Email     string    `json:"email"`
	Scopes    []string  `json:"scopes"`
	CSRFToken string    `json:"csrf_token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// AdminAuth handles OIDC Authorization Code + PKCE flow for the admin dashboard.
type AdminAuth struct {
	cfg          *config.Config
	logger       *slog.Logger
	provider     *oidc.Provider
	oauth2Config oauth2.Config
	verifier     *oidc.IDTokenVerifier
	aead         cipher.AEAD
	sessionTTL   time.Duration
}

// NewAdminAuth initializes OIDC discovery and builds the admin auth handler.
func NewAdminAuth(cfg *config.Config, logger *slog.Logger) (*AdminAuth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("new admin auth: config is nil")
	}
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	provider, err := oidc.NewProvider(ctx, cfg.OIDCIssuer)
	if err != nil {
		return nil, fmt.Errorf("new admin auth: discover oidc provider: %w", err)
	}

	callbackURL := strings.TrimRight(cfg.ListenAddr, "/") + "/admin/callback"
	if strings.HasPrefix(cfg.ListenAddr, ":") {
		callbackURL = "/admin/callback"
	}

	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.AdminOIDCClientID,
		ClientSecret: cfg.AdminOIDCClientSecret,
		RedirectURL:  callbackURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.AdminOIDCClientID})

	block, err := aes.NewCipher(cfg.AdminSessionKey[:])
	if err != nil {
		return nil, fmt.Errorf("new admin auth: create aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new admin auth: create gcm: %w", err)
	}

	sessionTTL := cfg.AdminSessionTTL
	if sessionTTL <= 0 {
		sessionTTL = 8 * time.Hour
	}

	return &AdminAuth{
		cfg:          cfg,
		logger:       logger,
		provider:     provider,
		oauth2Config: oauth2Cfg,
		verifier:     verifier,
		aead:         aead,
		sessionTTL:   sessionTTL,
	}, nil
}

// HandleLogin initiates the OIDC Authorization Code + PKCE flow.
func (a *AdminAuth) HandleLogin(w http.ResponseWriter, r *http.Request) {
	verifier, err := generatePKCEVerifier()
	if err != nil {
		a.logger.Error("generate PKCE verifier failed", slog.String("error", err.Error()))
		http.Error(w, errInternalError, http.StatusInternalServerError)
		return
	}
	challenge := pkceS256Challenge(verifier)

	state, err := a.generateState(verifier)
	if err != nil {
		a.logger.Error("generate state failed", slog.String("error", err.Error()))
		http.Error(w, errInternalError, http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     adminPathPrefix,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   600,
	})

	authURL := a.oauth2Config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// HandleCallback handles the OIDC callback after user authentication.
func (a *AdminAuth) HandleCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil {
		http.Error(w, "missing state cookie", http.StatusBadRequest)
		return
	}

	queryState := r.URL.Query().Get("state")
	if queryState != stateCookie.Value {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}

	verifier, err := a.extractVerifierFromState(stateCookie.Value)
	if err != nil {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	// Clear state cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     adminPathPrefix,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})

	code := r.URL.Query().Get("code")
	if strings.TrimSpace(code) == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}

	token, err := a.oauth2Config.Exchange(r.Context(), code,
		oauth2.SetAuthURLParam("code_verifier", verifier),
	)
	if err != nil {
		a.logger.Error("token exchange failed", slog.String("error", err.Error()))
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "missing id_token in response", http.StatusInternalServerError)
		return
	}

	idToken, err := a.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		a.logger.Error("id_token verification failed", slog.String("error", err.Error()))
		http.Error(w, "id_token verification failed", http.StatusUnauthorized)
		return
	}

	var claims struct {
		Subject string   `json:"sub"`
		Email   string   `json:"email"`
		Groups  []string `json:"groups"`
		Scope   string   `json:"scope"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "parse claims failed", http.StatusInternalServerError)
		return
	}

	scopes := mergeScopes(claims.Groups, claims.Scope)
	if !hasScope(scopes, a.cfg.AdminRequiredScope) {
		http.Error(w, "insufficient permissions: missing "+a.cfg.AdminRequiredScope+" scope", http.StatusForbidden)
		return
	}

	csrfToken, err := generateRandomBase64(32)
	if err != nil {
		http.Error(w, errInternalError, http.StatusInternalServerError)
		return
	}

	session := &AdminSession{
		Subject:   claims.Subject,
		Email:     claims.Email,
		Scopes:    scopes,
		CSRFToken: csrfToken,
		ExpiresAt: time.Now().Add(a.sessionTTL),
	}

	encrypted, err := a.encryptSession(session)
	if err != nil {
		a.logger.Error("encrypt session failed", slog.String("error", err.Error()))
		http.Error(w, errInternalError, http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    encrypted,
		Path:     adminPathPrefix,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(a.sessionTTL.Seconds()),
	})

	a.logger.Info("admin login", slog.String("sub", claims.Subject), slog.String("email", claims.Email))
	http.Redirect(w, r, adminPathPrefix, http.StatusFound)
}

// DecryptSession decrypts and validates the admin session cookie.
func (a *AdminAuth) DecryptSession(cookieValue string) (*AdminSession, error) {
	data, err := base64.URLEncoding.DecodeString(cookieValue)
	if err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}

	if len(data) < a.aead.NonceSize() {
		return nil, fmt.Errorf("session data too short")
	}

	nonce := data[:a.aead.NonceSize()]
	ciphertext := data[a.aead.NonceSize():]

	plaintext, err := a.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt session: %w", err)
	}

	var session AdminSession
	if err := json.Unmarshal(plaintext, &session); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}

	if time.Now().After(session.ExpiresAt) {
		return nil, fmt.Errorf("session expired")
	}

	return &session, nil
}

func (a *AdminAuth) encryptSession(session *AdminSession) (string, error) {
	plaintext, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("marshal session: %w", err)
	}

	nonce := make([]byte, a.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	sealed := a.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.URLEncoding.EncodeToString(sealed), nil
}

func (a *AdminAuth) generateState(verifier string) (string, error) {
	payload := map[string]string{"verifier": verifier}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, a.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	sealed := a.aead.Seal(nonce, nonce, data, nil)
	return base64.URLEncoding.EncodeToString(sealed), nil
}

func (a *AdminAuth) extractVerifierFromState(state string) (string, error) {
	data, err := base64.URLEncoding.DecodeString(state)
	if err != nil {
		return "", err
	}

	if len(data) < a.aead.NonceSize() {
		return "", fmt.Errorf("state data too short")
	}

	nonce := data[:a.aead.NonceSize()]
	ciphertext := data[a.aead.NonceSize():]

	plaintext, err := a.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	var payload struct {
		Verifier string `json:"verifier"`
	}
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return "", err
	}

	return payload.Verifier, nil
}

func generatePKCEVerifier() (string, error) {
	b := make([]byte, pkceVerifierLen)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("generate pkce verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func pkceS256Challenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func generateRandomBase64(n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func mergeScopes(groups []string, scopeClaim string) []string {
	seen := make(map[string]struct{})
	var scopes []string
	for _, g := range groups {
		trimmed := strings.TrimSpace(g)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		scopes = append(scopes, trimmed)
	}
	for _, s := range strings.Fields(scopeClaim) {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		scopes = append(scopes, trimmed)
	}
	return scopes
}

func hasScope(scopes []string, required string) bool {
	for _, s := range scopes {
		if s == required {
			return true
		}
	}
	return false
}
