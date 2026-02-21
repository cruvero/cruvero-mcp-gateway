package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/policy"
	storepkg "github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
)

var loadToolClassificationStoreFunc = loadToolClassificationStore

func toolCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("tool command requires a subcommand: list, classify, auto-classify")
	}

	subcommand := strings.TrimSpace(args[0])
	subcommandArgs := args[1:]

	switch subcommand {
	case "list":
		return toolListCommand(subcommandArgs)
	case "classify":
		return toolClassifyCommand(subcommandArgs)
	case "auto-classify":
		return toolAutoClassifyCommand(subcommandArgs)
	default:
		return fmt.Errorf("unknown tool subcommand %q", subcommand)
	}
}

func toolListCommand(args []string) error {
	fs := flag.NewFlagSet("tool list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	riskLevel := fs.String("risk-level", "", "filter by risk level (read_only, write, destructive, unknown)")
	format := fs.String("format", "table", "output format: table or json")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse tool list flags: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("tool list does not accept positional arguments")
	}

	ctx := context.Background()
	store, closer, err := loadToolClassificationStoreFunc(ctx)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	var classifications []types.ToolClassification
	level := strings.TrimSpace(*riskLevel)
	if level != "" {
		rl := types.RiskLevel(level)
		if !rl.IsValid() {
			return fmt.Errorf("invalid risk level %q: expected read_only, write, destructive, or unknown", level)
		}
		classifications, err = store.GetByRiskLevel(ctx, rl)
	} else {
		classifications, err = store.GetAll(ctx)
	}
	if err != nil {
		return fmt.Errorf("list tool classifications: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "json":
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(classifications); err != nil {
			return fmt.Errorf("encode classifications: %w", err)
		}
	case "table", "":
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "TOOL_NAME\tRISK_LEVEL\tREASON\tAUTO\tUPDATED_BY\tUPDATED_AT")
		for _, tc := range classifications {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%v\t%s\t%s\n",
				tc.ToolName,
				tc.RiskLevel,
				tc.Reason,
				tc.AutoClassified,
				tc.UpdatedBy,
				tc.UpdatedAt.UTC().Format(time.RFC3339),
			)
		}
		if err := tw.Flush(); err != nil {
			return fmt.Errorf("flush tool list: %w", err)
		}
	default:
		return fmt.Errorf("invalid format %q: expected table or json", *format)
	}
	return nil
}

func toolClassifyCommand(args []string) error {
	fs := flag.NewFlagSet("tool classify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	riskLevel := fs.String("risk-level", "", "risk level: read_only, write, destructive, unknown")
	reason := fs.String("reason", "", "reason for classification")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse tool classify flags: %w", err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("tool classify requires exactly one argument: <tool-name>")
	}

	toolName := strings.TrimSpace(fs.Arg(0))
	if toolName == "" {
		return fmt.Errorf("tool name is required")
	}

	level := strings.TrimSpace(*riskLevel)
	if level == "" {
		return fmt.Errorf("--risk-level is required")
	}
	rl := types.RiskLevel(level)
	if !rl.IsValid() {
		return fmt.Errorf("invalid risk level %q: expected read_only, write, destructive, or unknown", level)
	}

	ctx := context.Background()
	store, closer, err := loadToolClassificationStoreFunc(ctx)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	tc := &types.ToolClassification{
		ToolName:       toolName,
		RiskLevel:      rl,
		Reason:         strings.TrimSpace(*reason),
		AutoClassified: false,
		UpdatedBy:      "admin",
		UpdatedAt:      time.Now().UTC(),
	}

	if err := store.Upsert(ctx, tc); err != nil {
		return fmt.Errorf("classify tool: %w", err)
	}

	_, _ = fmt.Fprintf(stdout, "classified %s as %s\n", toolName, rl)
	return nil
}

func toolAutoClassifyCommand(args []string) error {
	fs := flag.NewFlagSet("tool auto-classify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse tool auto-classify flags: %w", err)
	}

	ctx := context.Background()
	store, closer, err := loadToolClassificationStoreFunc(ctx)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	all, err := store.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("list classifications: %w", err)
	}

	updated := 0
	for _, tc := range all {
		if !tc.AutoClassified {
			continue
		}

		newLevel, newReason := policy.AutoClassify(tc.ToolName, "")
		updated++
		replacement := &types.ToolClassification{
			ToolName:       tc.ToolName,
			RiskLevel:      newLevel,
			Reason:         newReason,
			AutoClassified: true,
			UpdatedBy:      "auto-reclassify",
			UpdatedAt:      time.Now().UTC(),
		}
		if err := store.Upsert(ctx, replacement); err != nil {
			return fmt.Errorf("reclassify tool %s: %w", tc.ToolName, err)
		}
	}

	_, _ = fmt.Fprintf(stdout, "auto-reclassified %d tools\n", updated)
	return nil
}

func loadToolClassificationStore(ctx context.Context) (storepkg.ToolClassificationStore, func() error, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("load tool classification store: load config: %w", err)
	}

	db, err := openDBFunc(ctx, cfg.DBURL)
	if err != nil {
		return nil, nil, fmt.Errorf("load tool classification store: open database: %w", err)
	}

	return storepkg.NewPostgresToolClassificationStore(db), db.Close, nil
}
