package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics collectors exposed for gateway observability and alerting.
var (
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mcpgw_http_requests_total",
			Help: "Total HTTP requests.",
		},
		[]string{"method", "path", "status_code"},
	)

	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "mcpgw_http_request_duration_seconds",
			Help:    "HTTP request duration distribution.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"method", "path"},
	)

	RateLimitedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mcpgw_rate_limited_total",
			Help: "Total requests rejected by rate limiting.",
		},
		[]string{"client_id", "route"},
	)

	PolicyDeniedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mcpgw_policy_denied_total",
			Help: "Total requests denied by policy.",
		},
		[]string{"reason", "tool"},
	)

	ActiveRegistrations = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "mcpgw_active_registrations",
			Help: "Current registration count by status.",
		},
		[]string{"status"},
	)

	UpstreamErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mcpgw_upstream_errors_total",
			Help: "Total upstream backend errors.",
		},
		[]string{"backend", "error_type"},
	)

	CircuitBreakerState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "mcpgw_circuit_breaker_state",
			Help: "Circuit breaker state by backend and state label.",
		},
		[]string{"backend", "state"},
	)

	NATSConnected = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "mcpgw_nats_connected",
			Help: "NATS connection status (1 connected, 0 disconnected).",
		},
	)

	ServerSettingsAppliedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mcpgw_server_settings_applied_total",
			Help: "Total successful server settings applies.",
		},
		[]string{"server_name"},
	)

	ServerSettingsRejectedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mcpgw_server_settings_rejected_total",
			Help: "Total rejected server settings updates.",
		},
		[]string{"server_name", "reason"},
	)

	ServerSettingsVersion = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "mcpgw_server_settings_version",
			Help: "Latest server settings config version by server.",
		},
		[]string{"server_name"},
	)

	ToolCallsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mcpgw_tool_calls_total",
			Help: "Total routed tool calls by tool/backend/status.",
		},
		[]string{"tool", "backend", "status"},
	)

	ToolCallDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "mcpgw_tool_call_duration_seconds",
			Help:    "Routed tool call duration distribution.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"tool", "backend"},
	)

	ActiveToolsGauge = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "mcpgw_active_tools",
			Help: "Current number of active tools in the capability index.",
		},
	)
)

// StartMetricsServer returns an HTTP server configured to expose Prometheus metrics on /metrics.
func StartMetricsServer(addr string) *http.Server {
	listenAddr := strings.TrimSpace(addr)
	if listenAddr == "" {
		listenAddr = ":9090"
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	return &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

// ObserveHTTPRequest records HTTP request count and latency.
func ObserveHTTPRequest(method string, path string, statusCode int, duration time.Duration) {
	HTTPRequestsTotal.WithLabelValues(strings.ToUpper(strings.TrimSpace(method)), normalizePath(path), strconv.Itoa(statusCode)).Inc()
	HTTPRequestDuration.WithLabelValues(strings.ToUpper(strings.TrimSpace(method)), normalizePath(path)).Observe(duration.Seconds())
}

// ObserveRateLimited increments rate-limit rejection counters.
func ObserveRateLimited(clientID string, route string) {
	RateLimitedTotal.WithLabelValues(normalizeLabel(clientID, "anonymous"), normalizeLabel(route, "unknown")).Inc()
}

// ObservePolicyDenied increments policy denial counters.
func ObservePolicyDenied(reason string, tool string) {
	PolicyDeniedTotal.WithLabelValues(normalizeLabel(reason, "policy_denied"), normalizeLabel(tool, "unknown")).Inc()
}

// AddActiveRegistrations adds a delta to active registration gauges by status.
func AddActiveRegistrations(status string, delta float64) {
	ActiveRegistrations.WithLabelValues(normalizeLabel(status, "unknown")).Add(delta)
}

// SetActiveRegistrations sets active registration gauges by status.
func SetActiveRegistrations(status string, value float64) {
	ActiveRegistrations.WithLabelValues(normalizeLabel(status, "unknown")).Set(value)
}

// ObserveUpstreamError increments upstream error counters by backend and type.
func ObserveUpstreamError(backend string, errorType string) {
	UpstreamErrorsTotal.WithLabelValues(normalizeLabel(backend, "unknown"), normalizeLabel(errorType, "unknown")).Inc()
}

// SetCircuitBreakerState updates breaker-state gauges for all possible states.
func SetCircuitBreakerState(backend string, state string) {
	b := normalizeLabel(backend, "unknown")
	for _, candidate := range []string{"closed", "open", "half_open"} {
		value := 0.0
		if strings.TrimSpace(state) == candidate {
			value = 1.0
		}
		CircuitBreakerState.WithLabelValues(b, candidate).Set(value)
	}
}

// SetNATSConnected sets NATS connectivity gauge.
func SetNATSConnected(connected bool) {
	if connected {
		NATSConnected.Set(1)
		return
	}
	NATSConnected.Set(0)
}

// ObserveServerSettingsApplied increments successful server-settings apply counters.
func ObserveServerSettingsApplied(serverName string) {
	ServerSettingsAppliedTotal.WithLabelValues(normalizeLabel(serverName, "unknown")).Inc()
}

// ObserveServerSettingsRejected increments rejected server-settings counters.
func ObserveServerSettingsRejected(serverName string, reason string) {
	ServerSettingsRejectedTotal.WithLabelValues(normalizeLabel(serverName, "unknown"), normalizeLabel(reason, "unknown")).Inc()
}

// SetServerSettingsVersion sets the latest observed settings version per server.
func SetServerSettingsVersion(serverName string, version int64) {
	ServerSettingsVersion.WithLabelValues(normalizeLabel(serverName, "unknown")).Set(float64(version))
}

// ObserveToolCall records routed tool call count and latency by labels.
func ObserveToolCall(tool string, backend string, status string, duration time.Duration) {
	normalizedTool := normalizeLabel(tool, "unknown")
	normalizedBackend := normalizeLabel(backend, "unknown")
	ToolCallsTotal.WithLabelValues(normalizedTool, normalizedBackend, normalizeLabel(status, "unknown")).Inc()
	ToolCallDuration.WithLabelValues(normalizedTool, normalizedBackend).Observe(duration.Seconds())
}

// SetActiveToolCount sets the active tools gauge to the given count.
func SetActiveToolCount(count int) {
	ActiveToolsGauge.Set(float64(count))
}

func normalizePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "/"
	}
	return trimmed
}

func normalizeLabel(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}
