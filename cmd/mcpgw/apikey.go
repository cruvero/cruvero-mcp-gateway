package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cruvero/mcp-gateway/internal/auth"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func apikeyCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("apikey command requires a subcommand: create, list, revoke")
	}

	subcommand := strings.TrimSpace(args[0])
	subcommandArgs := args[1:]

	switch subcommand {
	case "create":
		return apikeyCreateCommand(subcommandArgs)
	case "list":
		return apikeyListCommand(subcommandArgs)
	case "revoke":
		return apikeyRevokeCommand(subcommandArgs)
	default:
		return fmt.Errorf("unknown apikey subcommand %q", subcommand)
	}
}

func apikeyCreateCommand(args []string) error {
	fs := flag.NewFlagSet("apikey create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	name := fs.String("name", "", "display name for the API key")
	scopesRaw := fs.String("scopes", "read", "comma-separated scopes")
	expiresRaw := fs.String("expires", "", "expiration duration (e.g. 24h, 30d)")
	clientID := fs.String("client-id", "", "client identity bound to the key")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse apikey create flags: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("apikey create does not accept positional arguments")
	}

	trimmedName := strings.TrimSpace(*name)
	if trimmedName == "" {
		return fmt.Errorf("apikey create requires --name")
	}

	scopes := parseScopes(*scopesRaw)
	if len(scopes) == 0 {
		return fmt.Errorf("apikey create requires at least one scope")
	}

	expiresAt, err := parseExpiry(*expiresRaw)
	if err != nil {
		return err
	}

	plaintext, lookupHash, bcryptHash, err := auth.GenerateAPIKey()
	if err != nil {
		return fmt.Errorf("generate api key: %w", err)
	}

	effectiveClientID := strings.TrimSpace(*clientID)
	if effectiveClientID == "" {
		effectiveClientID = trimmedName
	}

	record := &types.APIKey{
		Name:          trimmedName,
		ClientID:      effectiveClientID,
		Scopes:        scopes,
		ExpiresAt:     expiresAt,
		KeyLookupHash: lookupHash,
		KeyBcryptHash: bcryptHash,
	}

	ctx := context.Background()
	apiKeyStore, closer, err := loadAPIKeyStoreFunc(ctx)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	if err := apiKeyStore.Create(ctx, record); err != nil {
		return fmt.Errorf("create api key: %w", err)
	}

	stored, err := apiKeyStore.GetByLookupHash(ctx, lookupHash)
	if err == nil && stored != nil {
		record.ID = stored.ID
		record.CreatedAt = stored.CreatedAt
	}

	expires := "never"
	if record.ExpiresAt != nil {
		expires = record.ExpiresAt.UTC().Format(time.RFC3339)
	}

	_, _ = fmt.Fprintf(stdout, "ID: %s\n", record.ID)
	_, _ = fmt.Fprintf(stdout, "Name: %s\n", record.Name)
	_, _ = fmt.Fprintf(stdout, "Client ID: %s\n", record.ClientID)
	_, _ = fmt.Fprintf(stdout, "Expires At: %s\n", expires)
	_, _ = fmt.Fprintf(stdout, "API Key: %s\n", plaintext)
	return nil
}

func apikeyListCommand(args []string) error {
	fs := flag.NewFlagSet("apikey list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	format := fs.String("format", "table", "output format: table or json")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse apikey list flags: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("apikey list does not accept positional arguments")
	}

	ctx := context.Background()
	apiKeyStore, closer, err := loadAPIKeyStoreFunc(ctx)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	keys, err := apiKeyStore.List(ctx)
	if err != nil {
		return fmt.Errorf("list api keys: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "json":
		safe := make([]map[string]any, 0, len(keys))
		for _, key := range keys {
			safe = append(safe, safeAPIKeyView(key))
		}
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(safe); err != nil {
			return fmt.Errorf("encode api key list: %w", err)
		}
		return nil
	case "table", "":
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "ID\tNAME\tCLIENT_ID\tSCOPES\tEXPIRES_AT\tCREATED_AT")
		for _, key := range keys {
			expiresAt := "never"
			if key.ExpiresAt != nil {
				expiresAt = key.ExpiresAt.UTC().Format(time.RFC3339)
			}
			_, _ = fmt.Fprintf(
				tw,
				"%s\t%s\t%s\t%s\t%s\t%s\n",
				key.ID,
				key.Name,
				key.ClientID,
				strings.Join(key.Scopes, ","),
				expiresAt,
				key.CreatedAt.UTC().Format(time.RFC3339),
			)
		}
		if err := tw.Flush(); err != nil {
			return fmt.Errorf("flush api key list: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("invalid format %q: expected table or json", *format)
	}
}

func apikeyRevokeCommand(args []string) error {
	fs := flag.NewFlagSet("apikey revoke", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	force := fs.Bool("force", false, "skip confirmation")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse apikey revoke flags: %w", err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("apikey revoke requires exactly one argument: <id>")
	}
	id := strings.TrimSpace(fs.Arg(0))

	ok, err := confirmAction(fmt.Sprintf("Revoke API key %s", id), *force)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("operation cancelled")
	}

	ctx := context.Background()
	apiKeyStore, closer, err := loadAPIKeyStoreFunc(ctx)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	if err := apiKeyStore.Revoke(ctx, id); err != nil {
		return fmt.Errorf("revoke api key %q: %w", id, err)
	}

	_, _ = fmt.Fprintf(stdout, "revoked api key %s\n", id)
	return nil
}

func parseScopes(raw string) []string {
	parts := strings.Split(raw, ",")
	scopes := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		scopes = append(scopes, trimmed)
	}
	return scopes
}

func parseExpiry(raw string) (*time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	duration, err := parseExtendedDuration(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse expires: %w", err)
	}
	if duration <= 0 {
		return nil, fmt.Errorf("parse expires: duration must be positive")
	}

	expiresAt := time.Now().UTC().Add(duration)
	return &expiresAt, nil
}

func parseExtendedDuration(raw string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if strings.HasSuffix(trimmed, "d") {
		number := strings.TrimSpace(strings.TrimSuffix(trimmed, "d"))
		days, err := strconv.Atoi(number)
		if err != nil {
			return 0, fmt.Errorf("invalid day duration %q", raw)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}

	duration, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, err
	}
	return duration, nil
}

func safeAPIKeyView(key types.APIKey) map[string]any {
	expiresAt := any(nil)
	if key.ExpiresAt != nil {
		expiresAt = key.ExpiresAt.UTC().Format(time.RFC3339)
	}

	return map[string]any{
		"id":         key.ID,
		"name":       key.Name,
		"client_id":  key.ClientID,
		"scopes":     append([]string(nil), key.Scopes...),
		"expires_at": expiresAt,
		"created_at": key.CreatedAt.UTC().Format(time.RFC3339),
	}
}
