package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/events"
	storepkg "github.com/cruvero/mcp-gateway/internal/store"
)

var (
	loadServerStoreFunc           = loadServerStore
	loadAPIKeyStoreFunc           = loadAPIKeyStore
	loadConfigStoreFunc           = loadConfigStore
	stdin               io.Reader = os.Stdin
)

func loadServerStore(ctx context.Context) (storepkg.ServerStore, func() error, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("load server store: load config: %w", err)
	}

	db, err := openDBFunc(ctx, cfg.DBURL)
	if err != nil {
		return nil, nil, fmt.Errorf("load server store: open database: %w", err)
	}

	return storepkg.NewPostgresServerStore(db), db.Close, nil
}

func loadAPIKeyStore(ctx context.Context) (storepkg.APIKeyStore, func() error, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("load apikey store: load config: %w", err)
	}

	db, err := openDBFunc(ctx, cfg.DBURL)
	if err != nil {
		return nil, nil, fmt.Errorf("load apikey store: open database: %w", err)
	}

	return storepkg.NewPostgresAPIKeyStore(db), db.Close, nil
}

func loadConfigStore(ctx context.Context) (events.ConfigStore, func() error, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("load config store: load config: %w", err)
	}

	db, err := openDBFunc(ctx, cfg.DBURL)
	if err != nil {
		return nil, nil, fmt.Errorf("load config store: open database: %w", err)
	}

	return events.NewPostgresConfigStore(db), db.Close, nil
}

func confirmAction(prompt string, force bool) (bool, error) {
	if force {
		return true, nil
	}

	_, _ = fmt.Fprintf(stdout, "%s [y/N]: ", strings.TrimSpace(prompt))
	reader := bufio.NewReader(stdin)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read confirmation: %w", err)
	}

	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func closeQuietly(closer func() error) {
	if closer == nil {
		return
	}
	if err := closer(); err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: close resource failed: %v\n", err)
	}
}

func sqlNoRows(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), strings.ToLower(sql.ErrNoRows.Error()))
}
