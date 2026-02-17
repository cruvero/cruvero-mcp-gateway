package main

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestServeWithContextInitializesAndShutsDown(t *testing.T) {
	t.Setenv("MCPGW_DB_URL", "postgres://example.invalid:5432/mcpgw?sslmode=disable")
	t.Setenv("MCPGW_LISTEN_ADDR", "127.0.0.1:0")
	t.Setenv("MCPGW_METRICS_ADDR", "127.0.0.1:0")
	t.Setenv("MCPGW_CRUVERO_ENABLED", "false")
	t.Setenv("MCPGW_OIDC_ISSUER", "")
	t.Setenv("MCPGW_OIDC_AUDIENCE", "")
	t.Setenv("MCPGW_CORS_ENABLED", "false")

	origOpen := openDBFunc
	openDBFunc = func(ctx context.Context, dbURL string) (*sql.DB, error) {
		_ = ctx
		_ = dbURL
		db, mock, err := sqlmock.New()
		if err != nil {
			return nil, err
		}
		rows := sqlmock.NewRows([]string{"id", "name", "spiffe_id", "version", "host", "port", "capabilities", "status", "policy_profile", "last_heartbeat", "created_at", "updated_at"})
		mock.ExpectQuery("SELECT (.+) FROM mcp_servers WHERE status = \\$1 ORDER BY created_at DESC").WithArgs("active").WillReturnRows(rows)
		mock.ExpectClose()
		return db, nil
	}
	t.Cleanup(func() {
		openDBFunc = origOpen
	})

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
