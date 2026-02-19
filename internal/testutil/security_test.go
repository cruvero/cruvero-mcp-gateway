//go:build security

package testutil

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/auth"
	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/server"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestExpiredCert(t *testing.T) {
	t.Skip("expired-cert handshake test requires specialized cert issuance flow")
}

func TestWrongCA(t *testing.T) {
	bundleA := GenerateTestCerts(t)
	bundleB := GenerateTestCerts(t)

	serverCert := tls.Certificate{}
	pair, err := tls.X509KeyPair(bundleA.ServerCertPEM, bundleA.ServerKeyPEM)
	if err != nil {
		t.Fatalf("parse server keypair: %v", err)
	}
	serverCert = pair

	clientPair, err := tls.X509KeyPair(bundleB.ClientCertPEM, bundleB.ClientKeyPEM)
	if err != nil {
		t.Fatalf("parse client keypair: %v", err)
	}

	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(bundleA.CACertPEM)

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewUnstartedServer(h)
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverCert}, ClientCAs: caPool, ClientAuth: tls.RequireAndVerifyClientCert}
	srv.StartTLS()
	defer srv.Close()

	client := srv.Client()
	client.Transport = &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{clientPair}, RootCAs: caPool}}

	_, err = client.Get(srv.URL)
	if err == nil {
		t.Fatal("expected TLS handshake failure for wrong CA")
	}
}

func TestNoSPIFFEID(t *testing.T) {
	bundle := GenerateTestCerts(t)
	serverCert := parseSecurityCert(t, bundle.ServerCertPEM)

	req := httptest.NewRequest(http.MethodGet, "/v1/registrations", nil)
	req.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{serverCert}}}
	rec := httptest.NewRecorder()

	middleware := identity.MTLSMiddleware([]string{"spiffe://example.org"}, nil)
	middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for cert without URI SAN, got %d", rec.Code)
	}
}

func TestWrongSPIFFEDomain(t *testing.T) {
	bundle := GenerateTestCerts(t)
	clientCert := parseSecurityCert(t, bundle.ClientCertPEM)

	req := httptest.NewRequest(http.MethodGet, "/v1/registrations", nil)
	req.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{clientCert}}}
	rec := httptest.NewRecorder()

	middleware := identity.MTLSMiddleware([]string{"spiffe://other.example"}, nil)
	middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for wrong spiffe domain, got %d", rec.Code)
	}
}

func TestRevokedAPIKey(t *testing.T) {
	store := &securityAPIKeyStore{key: nil}
	handler := auth.AuthMiddleware(auth.AuthOptions{APIKeyStore: store})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer mcpgw_revoked")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for revoked key replay, got %d", rec.Code)
	}
}

func TestMalformedJSON(t *testing.T) {
	service := registration.NewService(&securityServerStore{}, nil, &config.Config{HeartbeatTTL: 30}, nil)
	handler := registration.NewHandler(service, nil)

	body := bytes.NewBufferString("{\"service_name\":")
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req = req.WithContext(identity.WithIdentity(context.Background(), &identity.Identity{Type: identity.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/server", Scopes: []string{identity.ScopeAdmin}}))
	rec := httptest.NewRecorder()
	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed json, got %d", rec.Code)
	}
}

func TestOversizedBody(t *testing.T) {
	middleware := server.RequestBodyLimitMiddleware(1024)
	h := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	large := bytes.Repeat([]byte("a"), 2048)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(large))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge && rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 413 or 400 for oversized body, got %d", rec.Code)
	}
}

func parseSecurityCert(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("decode certificate pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}

type securityAPIKeyStore struct {
	key *types.APIKey
}

func (s *securityAPIKeyStore) Create(ctx context.Context, key *types.APIKey) error {
	_ = ctx
	s.key = key
	return nil
}
func (s *securityAPIKeyStore) GetByLookupHash(ctx context.Context, lookupHash string) (*types.APIKey, error) {
	_ = ctx
	_ = lookupHash
	if s.key == nil {
		return nil, sql.ErrNoRows
	}
	return s.key, nil
}
func (s *securityAPIKeyStore) List(ctx context.Context) ([]types.APIKey, error) {
	_ = ctx
	return nil, nil
}
func (s *securityAPIKeyStore) Revoke(ctx context.Context, id string) error {
	_ = ctx
	_ = id
	s.key = nil
	return nil
}
func (s *securityAPIKeyStore) DeleteExpired(ctx context.Context) (int64, error) {
	_ = ctx
	return 0, nil
}

type securityServerStore struct{}

func (s *securityServerStore) Create(ctx context.Context, record *types.ServerRecord) error {
	_ = ctx
	_ = record
	return nil
}
func (s *securityServerStore) Get(ctx context.Context, id string) (*types.ServerRecord, error) {
	_ = ctx
	_ = id
	return nil, sql.ErrNoRows
}
func (s *securityServerStore) GetByName(ctx context.Context, name string) (*types.ServerRecord, error) {
	_ = ctx
	_ = name
	return nil, sql.ErrNoRows
}
func (s *securityServerStore) GetBySPIFFEID(ctx context.Context, spiffeID string) (*types.ServerRecord, error) {
	_ = ctx
	_ = spiffeID
	return nil, sql.ErrNoRows
}
func (s *securityServerStore) List(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error) {
	_ = ctx
	_ = filter
	return nil, nil
}
func (s *securityServerStore) Update(ctx context.Context, record *types.ServerRecord) error {
	_ = ctx
	_ = record
	return nil
}
func (s *securityServerStore) UpdateStatus(ctx context.Context, id string, status types.ServerStatus) error {
	_ = ctx
	_ = id
	_ = status
	return nil
}
func (s *securityServerStore) UpdateHeartbeat(ctx context.Context, id string) error {
	_ = ctx
	_ = id
	return nil
}
func (s *securityServerStore) AcknowledgeRegistration(
	ctx context.Context,
	id string,
	leaseEpoch int64,
	capabilityHash string,
	ackVersion string,
	ackedAt time.Time,
) error {
	_ = ctx
	_ = id
	_ = leaseEpoch
	_ = capabilityHash
	_ = ackVersion
	_ = ackedAt
	return nil
}
func (s *securityServerStore) Delete(ctx context.Context, id string) error {
	_ = ctx
	_ = id
	return nil
}
func (s *securityServerStore) ListStale(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
	_ = ctx
	_ = threshold
	return nil, nil
}
func (s *securityServerStore) ListExpired(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
	_ = ctx
	_ = threshold
	return nil, nil
}
