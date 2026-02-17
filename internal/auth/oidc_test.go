package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/golang-jwt/jwt/v5"
)

func TestOIDCValidateSuccessAndClaimsExtraction(t *testing.T) {
	t.Parallel()

	provider := newOIDCTestProvider(t)
	defer provider.Close()

	ctx := context.Background()
	validator, err := NewOIDCValidator(ctx, provider.Issuer(), "mcpgw")
	if err != nil {
		t.Fatalf("new oidc validator: %v", err)
	}

	token := provider.IssueToken(t, "mcpgw", time.Now().Add(10*time.Minute), map[string]any{
		"sub":    "user-123",
		"email":  "user@example.com",
		"groups": []string{"read", "admin"},
		"scope":  "write read",
	})

	id, err := validator.Validate(ctx, token)
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}
	if id.Type != identity.IdentityOIDC {
		t.Fatalf("expected oidc identity type, got %q", id.Type)
	}
	if id.ID != "user-123" {
		t.Fatalf("expected subject user-123, got %q", id.ID)
	}
	if !id.HasScope("read") || !id.HasScope("write") || !id.HasScope("admin") {
		t.Fatalf("expected merged group/scope claims, got %+v", id.Scopes)
	}
	if id.Metadata["email"] != "user@example.com" {
		t.Fatalf("expected email metadata, got %+v", id.Metadata)
	}
}

func TestOIDCValidateErrors(t *testing.T) {
	t.Parallel()

	provider := newOIDCTestProvider(t)
	defer provider.Close()

	ctx := context.Background()
	validator, err := NewOIDCValidator(ctx, provider.Issuer(), "mcpgw")
	if err != nil {
		t.Fatalf("new oidc validator: %v", err)
	}

	if _, err := validator.Validate(ctx, "not-a-token"); err == nil {
		t.Fatal("expected error for invalid token")
	}

	expiredToken := provider.IssueToken(t, "mcpgw", time.Now().Add(-1*time.Minute), map[string]any{
		"sub": "user-123",
	})
	if _, err := validator.Validate(ctx, expiredToken); err == nil {
		t.Fatal("expected error for expired token")
	}

	wrongAudienceToken := provider.IssueToken(t, "other-audience", time.Now().Add(5*time.Minute), map[string]any{
		"sub": "user-123",
	})
	if _, err := validator.Validate(ctx, wrongAudienceToken); err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestOIDCMiddleware(t *testing.T) {
	t.Parallel()

	provider := newOIDCTestProvider(t)
	defer provider.Close()

	ctx := context.Background()
	validator, err := NewOIDCValidator(ctx, provider.Issuer(), "mcpgw")
	if err != nil {
		t.Fatalf("new oidc validator: %v", err)
	}

	validToken := provider.IssueToken(t, "mcpgw", time.Now().Add(10*time.Minute), map[string]any{
		"sub": "user-123",
	})

	handler := OIDCMiddleware(validator, testOIDCLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := identity.FromContext(r.Context())
		if !ok {
			t.Fatal("expected identity in request context")
		}
		if id.Type != identity.IdentityOIDC {
			t.Fatalf("expected oidc identity type, got %q", id.Type)
		}
		w.WriteHeader(http.StatusOK)
	}))

	validReq := httptest.NewRequest(http.MethodGet, "/protected", nil)
	validReq.Header.Set("Authorization", "Bearer "+validToken)
	validRec := httptest.NewRecorder()
	handler.ServeHTTP(validRec, validReq)
	if validRec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", validRec.Code)
	}

	invalidReq := httptest.NewRequest(http.MethodGet, "/protected", nil)
	invalidReq.Header.Set("Authorization", "Bearer invalid")
	invalidRec := httptest.NewRecorder()
	handler.ServeHTTP(invalidRec, invalidReq)
	if invalidRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", invalidRec.Code)
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/protected", nil)
	missingRec := httptest.NewRecorder()
	handler.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", missingRec.Code)
	}
}

type oidcTestProvider struct {
	server  *httptest.Server
	keyID   string
	private *rsa.PrivateKey
}

func newOIDCTestProvider(t *testing.T) *oidcTestProvider {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	provider := &oidcTestProvider{
		keyID:   "test-key-1",
		private: privateKey,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":   provider.Issuer(),
			"jwks_uri": provider.Issuer() + "/keys",
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		pub := provider.private.PublicKey
		n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{
					"kty": "RSA",
					"use": "sig",
					"alg": "RS256",
					"kid": provider.keyID,
					"n":   n,
					"e":   e,
				},
			},
		})
	})

	provider.server = httptest.NewServer(mux)
	return provider
}

func (p *oidcTestProvider) Issuer() string {
	if p.server == nil {
		return ""
	}
	return p.server.URL
}

func (p *oidcTestProvider) Close() {
	if p.server != nil {
		p.server.Close()
	}
}

func (p *oidcTestProvider) IssueToken(t *testing.T, audience string, expiry time.Time, extraClaims map[string]any) string {
	t.Helper()

	claims := jwt.MapClaims{
		"iss": p.Issuer(),
		"aud": audience,
		"exp": expiry.Unix(),
		"iat": time.Now().Add(-1 * time.Minute).Unix(),
		"nbf": time.Now().Add(-1 * time.Minute).Unix(),
	}
	for key, value := range extraClaims {
		claims[key] = value
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = p.keyID

	rawToken, err := token.SignedString(p.private)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return rawToken
}

func testOIDCLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func TestNewOIDCValidatorErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if _, err := NewOIDCValidator(ctx, "", "mcpgw"); err == nil {
		t.Fatal("expected error for empty issuer URL")
	}
	if _, err := NewOIDCValidator(ctx, "http://issuer", ""); err == nil {
		t.Fatal("expected error for empty audience")
	}
	if _, err := NewOIDCValidator(ctx, "http://127.0.0.1:1", "mcpgw"); err == nil {
		t.Fatal("expected provider discovery error")
	}
}

func TestOIDCMiddlewareValidatorNotConfigured(t *testing.T) {
	t.Parallel()

	handler := OIDCMiddleware(nil, testOIDCLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+fmt.Sprintf("%d", time.Now().UnixNano()))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
}
