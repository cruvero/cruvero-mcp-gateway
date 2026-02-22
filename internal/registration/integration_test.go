package registration

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
	identitypkg "github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/server"
	"github.com/cruvero/mcp-gateway/internal/testutil"
	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/go-chi/chi/v5"
)

func TestRegistrationFlowMountedInServer(t *testing.T) {
	t.Parallel()

	certs := testutil.GenerateTestCerts(t)
	clientCert := parseClientCert(t, certs.ClientCertPEM)

	store := newInMemoryServerStore()
	svc := NewService(store, &mockAuditStore{}, &config.Config{HeartbeatTTL: 30 * time.Second, SPIFFEAllowList: []string{"spiffe://example.org"}}, testIntegrationLogger())
	handler := NewHandler(svc, testIntegrationLogger())

	srv := server.New(&config.Config{SPIFFEAllowList: []string{"spiffe://example.org"}}, testIntegrationLogger(), nil)
	srv.MountRegistrationRoutes(handler.Routes())

	registerBody := `{"service_name":"svc-alpha","version":"1.0.0","listen":{"host":"svc-alpha.default.svc","port":8080,"protocol":"https"},"capabilities":{"tools":["tool.alpha"],"resources":[],"prompts":[]},"labels":{"team":"platform"}}`
	registerReq := httptest.NewRequest(http.MethodPost, "/v1/registrations", bytes.NewBufferString(registerBody))
	registerReq.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{clientCert}}}
	registerRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(registerRec, registerReq)

	if registerRec.Code != http.StatusCreated {
		t.Fatalf("expected registration status 201, got %d", registerRec.Code)
	}

	var regResp RegistrationResponse
	if err := json.Unmarshal(registerRec.Body.Bytes(), &regResp); err != nil {
		t.Fatalf("decode registration response: %v", err)
	}
	if regResp.InstanceID == "" {
		t.Fatal("expected instance_id in registration response")
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/registrations", nil)
	listReq.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{clientCert}}}
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list status 200, got %d", listRec.Code)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/v1/registrations/"+regResp.InstanceID, nil)
	deleteReq.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{clientCert}}}
	deleteRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("expected deregister status 204, got %d", deleteRec.Code)
	}
}

func TestRegistrationAdminScopeAndOwnershipChecks(t *testing.T) {
	t.Parallel()

	store := newInMemoryServerStore()
	svc := NewService(store, &mockAuditStore{}, &config.Config{HeartbeatTTL: 30 * time.Second, SPIFFEAllowList: []string{"spiffe://example.org"}}, testIntegrationLogger())

	owner := &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/server-owner", Scopes: []string{identitypkg.ScopeRead}}
	resp, err := svc.Register(context.Background(), owner, RegistrationRequest{
		ServiceName: "svc-alpha",
		Version:     "1.0.0",
		Listen:      ListenConfig{Host: "svc-alpha.default.svc", Port: 8080, Protocol: "https"},
		Capabilities: types.Capability{
			Tools: []string{"tool.alpha"},
		},
	})
	if err != nil {
		t.Fatalf("seed registration: %v", err)
	}

	h := NewHandler(svc, testIntegrationLogger())
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/other", Scopes: []string{identitypkg.ScopeRead}}
			next.ServeHTTP(w, r.WithContext(identitypkg.WithIdentity(r.Context(), id)))
		})
	})
	router.Mount("/v1/registrations", h.Routes())

	listReq := httptest.NewRequest(http.MethodGet, "/v1/registrations", nil)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusForbidden {
		t.Fatalf("expected list status 403 without admin scope, got %d", listRec.Code)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/v1/registrations/"+resp.InstanceID, nil)
	deleteRec := httptest.NewRecorder()
	router.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusForbidden {
		t.Fatalf("expected deregister status 403 for non-matching identity, got %d", deleteRec.Code)
	}
}

type inMemoryServerStore struct {
	mu      sync.RWMutex
	records map[string]types.ServerRecord
}

func newInMemoryServerStore() *inMemoryServerStore {
	return &inMemoryServerStore{records: make(map[string]types.ServerRecord)}
}

func (s *inMemoryServerStore) Create(ctx context.Context, record *types.ServerRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[record.ID] = *record
	return nil
}

func (s *inMemoryServerStore) Get(ctx context.Context, id string) (*types.ServerRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	copyRecord := record
	return &copyRecord, nil
}

func (s *inMemoryServerStore) GetByName(ctx context.Context, name string) (*types.ServerRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, record := range s.records {
		if record.Name == name {
			copyRecord := record
			return &copyRecord, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (s *inMemoryServerStore) GetBySPIFFEID(ctx context.Context, spiffeID string) (*types.ServerRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, record := range s.records {
		if record.SPIFFEID == spiffeID {
			copyRecord := record
			return &copyRecord, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (s *inMemoryServerStore) List(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]types.ServerRecord, 0, len(s.records))
	for _, record := range s.records {
		if filter.Status != nil && record.Status != *filter.Status {
			continue
		}
		if filter.NamePattern != "" && !bytes.Contains([]byte(record.Name), []byte(filter.NamePattern)) {
			continue
		}
		out = append(out, record)
	}
	return out, nil
}

func (s *inMemoryServerStore) Update(ctx context.Context, record *types.ServerRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[record.ID]; !ok {
		return sql.ErrNoRows
	}
	s.records[record.ID] = *record
	return nil
}

func (s *inMemoryServerStore) UpdateStatus(ctx context.Context, id string, status types.ServerStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return sql.ErrNoRows
	}
	record.Status = status
	s.records[id] = record
	return nil
}

func (s *inMemoryServerStore) UpdateHeartbeat(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return sql.ErrNoRows
	}
	now := time.Now().UTC()
	record.LastHeartbeat = &now
	s.records[id] = record
	return nil
}

func (s *inMemoryServerStore) AcknowledgeRegistration(
	ctx context.Context,
	id string,
	leaseEpoch int64,
	capabilityHash string,
	ackVersion string,
	ackedAt time.Time,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return sql.ErrNoRows
	}
	if record.LeaseEpoch != leaseEpoch {
		return sql.ErrNoRows
	}
	if strings.TrimSpace(capabilityHash) != "" && strings.TrimSpace(record.CapabilityHash) != strings.TrimSpace(capabilityHash) {
		return sql.ErrNoRows
	}
	record.SyncState = types.SyncStateAcked
	record.LastPlatformAckVersion = strings.TrimSpace(ackVersion)
	ack := ackedAt.UTC()
	record.LastPlatformAckAt = &ack
	s.records[id] = record
	return nil
}

func (s *inMemoryServerStore) UpdateRateLimit(_ context.Context, _ string, _, _ *int) error {
	return nil
}

func (s *inMemoryServerStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, id)
	return nil
}

func (s *inMemoryServerStore) ListStale(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
	return nil, nil
}

func (s *inMemoryServerStore) ListExpired(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
	return nil, nil
}

func parseClientCert(t *testing.T, pemBytes []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatal("decode client cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse client cert: %v", err)
	}
	return cert
}

func testIntegrationLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
