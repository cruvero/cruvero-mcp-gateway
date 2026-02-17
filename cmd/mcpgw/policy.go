package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/cruvero/mcp-gateway/internal/events"
	"github.com/cruvero/mcp-gateway/internal/types"
)

const configCachePolicyKey = "config.policy"

func policyCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("policy command requires a subcommand: list, set, reset")
	}

	subcommand := strings.TrimSpace(args[0])
	subcommandArgs := args[1:]

	switch subcommand {
	case "list":
		return policyListCommand(subcommandArgs)
	case "set":
		return policySetCommand(subcommandArgs)
	case "reset":
		return policyResetCommand(subcommandArgs)
	default:
		return fmt.Errorf("unknown policy subcommand %q", subcommand)
	}
}

func policyListCommand(args []string) error {
	fs := flag.NewFlagSet("policy list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse policy list flags: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("policy list does not accept positional arguments")
	}

	ctx := context.Background()
	store, closer, err := loadConfigStoreFunc(ctx)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	profiles, err := loadPolicyProfiles(ctx, store)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tRATE_LIMIT\tRATE_BURST\tENFORCEMENT_MODE\tALLOWLIST\tDENYLIST")
	for _, name := range sortedProfileNames(profiles) {
		profile := profiles[name]
		_, _ = fmt.Fprintf(
			tw,
			"%s\t%d\t%d\t%s\t%d\t%d\n",
			profile.Name,
			profile.RateLimit,
			profile.RateBurst,
			profile.EnforcementMode,
			len(profile.ToolAllowlist),
			len(profile.ToolDenylist),
		)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("flush policy table: %w", err)
	}
	return nil
}

func policySetCommand(args []string) error {
	fs := flag.NewFlagSet("policy set", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	rateLimit := fs.Int("rate-limit", 0, "requests per second")
	rateBurst := fs.Int("rate-burst", 0, "burst size")
	enforcement := fs.String("enforcement-mode", "", "enforcement mode: enforce or audit")
	allowRaw := fs.String("tool-allowlist", "", "comma-separated tool allowlist")
	denyRaw := fs.String("tool-denylist", "", "comma-separated tool denylist")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse policy set flags: %w", err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("policy set requires exactly one argument: <name>")
	}

	name := strings.TrimSpace(fs.Arg(0))
	if name == "" {
		return fmt.Errorf("policy name is required")
	}

	ctx := context.Background()
	store, closer, err := loadConfigStoreFunc(ctx)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	profiles, err := loadPolicyProfiles(ctx, store)
	if err != nil {
		return err
	}

	profile := resolveOrDefaultProfile(name, profiles)
	if *rateLimit > 0 {
		profile.RateLimit = *rateLimit
	}
	if *rateBurst > 0 {
		profile.RateBurst = *rateBurst
	}
	if strings.TrimSpace(*enforcement) != "" {
		mode := types.EnforcementMode(strings.ToLower(strings.TrimSpace(*enforcement)))
		if mode != types.ModeEnforce && mode != types.ModeAudit {
			return fmt.Errorf("invalid enforcement mode %q", *enforcement)
		}
		profile.EnforcementMode = mode
	}
	if strings.TrimSpace(*allowRaw) != "" {
		profile.ToolAllowlist = parseScopes(*allowRaw)
	}
	if strings.TrimSpace(*denyRaw) != "" {
		profile.ToolDenylist = parseScopes(*denyRaw)
	}

	profiles[name] = profile
	if err := persistPolicyProfiles(ctx, store, profiles); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "updated policy profile %s\n", name)
	return nil
}

func policyResetCommand(args []string) error {
	fs := flag.NewFlagSet("policy reset", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse policy reset flags: %w", err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("policy reset requires exactly one argument: <name>")
	}

	name := strings.TrimSpace(fs.Arg(0))
	defaults := defaultPolicyProfiles()
	defaultProfile, ok := defaults[name]
	if !ok {
		return fmt.Errorf("no default profile exists for %q", name)
	}

	ctx := context.Background()
	store, closer, err := loadConfigStoreFunc(ctx)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	profiles, err := loadPolicyProfiles(ctx, store)
	if err != nil {
		return err
	}
	profiles[name] = clonePolicyProfile(defaultProfile)
	if err := persistPolicyProfiles(ctx, store, profiles); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "reset policy profile %s\n", name)
	return nil
}

func loadPolicyProfiles(ctx context.Context, configStore events.ConfigStore) (map[string]*types.PolicyProfile, error) {
	profiles := defaultPolicyProfiles()
	if configStore == nil {
		return profiles, nil
	}

	raw, err := configStore.Load(ctx, configCachePolicyKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || sqlNoRows(err) {
			return profiles, nil
		}
		return nil, fmt.Errorf("load policy profiles: %w", err)
	}

	var message events.PolicyConfigMessage
	if unmarshalErr := json.Unmarshal(raw, &message); unmarshalErr != nil {
		return nil, fmt.Errorf("load policy profiles: decode payload: %w", unmarshalErr)
	}

	loaded := make(map[string]*types.PolicyProfile, len(message.Profiles))
	for _, profile := range message.Profiles {
		if strings.TrimSpace(profile.Name) == "" {
			continue
		}
		copyProfile := profile
		loaded[copyProfile.Name] = &copyProfile
	}
	if len(loaded) == 0 {
		return profiles, nil
	}
	return loaded, nil
}

func persistPolicyProfiles(ctx context.Context, configStore events.ConfigStore, profiles map[string]*types.PolicyProfile) error {
	if configStore == nil {
		return fmt.Errorf("persist policy profiles: config store is nil")
	}

	ordered := make([]types.PolicyProfile, 0, len(profiles))
	for _, name := range sortedProfileNames(profiles) {
		ordered = append(ordered, *clonePolicyProfile(profiles[name]))
	}

	payload, err := json.Marshal(events.PolicyConfigMessage{Profiles: ordered})
	if err != nil {
		return fmt.Errorf("persist policy profiles: encode payload: %w", err)
	}
	if err := configStore.Save(ctx, configCachePolicyKey, payload); err != nil {
		return fmt.Errorf("persist policy profiles: %w", err)
	}
	return nil
}

func resolveOrDefaultProfile(name string, existing map[string]*types.PolicyProfile) *types.PolicyProfile {
	if profile, ok := existing[name]; ok {
		return clonePolicyProfile(profile)
	}
	if defaults := defaultPolicyProfiles(); defaults[name] != nil {
		return clonePolicyProfile(defaults[name])
	}
	return &types.PolicyProfile{
		Name:            name,
		RateLimit:       10,
		RateBurst:       20,
		ToolAllowlist:   []string{},
		ToolDenylist:    []string{},
		EnforcementMode: types.ModeEnforce,
	}
}

func defaultPolicyProfiles() map[string]*types.PolicyProfile {
	return map[string]*types.PolicyProfile{
		"default": {
			Name:            "default",
			RateLimit:       10,
			RateBurst:       20,
			ToolAllowlist:   []string{},
			ToolDenylist:    []string{},
			EnforcementMode: types.ModeEnforce,
		},
		"premium": {
			Name:            "premium",
			RateLimit:       50,
			RateBurst:       100,
			ToolAllowlist:   []string{},
			ToolDenylist:    []string{},
			EnforcementMode: types.ModeEnforce,
		},
		"admin": {
			Name:            "admin",
			RateLimit:       100,
			RateBurst:       200,
			ToolAllowlist:   []string{},
			ToolDenylist:    []string{},
			EnforcementMode: types.ModeEnforce,
		},
	}
}

func clonePolicyProfile(profile *types.PolicyProfile) *types.PolicyProfile {
	if profile == nil {
		return nil
	}
	out := *profile
	out.ToolAllowlist = append([]string(nil), profile.ToolAllowlist...)
	out.ToolDenylist = append([]string(nil), profile.ToolDenylist...)
	return &out
}

func sortedProfileNames(profiles map[string]*types.PolicyProfile) []string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
