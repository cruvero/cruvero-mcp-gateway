package testutil

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
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
		t.Fatalf("open test db: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("ping test db: %v", err)
	}

	if err := runMigrations(dbURL, db); err != nil {
		_ = db.Close()
		t.Fatalf("run test db migrations: %v", err)
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

func runMigrations(dbURL string, db *sql.DB) error {
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("build migrate postgres driver: %w", err)
	}

	migrationsPath, err := migrationsSourceURL()
	if err != nil {
		return fmt.Errorf("resolve migrations path: %w", err)
	}

	migrator, err := migrate.NewWithDatabaseInstance(migrationsPath, "postgres", driver)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	defer func() {
		_, _ = migrator.Close()
	}()

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

// SetupTestNATS starts an embedded NATS server and returns a client connection and URL.
func SetupTestNATS(t *testing.T) (*nats.Conn, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping NATS integration setup in short mode")
	}

	natsServer, err := server.NewServer(&server.Options{
		Host:   "127.0.0.1",
		Port:   -1,
		NoLog:  true,
		NoSigs: true,
	})
	if err != nil {
		t.Fatalf("create embedded nats server: %v", err)
	}

	go natsServer.Start()
	if !natsServer.ReadyForConnections(5 * time.Second) {
		natsServer.Shutdown()
		t.Fatal("embedded nats server did not become ready")
	}

	natsURL := fmt.Sprintf("nats://%s", natsServer.Addr().String())
	conn, err := nats.Connect(natsURL)
	if err != nil {
		natsServer.Shutdown()
		t.Fatalf("connect to embedded nats server: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Drain()
		conn.Close()
		natsServer.Shutdown()
	})

	return conn, natsURL
}

// NewTestServer creates an HTTPS httptest server from test cert fixtures.
func NewTestServer(t *testing.T, handler http.Handler, certs *CertBundle) *httptest.Server {
	t.Helper()
	if handler == nil {
		handler = http.NewServeMux()
	}
	if certs == nil {
		certs = GenerateTestCerts(t)
	}

	pair, err := tls.X509KeyPair(certs.ServerCertPEM, certs.ServerKeyPEM)
	if err != nil {
		t.Fatalf("parse server keypair: %v", err)
	}

	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{pair},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	return srv
}
