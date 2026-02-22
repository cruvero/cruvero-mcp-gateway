//go:build integration

package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
)

const defaultTestDBURL = "postgres://localhost:5432/mcpgw_test?sslmode=disable"

// SetupTestDB initializes a real Postgres test database and runs migrations.
func SetupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping Postgres integration setup in short mode")
	}

	dbURL := strings.TrimSpace(os.Getenv("MCPGW_TEST_DB_URL"))
	if dbURL == "" {
		dbURL = defaultTestDBURL
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Skipf("skipping Postgres integration setup: open test db: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Skipf("skipping Postgres integration setup: ping test db: %v", err)
	}

	if err := runMigrations(dbURL); err != nil {
		_ = db.Close()
		t.Skipf("skipping Postgres integration setup: run migrations: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := db.ExecContext(cleanupCtx, `DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;`); err != nil {
			t.Fatalf("cleanup test db schema: %v", err)
		}
		if _, err := db.ExecContext(cleanupCtx, `CREATE EXTENSION IF NOT EXISTS pgcrypto;`); err != nil {
			t.Fatalf("recreate pgcrypto extension: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close test db: %v", err)
		}
	})

	return db
}

// runMigrations uses a dedicated database URL (not a shared *sql.DB) so the
// migrator can be fully closed without affecting the caller's connection pool.
func runMigrations(dbURL string) error {
	migrationsPath, err := migrationsSourceURL()
	if err != nil {
		return fmt.Errorf("resolve migrations path: %w", err)
	}

	migrator, err := migrate.New(migrationsPath, dbURL)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	defer migrator.Close()

	if err := migrator.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("apply up migrations: %w", err)
	}
	return nil
}

func migrationsSourceURL() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	current := wd
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return "file://" + filepath.ToSlash(filepath.Join(current, "migrations")), nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return "", fmt.Errorf("could not locate go.mod from %s", wd)
}
