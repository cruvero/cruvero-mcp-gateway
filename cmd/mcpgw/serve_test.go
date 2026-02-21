package main

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func setupServeMock(t *testing.T) {
	t.Helper()
	origOpen := openDBFunc
	openDBFunc = func(_ context.Context, _ string) (*sql.DB, error) {
		db, mock, err := sqlmock.New()
		if err != nil {
			return nil, err
		}
		rows := sqlmock.NewRows([]string{"id", "name", "spiffe_id", "version", "host", "port", "capabilities", "status", "policy_profile", "last_heartbeat", "created_at", "updated_at"})
		mock.ExpectQuery("SELECT (.+) FROM mcp_servers WHERE \\(\\$1::text IS NULL OR status = \\$1\\) AND \\(\\$2::text IS NULL OR name ILIKE \\$2\\) ORDER BY created_at DESC LIMIT \\$3 OFFSET \\$4").
			WithArgs("active", nil, int64(9223372036854775807), int64(0)).
			WillReturnRows(rows)
		mock.ExpectClose()
		return db, nil
	}
	t.Cleanup(func() {
		openDBFunc = origOpen
	})
}

func TestServeWithContextInitializesAndShutsDown(t *testing.T) {
	t.Setenv("MCPGW_DB_URL", "postgres://example.invalid:5432/mcpgw?sslmode=disable")
	t.Setenv("MCPGW_LISTEN_ADDR", "127.0.0.1:0")
	t.Setenv("MCPGW_METRICS_ADDR", "127.0.0.1:0")
	t.Setenv("MCPGW_CRUVERO_ENABLED", "false")
	t.Setenv("MCPGW_OIDC_ISSUER", "")
	t.Setenv("MCPGW_OIDC_AUDIENCE", "")
	t.Setenv("MCPGW_CORS_ENABLED", "false")

	setupServeMock(t)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveWithContext(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("serveWithContext returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serveWithContext did not shut down in time")
	}
}

func TestServeWithContext_AdminDevMode(t *testing.T) {
	t.Setenv("MCPGW_DB_URL", "postgres://example.invalid:5432/mcpgw?sslmode=disable")
	t.Setenv("MCPGW_LISTEN_ADDR", "127.0.0.1:0")
	t.Setenv("MCPGW_METRICS_ADDR", "127.0.0.1:0")
	t.Setenv("MCPGW_CRUVERO_ENABLED", "false")
	t.Setenv("MCPGW_OIDC_ISSUER", "")
	t.Setenv("MCPGW_OIDC_AUDIENCE", "")
	t.Setenv("MCPGW_CORS_ENABLED", "false")
	t.Setenv("MCPGW_ADMIN_ENABLED", "true")
	t.Setenv("MCPGW_ADMIN_DEV_MODE", "true")

	setupServeMock(t)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveWithContext(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("serveWithContext returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serveWithContext did not shut down in time")
	}
}
