package auth

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestServerScopeContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		scope    []string
		wantNil  bool
		wantLen  int
	}{
		{
			name:    "nil scope returns nil",
			scope:   nil,
			wantNil: true,
		},
		{
			name:    "empty scope returns empty",
			scope:   []string{},
			wantLen: 0,
		},
		{
			name:    "single scope round-trips",
			scope:   []string{"server-a"},
			wantLen: 1,
		},
		{
			name:    "multiple scopes round-trip",
			scope:   []string{"server-a", "server-b"},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if tt.scope != nil {
				ctx = ContextWithServerScope(ctx, tt.scope)
			}
			got := ServerScopeFromContext(ctx)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil scope, got %v", got)
				}
				return
			}
			if len(got) != tt.wantLen {
				t.Fatalf("expected scope len %d, got %d", tt.wantLen, len(got))
			}
		})
	}
}

func TestServerScopeMiddleware(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	tests := []struct {
		name           string
		scope          []string
		serverName     string // chi URL param; empty means unified endpoint
		wantStatus     int
		wantHeader     string // expected X-Denied-Reason header
		wantBodyReason string // expected reason in JSON body
	}{
		{
			name:       "empty scope passes",
			scope:      nil,
			serverName: "server-a",
			wantStatus: http.StatusOK,
		},
		{
			name:       "matching scope passes",
			scope:      []string{"server-a", "server-b"},
			serverName: "server-a",
			wantStatus: http.StatusOK,
		},
		{
			name:           "non-matching scope returns 403",
			scope:          []string{"server-b"},
			serverName:     "server-a",
			wantStatus:     http.StatusForbidden,
			wantHeader:     "server_scope",
			wantBodyReason: "server_scope",
		},
		{
			name:       "case-insensitive match passes",
			scope:      []string{"Server-A"},
			serverName: "server-a",
			wantStatus: http.StatusOK,
		},
		{
			name:       "mcp- prefix stripped for match",
			scope:      []string{"github"},
			serverName: "mcp-github",
			wantStatus: http.StatusOK,
		},
		{
			name:       "unified endpoint with scope passes through to handler",
			scope:      []string{"server-a"},
			serverName: "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "empty scope on unified endpoint passes",
			scope:      nil,
			serverName: "",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := ServerScopeMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
			if len(tt.scope) > 0 {
				req = req.WithContext(ContextWithServerScope(req.Context(), tt.scope))
			}

			if tt.serverName != "" {
				// Set up chi route context for per-server endpoint
				rctx := chi.NewRouteContext()
				rctx.URLParams.Add("serverName", tt.serverName)
				req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}

			if tt.wantHeader != "" {
				got := rec.Header().Get("X-Denied-Reason")
				if got != tt.wantHeader {
					t.Fatalf("expected X-Denied-Reason %q, got %q", tt.wantHeader, got)
				}
			}

			if tt.wantBodyReason != "" {
				var body map[string]string
				if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if body["reason"] != tt.wantBodyReason {
					t.Fatalf("expected body reason %q, got %q", tt.wantBodyReason, body["reason"])
				}
			}
		})
	}
}

func TestCheckServerScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		scope      []string
		serverName string
		want       bool
	}{
		{
			name:       "nil scope allows all",
			scope:      nil,
			serverName: "any-server",
			want:       true,
		},
		{
			name:       "empty scope allows all",
			scope:      []string{},
			serverName: "any-server",
			want:       true,
		},
		{
			name:       "matching server allowed",
			scope:      []string{"server-a"},
			serverName: "server-a",
			want:       true,
		},
		{
			name:       "non-matching server denied",
			scope:      []string{"server-a"},
			serverName: "server-b",
			want:       false,
		},
		{
			name:       "case-insensitive match",
			scope:      []string{"Server-A"},
			serverName: "server-a",
			want:       true,
		},
		{
			name:       "mcp- prefix stripping",
			scope:      []string{"github"},
			serverName: "mcp-github",
			want:       true,
		},
		{
			name:       "empty server name denied",
			scope:      []string{"server-a"},
			serverName: "",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			if tt.scope != nil {
				ctx = ContextWithServerScope(ctx, tt.scope)
			}

			got := CheckServerScope(ctx, tt.serverName)
			if got != tt.want {
				t.Fatalf("CheckServerScope(%q) = %v, want %v", tt.serverName, got, tt.want)
			}
		})
	}
}

func TestServerScopeMiddlewareNilLogger(t *testing.T) {
	t.Parallel()

	// Verify nil logger does not panic.
	handler := ServerScopeMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}

func TestServerScopeMiddlewareScopeRunsAfterAuth(t *testing.T) {
	t.Parallel()

	// Simulates the middleware chain: auth sets scope, then scope middleware checks it.
	scope := []string{"server-a"}

	authMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := ContextWithServerScope(r.Context(), scope)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	scopeMW := ServerScopeMiddleware(logger)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := authMW(scopeMW(inner))

	// Per-server endpoint with non-matching server.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("serverName", "server-b")
	req := httptest.NewRequest(http.MethodGet, "/mcp/servers/server-b", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", rec.Code)
	}

	header := rec.Header().Get("X-Denied-Reason")
	if header != "server_scope" {
		t.Fatalf("expected X-Denied-Reason 'server_scope', got %q", header)
	}
}
