package server

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsMiddlewareIncrementsCounters(t *testing.T) {
	t.Parallel()

	handler := MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	beforeCount := testutil.ToFloat64(HTTPRequestsTotal.WithLabelValues("GET", "/metrics-test", "418"))
	req := httptest.NewRequest(http.MethodGet, "/metrics-test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	afterCount := testutil.ToFloat64(HTTPRequestsTotal.WithLabelValues("GET", "/metrics-test", "418"))

	if afterCount-beforeCount != 1 {
		t.Fatalf("expected request counter increment by 1, got delta=%v", afterCount-beforeCount)
	}
}

func TestStartMetricsServerExposesMetricsEndpoint(t *testing.T) {
	t.Parallel()

	HTTPRequestsTotal.WithLabelValues("GET", "/readyz", "200").Inc()
	HTTPRequestDuration.WithLabelValues("GET", "/readyz").Observe(0.01)
	RateLimitedTotal.WithLabelValues("client", "/mcp").Inc()
	PolicyDeniedTotal.WithLabelValues("denylist", "tool.x").Inc()
	ActiveRegistrations.WithLabelValues("active").Set(1)
	UpstreamErrorsTotal.WithLabelValues("backend-a", "upstream_call").Inc()
	CircuitBreakerState.WithLabelValues("backend-a", "closed").Set(1)
	NATSConnected.Set(1)
	ServerSettingsAppliedTotal.WithLabelValues("svc-a").Inc()
	ServerSettingsRejectedTotal.WithLabelValues("svc-a", "apply_failed").Inc()
	ServerSettingsVersion.WithLabelValues("svc-a").Set(2)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen metrics port: %v", err)
	}

	srv := StartMetricsServer(listener.Addr().String())
	defer func() {
		_ = srv.Close()
	}()

	go func() {
		_ = srv.Serve(listener)
	}()

	resp, err := http.Get("http://" + listener.Addr().String() + "/metrics")
	if err != nil {
		t.Fatalf("get metrics endpoint: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read metrics body: %v", err)
	}
	output := string(body)

	for _, metricName := range []string{
		"mcpgw_http_requests_total",
		"mcpgw_http_request_duration_seconds",
		"mcpgw_rate_limited_total",
		"mcpgw_policy_denied_total",
		"mcpgw_active_registrations",
		"mcpgw_upstream_errors_total",
		"mcpgw_circuit_breaker_state",
		"mcpgw_nats_connected",
		"mcpgw_server_settings_applied_total",
		"mcpgw_server_settings_rejected_total",
		"mcpgw_server_settings_version",
	} {
		if !strings.Contains(output, metricName) {
			t.Fatalf("expected metric name %q in /metrics output", metricName)
		}
	}
}

func TestMetricsHistogramBucketsArePresent(t *testing.T) {
	t.Parallel()

	handler := MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/bucket-test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen metrics port: %v", err)
	}

	srv := StartMetricsServer(listener.Addr().String())
	defer func() {
		_ = srv.Close()
	}()
	go func() {
		_ = srv.Serve(listener)
	}()

	resp, err := http.Get("http://" + listener.Addr().String() + "/metrics")
	if err != nil {
		t.Fatalf("get metrics endpoint: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read metrics body: %v", err)
	}
	output := string(body)

	if !strings.Contains(output, "mcpgw_http_request_duration_seconds_bucket") {
		t.Fatalf("expected duration histogram buckets in output")
	}
	if !strings.Contains(output, `le="0.005"`) {
		t.Fatalf("expected 0.005 bucket in output")
	}
}
