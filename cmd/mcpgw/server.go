package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
)

func serverCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("server command requires a subcommand: list, inspect, deregister")
	}

	subcommand := strings.TrimSpace(args[0])
	subcommandArgs := args[1:]

	switch subcommand {
	case "list":
		return serverListCommand(subcommandArgs)
	case "inspect":
		return serverInspectCommand(subcommandArgs)
	case "deregister":
		return serverDeregisterCommand(subcommandArgs)
	default:
		return fmt.Errorf("unknown server subcommand %q", subcommand)
	}
}

func serverListCommand(args []string) error {
	fs := flag.NewFlagSet("server list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	statusFlag := fs.String("status", "", "filter by status")
	formatFlag := fs.String("format", "table", "output format: table or json")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse server list flags: %w", err)
	}

	var status *types.ServerStatus
	if strings.TrimSpace(*statusFlag) != "" {
		parsed, err := parseServerStatus(*statusFlag)
		if err != nil {
			return err
		}
		status = parsed
	}

	ctx := context.Background()
	serverStore, closer, err := loadServerStoreFunc(ctx)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	records, err := serverStore.List(ctx, types.ServerFilter{Status: status})
	if err != nil {
		return fmt.Errorf("list servers: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(*formatFlag)) {
	case "json":
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(records); err != nil {
			return fmt.Errorf("encode server list: %w", err)
		}
		return nil
	case "table", "":
		return writeServerTable(stdout, records)
	default:
		return fmt.Errorf("invalid format %q: expected table or json", *formatFlag)
	}
}

func serverInspectCommand(args []string) error {
	fs := flag.NewFlagSet("server inspect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse server inspect flags: %w", err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("server inspect requires exactly one argument: <id-or-name>")
	}

	ctx := context.Background()
	serverStore, closer, err := loadServerStoreFunc(ctx)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	record, err := resolveServerRecord(ctx, serverStore, fs.Arg(0))
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(record); err != nil {
		return fmt.Errorf("encode server details: %w", err)
	}
	return nil
}

func serverDeregisterCommand(args []string) error {
	fs := flag.NewFlagSet("server deregister", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	force := fs.Bool("force", false, "skip confirmation")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse server deregister flags: %w", err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("server deregister requires exactly one argument: <id-or-name>")
	}
	idOrName := fs.Arg(0)

	ok, err := confirmAction(fmt.Sprintf("Deregister server %s", idOrName), *force)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("operation cancelled")
	}

	ctx := context.Background()
	serverStore, closer, err := loadServerStoreFunc(ctx)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	record, err := resolveServerRecord(ctx, serverStore, idOrName)
	if err != nil {
		return err
	}

	if err := serverStore.Delete(ctx, record.ID); err != nil {
		return fmt.Errorf("deregister server %q: %w", idOrName, err)
	}

	_, _ = fmt.Fprintf(stdout, "deregistered server %s (%s)\n", record.Name, record.ID)
	return nil
}

func writeServerTable(w io.Writer, records []types.ServerRecord) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "ID\tNAME\tSTATUS\tSPIFFE_ID\tLAST_HEARTBEAT\tCAPABILITIES"); err != nil {
		return fmt.Errorf("write server table header: %w", err)
	}

	for _, record := range records {
		lastHeartbeat := "-"
		if record.LastHeartbeat != nil {
			lastHeartbeat = record.LastHeartbeat.UTC().Format(time.RFC3339)
		}
		capabilitySummary := fmt.Sprintf("tools=%d resources=%d prompts=%d", len(record.Capabilities.Tools), len(record.Capabilities.Resources), len(record.Capabilities.Prompts))
		if _, err := fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\n",
			record.ID,
			record.Name,
			record.Status,
			record.SPIFFEID,
			lastHeartbeat,
			capabilitySummary,
		); err != nil {
			return fmt.Errorf("write server table row: %w", err)
		}
	}

	if err := tw.Flush(); err != nil {
		return fmt.Errorf("flush server table: %w", err)
	}
	return nil
}

func resolveServerRecord(ctx context.Context, serverStore interface {
	Get(ctx context.Context, id string) (*types.ServerRecord, error)
	GetByName(ctx context.Context, name string) (*types.ServerRecord, error)
}, idOrName string) (*types.ServerRecord, error) {
	key := strings.TrimSpace(idOrName)
	if key == "" {
		return nil, fmt.Errorf("server id or name is required")
	}

	record, err := serverStore.Get(ctx, key)
	if err == nil {
		return record, nil
	}
	if !errors.Is(err, sql.ErrNoRows) && !sqlNoRows(err) {
		return nil, fmt.Errorf("lookup server %q by id: %w", key, err)
	}

	record, err = serverStore.GetByName(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || sqlNoRows(err) {
			return nil, fmt.Errorf("server %q not found", key)
		}
		return nil, fmt.Errorf("lookup server %q by name: %w", key, err)
	}
	return record, nil
}

func parseServerStatus(raw string) (*types.ServerStatus, error) {
	status := types.ServerStatus(strings.ToLower(strings.TrimSpace(raw)))
	switch status {
	case types.StatusPending, types.StatusApproved, types.StatusActive, types.StatusStale, types.StatusExpired:
		return &status, nil
	default:
		return nil, fmt.Errorf("invalid status %q", raw)
	}
}
