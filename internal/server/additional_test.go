package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

type stubConfigHandler struct {
	err error
}

func (s *stubConfigHandler) Handle(ctx context.Context, data []byte) error {
	_ = ctx
	_ = data
	return s.err
}

func TestMetricsHelpersAndLabels(t *testing.T) {
	beforeDenied := testutil.ToFloat64(PolicyDeniedTotal.WithLabelValues("denylist", "tool.blocked"))
	ObservePolicyDenied("denylist", "tool.blocked")
	afterDenied := testutil.ToFloat64(PolicyDeniedTotal.WithLabelValues("denylist", "tool.blocked"))
	if afterDenied-beforeDenied != 1 {
		t.Fatalf("expected policy denied increment by 1, got %v", afterDenied-beforeDenied)
	}

	beforeActive := testutil.ToFloat64(ActiveRegistrations.WithLabelValues("active"))
	AddActiveRegistrations("active", 2)
	SetActiveRegistrations("active", beforeActive+5)
	currentActive := testutil.ToFloat64(ActiveRegistrations.WithLabelValues("active"))
	if currentActive != beforeActive+5 {
		t.Fatalf("expected active registrations %v, got %v", beforeActive+5, currentActive)
	}

	beforeUpstream := testutil.ToFloat64(UpstreamErrorsTotal.WithLabelValues("backend-a", "timeout"))
	ObserveUpstreamError("backend-a", "timeout")
	afterUpstream := testutil.ToFloat64(UpstreamErrorsTotal.WithLabelValues("backend-a", "timeout"))
	if afterUpstream-beforeUpstream != 1 {
		t.Fatalf("expected upstream error increment by 1, got %v", afterUpstream-beforeUpstream)
	}

	SetCircuitBreakerState("backend-a", "open")
	if got := testutil.ToFloat64(CircuitBreakerState.WithLabelValues("backend-a", "open")); got != 1 {
		t.Fatalf("expected open=1, got %v", got)
	}
	if got := testutil.ToFloat64(CircuitBreakerState.WithLabelValues("backend-a", "closed")); got != 0 {
		t.Fatalf("expected closed=0, got %v", got)
	}
}

func TestObserveToolCallAndNormalizeHelpers(t *testing.T) {
	before := testutil.ToFloat64(ToolCallsTotal.WithLabelValues("unknown", "backend", "success"))
	ObserveToolCall("", "backend", "success", 15*time.Millisecond)
	after := testutil.ToFloat64(ToolCallsTotal.WithLabelValues("unknown", "backend", "success"))
	if after-before != 1 {
		t.Fatalf("expected tool call increment by 1, got %v", after-before)
	}

	if got := normalizePath("   "); got != "/" {
		t.Fatalf("expected normalizePath empty to /, got %q", got)
	}
	if got := normalizeLabel("   ", "fallback"); got != "fallback" {
		t.Fatalf("expected normalizeLabel fallback, got %q", got)
	}
}

func TestMetricsServerSettingsHandler(t *testing.T) {
	successPayload := []byte(`{"config_version":3,"servers":[{"server_name":"svc-a","effective_settings":{"max_concurrency":2}}]}`)
	beforeApplied := testutil.ToFloat64(ServerSettingsAppliedTotal.WithLabelValues("svc-a"))
	beforeVersion := testutil.ToFloat64(ServerSettingsVersion.WithLabelValues("svc-a"))

	handler := &metricsServerSettingsHandler{next: &stubConfigHandler{}}
	if err := handler.Handle(context.Background(), successPayload); err != nil {
		t.Fatalf("unexpected success handler error: %v", err)
	}
	afterApplied := testutil.ToFloat64(ServerSettingsAppliedTotal.WithLabelValues("svc-a"))
	if afterApplied-beforeApplied != 1 {
		t.Fatalf("expected applied increment by 1, got %v", afterApplied-beforeApplied)
	}
	afterVersion := testutil.ToFloat64(ServerSettingsVersion.WithLabelValues("svc-a"))
	if afterVersion < beforeVersion || afterVersion != 3 {
		t.Fatalf("expected settings version 3, got %v", afterVersion)
	}

	failPayload := []byte(`{"config_version":4,"servers":[]}`)
	beforeRejected := testutil.ToFloat64(ServerSettingsRejectedTotal.WithLabelValues("unknown", "apply_failed"))
	err := (&metricsServerSettingsHandler{next: &stubConfigHandler{err: errors.New("apply failed")}}).Handle(context.Background(), failPayload)
	if err == nil {
		t.Fatal("expected error from downstream handler")
	}
	afterRejected := testutil.ToFloat64(ServerSettingsRejectedTotal.WithLabelValues("unknown", "apply_failed"))
	if afterRejected-beforeRejected != 1 {
		t.Fatalf("expected rejected increment by 1, got %v", afterRejected-beforeRejected)
	}
}

func TestServerMiddlewareSettersAndHandlers(t *testing.T) {
	cfg := baseConfig()
	srv := New(cfg, testLogger(), nil)

	var authCalled bool
	var policyCalled bool
	srv.SetProxyAuthMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authCalled = true
			next.ServeHTTP(w, r)
		})
	})
	srv.SetProxyPolicyMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			policyCalled = true
			next.ServeHTTP(w, r)
		})
	})

	srv.MountProxyRoutes(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected mounted proxy route to return 200, got %d", rec.Code)
	}
	if !authCalled || !policyCalled {
		t.Fatalf("expected auth/policy middleware to run, got auth=%t policy=%t", authCalled, policyCalled)
	}

	srv.MountRegistrationRoutes(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	regReq := httptest.NewRequest(http.MethodGet, "/v1/registrations/", nil)
	regRec := httptest.NewRecorder()
	srv.router.ServeHTTP(regRec, regReq)
	if regRec.Code == http.StatusNotFound {
		t.Fatalf("expected registration route to be mounted, got 404")
	}

	var nilServer *Server
	nilReq := httptest.NewRequest(http.MethodGet, "/x", nil)
	nilRec := httptest.NewRecorder()
	nilServer.Handler().ServeHTTP(nilRec, nilReq)
	if nilRec.Code != http.StatusNotFound {
		t.Fatalf("expected nil server handler to return 404, got %d", nilRec.Code)
	}
}
