package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/cruvero/mcp-gateway/internal/mockbackend"
)

func main() {
	cfg, err := mockbackend.LoadConfigFromEnv()
	if err != nil {
		slog.Error("load mock backend config failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := mockbackend.Run(ctx, cfg, logger); err != nil && err != context.Canceled {
		slog.Error("mock backend exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
