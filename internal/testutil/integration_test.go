//go:build integration

package testutil

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/cruvero/mcp-gateway/internal/auth"
	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/events"
	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/proxy"
	"github.com/cruvero/mcp-gateway/internal/registration"
	gwserver "github.com/cruvero/mcp-gateway/internal/server"
	storepkg "github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestFullLifecycle(t *testing.T) {
	db := SetupTestDB(t)
	natsConn, natsURL := SetupTestNATS(t)

	if err := natsConn.Flush(); err != nil {
		t.Fatalf("flush nats: %v", err)
	}
	sub, err := natsConn.SubscribeSync("mcpgw.gw-integration.events.>")
	if err != nil {
		t.Fatalf("subscribe lifecycle events: %v", err)
	}

	cfg := &config.Config{
		DBURL:            "postgres://test-db",
		HeartbeatTTL:     2 * time.Second,
		SPIFFEAllowList:  []string{"spiffe://example.org"},
		RateDefault:      10,
		RateBurst:        20,
		CircuitThreshold: 5,
		RetryMax:         3,
		GatewayID:        "gw-integration",
	}

	serverStore := storepkg.NewPostgresServerStore(db)
	auditStore := storepkg.NewPostgresAuditStore(db)
	index := registration.NewCapabilityIndex()

	eventClient, err := events.NewClient(natsURL, cfg.GatewayID)
	if err != nil {
		t.Fatalf("new events client: %v", err)
	}
	defer func() {
		_ = eventClient.Close()
	}()
	publisher := events.NewPublisher(eventClient, cfg.GatewayID, testutilLogger())

	service := registration.NewService(serverStore, auditStore, cfg, testutilLogger())
	service.SetLifecycleEventPublisher(publisher)
	wrapped := &integrationIndexedService{base: service, store: serverStore, index: index}

	sweeper := registration.NewSweeper(serverStore, index, cfg, testutilLogger())
	sweeper.SetLifecycleEventPublisher(publisher)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sweeper.Start(ctx)
	defer sweeper.Stop()

	backendRecord, backendTLSConfig, backendCleanup := buildIntegrationBackend(t)
	defer backendCleanup()

	proxyServer := proxy.NewProxyServer(index, cfg, backendTLSConfig, 2*time.Second, testutilLogger())
	gateway := gwserver.New(cfg, testutilLogger(), nil)
	gateway.MountRegistrationRoutes(registration.NewHandler(wrapped, testutilLogger()).Routes())
	gateway.MountProxyRoutes(proxyServer.Handler())

	httpServer := httptest.NewServer(gateway.Handler())
	defer httpServer.Close()

	certs := GenerateTestCerts(t)
	clientCert := parsePEMCertificate(t, certs.ClientCertPEM)

	// Register with a private RFC 1918 IP to satisfy SSRF validation (loopback
	// is blocked in production). After registration we override the host in
	// the capability index so the proxy routes to the real httptest backend.
	registerPayload := registration.RegistrationRequest{
		ServiceName: backendRecord.Name,
		Version:     "1.0.0",
		Listen: registration.ListenConfig{
			Host:     "10.0.0.1",
			Port:     backendRecord.Port,
			Protocol: "https",
		},
		Capabilities: backendRecord.Capabilities,
		Labels:       map[string]string{"suite": "integration"},
	}
	registerRespBody, registerStatus := callRegistrationEndpoint(t, gateway.Handler(), clientCert, http.MethodPost, "/v1/registrations", registerPayload)
	if registerStatus != http.StatusCreated {
		t.Fatalf("expected register status 201, got %d body=%s", registerStatus, string(registerRespBody))
	}

	var regResp registration.RegistrationResponse
	if err := json.Unmarshal(registerRespBody, &regResp); err != nil {
		t.Fatalf("decode registration response: %v", err)
	}
	if regResp.InstanceID == "" {
		t.Fatal("expected registration instance_id")
	}

	if err := serverStore.UpdateStatus(context.Background(), regResp.InstanceID, types.StatusApproved); err != nil {
		t.Fatalf("approve server status: %v", err)
	}

	heartbeatBody, heartbeatStatus := callRegistrationEndpoint(t, gateway.Handler(), clientCert, http.MethodPost, "/v1/registrations/"+regResp.InstanceID+"/heartbeat", map[string]any{})
	if heartbeatStatus != http.StatusOK {
		t.Fatalf("expected heartbeat status 200, got %d body=%s", heartbeatStatus, string(heartbeatBody))
	}

	var heartbeatResp registration.HeartbeatResponse
	if err := json.Unmarshal(heartbeatBody, &heartbeatResp); err != nil {
		t.Fatalf("decode heartbeat response: %v", err)
	}
	if heartbeatResp.ServerStatus != types.StatusActive {
		t.Fatalf("expected active status after heartbeat, got %s", heartbeatResp.ServerStatus)
	}

	record, err := serverStore.Get(context.Background(), regResp.InstanceID)
	if err != nil {
		t.Fatalf("load registered server: %v", err)
	}
	record.Host = backendRecord.Host
	index.Add(*record)

	mcpGatewayClient, err := mcpclient.NewStreamableHttpClient(httpServer.URL + "/mcp")
	if err != nil {
		t.Fatalf("new mcp gateway client: %v", err)
	}
	defer func() {
		_ = mcpGatewayClient.Close()
	}()

	mcpCtx, mcpCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer mcpCancel()
	if err := mcpGatewayClient.Start(mcpCtx); err != nil {
		t.Fatalf("start mcp gateway client: %v", err)
	}
	if _, err := mcpGatewayClient.Initialize(mcpCtx, mcp.InitializeRequest{Params: mcp.InitializeParams{ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION, ClientInfo: mcp.Implementation{Name: "integration-test", Version: "1.0.0"}}}); err != nil {
		t.Fatalf("initialize mcp gateway client: %v", err)
	}

	tools, err := mcpGatewayClient.ListTools(mcpCtx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list tools through gateway: %v", err)
	}
	if len(tools.Tools) == 0 || tools.Tools[0].Name != "tool.echo" {
		t.Fatalf("expected tool.echo in aggregated tools, got %+v", tools.Tools)
	}

	toolResp, err := mcpGatewayClient.CallTool(mcpCtx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "tool.echo", Arguments: map[string]any{"message": "hello"}}})
	if err != nil {
		t.Fatalf("call tool through gateway: %v", err)
	}
	if len(toolResp.Content) != 1 {
		t.Fatalf("expected one tool content entry, got %d", len(toolResp.Content))
	}
	textContent, ok := toolResp.Content[0].(mcp.TextContent)
	if !ok || textContent.Text != "echo-response" {
		t.Fatalf("unexpected tool result content: %#v", toolResp.Content[0])
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		current, getErr := serverStore.Get(context.Background(), regResp.InstanceID)
		if getErr == nil && current.Status == types.StatusExpired {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	current, err := serverStore.Get(context.Background(), regResp.InstanceID)
	if err != nil {
		t.Fatalf("reload server status: %v", err)
	}
	if current.Status != types.StatusExpired {
		t.Fatalf("expected expired status after missed heartbeats, got %s", current.Status)
	}

	_, deleteStatus := callRegistrationEndpoint(t, gateway.Handler(), clientCert, http.MethodDelete, "/v1/registrations/"+regResp.InstanceID, nil)
	if deleteStatus != http.StatusNoContent {
		t.Fatalf("expected deregister status 204, got %d", deleteStatus)
	}

	eventTypes := map[string]bool{}
	eventDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(eventDeadline) {
		msg, msgErr := sub.NextMsg(100 * time.Millisecond)
		if msgErr != nil {
			continue
		}
		var envelope events.EventEnvelope
		if err := json.Unmarshal(msg.Data, &envelope); err == nil {
			eventTypes[envelope.EventType] = true
		}
		if eventTypes[events.EventServerRegistered] && eventTypes[events.EventServerDeregistered] && eventTypes[events.EventServerHealthChanged] {
			break
		}
	}

	if !eventTypes[events.EventServerRegistered] {
		t.Fatal("expected server.registered event")
	}
	if !eventTypes[events.EventServerHealthChanged] {
		t.Fatal("expected server.health_changed event")
	}
	if !eventTypes[events.EventServerDeregistered] {
		t.Fatal("expected server.deregistered event")
	}
}

func TestAuthFlow(t *testing.T) {
	db := SetupTestDB(t)

	apiKeyStore := storepkg.NewPostgresAPIKeyStore(db)
	plaintext, lookupHash, bcryptHash, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}
	expires := time.Now().Add(1 * time.Hour).UTC()
	if err := apiKeyStore.Create(context.Background(), &types.APIKey{
		Name:          "integration",
		ClientID:      "integration-client",
		Scopes:        []string{"read"},
		ExpiresAt:     &expires,
		KeyLookupHash: lookupHash,
		KeyBcryptHash: bcryptHash,
	}); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	handler := auth.AuthMiddleware(auth.AuthOptions{APIKeyStore: apiKeyStore, Logger: testutilLogger()})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	certs := GenerateTestCerts(t)
	clientCert := parsePEMCertificate(t, certs.ClientCertPEM)

	mtlsReq := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	mtlsReq.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{clientCert}}}
	mtlsReq = mtlsReq.WithContext(identity.WithIdentity(mtlsReq.Context(), &identity.Identity{Type: identity.IdentityMTLS, ID: certs.ClientSPIFFE, Scopes: []string{identity.ScopeAdmin}}))
	mtlsRec := httptest.NewRecorder()
	handler.ServeHTTP(mtlsRec, mtlsReq)
	if mtlsRec.Code != http.StatusOK {
		t.Fatalf("expected mtls auth status 200, got %d", mtlsRec.Code)
	}

	validReq := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	validReq.Header.Set("Authorization", "Bearer "+plaintext)
	validRec := httptest.NewRecorder()
	handler.ServeHTTP(validRec, validReq)
	if validRec.Code != http.StatusOK {
		t.Fatalf("expected valid api key status 200, got %d", validRec.Code)
	}

	invalidReq := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	invalidReq.Header.Set("Authorization", "Bearer mcpgw_invalid")
	invalidRec := httptest.NewRecorder()
	handler.ServeHTTP(invalidRec, invalidReq)
	if invalidRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected invalid api key status 401, got %d", invalidRec.Code)
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	missingRec := httptest.NewRecorder()
	handler.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing auth status 401, got %d", missingRec.Code)
	}
}

type integrationIndexedService struct {
	base  registration.RegistrationService
	store storepkg.ServerStore
	index *registration.CapabilityIndex
}

func (s *integrationIndexedService) Register(ctx context.Context, caller *identity.Identity, req registration.RegistrationRequest) (*registration.RegistrationResponse, error) {
	resp, err := s.base.Register(ctx, caller, req)
	if err != nil {
		return nil, err
	}
	if resp != nil && resp.Status.IsRoutable() {
		if record, getErr := s.store.Get(ctx, resp.InstanceID); getErr == nil {
			s.index.Add(*record)
		}
	}
	return resp, nil
}

func (s *integrationIndexedService) Heartbeat(ctx context.Context, caller *identity.Identity, id string, req registration.HeartbeatRequest) (*registration.HeartbeatResponse, error) {
	resp, err := s.base.Heartbeat(ctx, caller, id, req)
	if err != nil {
		return nil, err
	}
	if resp != nil && resp.ServerStatus.IsRoutable() {
		if record, getErr := s.store.Get(ctx, id); getErr == nil {
			s.index.Add(*record)
		}
	}
	return resp, nil
}

func (s *integrationIndexedService) List(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error) {
	return s.base.List(ctx, filter)
}

func (s *integrationIndexedService) Deregister(ctx context.Context, caller *identity.Identity, id string) error {
	if err := s.base.Deregister(ctx, caller, id); err != nil {
		return err
	}
	s.index.Remove(id)
	return nil
}

func callRegistrationEndpoint(t *testing.T, handler http.Handler, cert *x509.Certificate, method string, path string, payload any) ([]byte, int) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		bytesPayload, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		body = bytes.NewReader(bytesPayload)
	}

	req := httptest.NewRequest(method, path, body)
	req.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{cert}}}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Body.Bytes(), rec.Code
}

func buildIntegrationBackend(t *testing.T) (types.ServerRecord, *tls.Config, func()) {
	t.Helper()

	mcpSrv := mcpserver.NewMCPServer("integration-backend", "1.0.0", mcpserver.WithToolCapabilities(true))
	mcpSrv.AddTool(
		mcp.NewTool("tool.echo", mcp.WithDescription("echo tool"), mcp.WithString("message")),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("echo-response"), nil
		},
	)

	handler := mcpserver.NewStreamableHTTPServer(mcpSrv)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	backend := httptest.NewTLSServer(mux)

	parsedURL, err := url.Parse(backend.URL)
	if err != nil {
		backend.Close()
		t.Fatalf("parse backend url: %v", err)
	}
	host := parsedURL.Hostname()
	portText := parsedURL.Port()
	port := 443
	if portText != "" {
		fmt.Sscanf(portText, "%d", &port)
	}

	rootPool := x509.NewCertPool()
	rootPool.AddCert(backend.Certificate())
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: rootPool}

	record := types.ServerRecord{
		ID:   "integration-backend-1",
		Name: "integration-backend",
		Host: host,
		Port: port,
		Capabilities: types.Capability{
			Tools: []string{"tool.echo"},
		},
		Status: types.StatusActive,
	}

	cleanup := func() {
		backend.Close()
	}
	return record, tlsCfg, cleanup
}

func parsePEMCertificate(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("decode certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}

func testutilLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
