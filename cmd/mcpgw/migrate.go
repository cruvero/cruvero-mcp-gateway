package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

type migrateRunner interface {
	Up() error
	Down() error
	Steps(n int) error
	Version() (uint, bool, error)
	Close() (error, error)
}

var newMigrateRunnerFunc = newMigrateRunner

func migrateCommand(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	direction := fs.String("direction", "up", "migration direction: up or down")
	steps := fs.Int("steps", 0, "number of steps; 0 means all")
	dbURLOverride := fs.String("db-url", "", "database URL override")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse migrate flags: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("migrate command does not accept positional arguments")
	}

	resolvedDirection := strings.ToLower(strings.TrimSpace(*direction))
	if resolvedDirection != "up" && resolvedDirection != "down" {
		return fmt.Errorf("invalid migrate direction %q", *direction)
	}

	dbURL, err := resolveMigrateDBURL(strings.TrimSpace(*dbURLOverride))
	if err != nil {
		return err
	}

	runner, err := newMigrateRunnerFunc(dbURL)
	if err != nil {
		return err
	}
	defer func() {
		sourceErr, dbErr := runner.Close()
		if sourceErr != nil {
			_, _ = fmt.Fprintf(stderr, "warning: close migration source failed: %v\n", sourceErr)
		}
		if dbErr != nil {
			_, _ = fmt.Fprintf(stderr, "warning: close migration db failed: %v\n", dbErr)
		}
	}()

	if err := applyMigrations(runner, resolvedDirection, *steps); err != nil {
		if err == migrate.ErrNoChange {
			_, _ = fmt.Fprintln(stdout, "no migrations to apply")
		} else {
			return err
		}
	}

	version, dirty, err := runner.Version()
	if err != nil {
		if err == migrate.ErrNilVersion {
			_, _ = fmt.Fprintln(stdout, "migration version: none")
			return nil
		}
		return fmt.Errorf("read migration version: %w", err)
	}

	_, _ = fmt.Fprintf(stdout, "migration version: %d\n", version)
	_, _ = fmt.Fprintf(stdout, "dirty: %t\n", dirty)
	return nil
}

func resolveMigrateDBURL(override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		return override, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("resolve migrate db url: load config: %w", err)
	}
	return cfg.DBURL, nil
}

func applyMigrations(runner migrateRunner, direction string, steps int) error {
	if steps < 0 {
		return fmt.Errorf("apply migrations: steps must be non-negative")
	}

	switch direction {
	case "up":
		if steps > 0 {
			return runner.Steps(steps)
		}
		return runner.Up()
	case "down":
		if steps > 0 {
			return runner.Steps(-steps)
		}
		return runner.Down()
	default:
		return fmt.Errorf("apply migrations: unsupported direction %q", direction)
	}
}

func newMigrateRunner(dbURL string) (migrateRunner, error) {
	db, err := openDBFunc(context.Background(), dbURL)
	if err != nil {
		return nil, fmt.Errorf("initialize migrator: open database: %w", err)
	}

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize migrator: create postgres driver: %w", err)
	}

	runner, err := migrate.NewWithDatabaseInstance(migrationSourceURL(), "postgres", driver)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize migrator: create migrate instance: %w", err)
	}

	return runner, nil
}

func migrationSourceURL() string {
	if _, err := os.Stat("/migrations"); err == nil {
		return "file:///migrations"
	}
	return "file://migrations"
}

var _ migrateRunner = (*migrate.Migrate)(nil)
