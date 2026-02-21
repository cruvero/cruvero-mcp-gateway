package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	storepkg "github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
)

type mockToolClassificationStore struct {
	classifications []types.ToolClassification
	upserted        []*types.ToolClassification
	filterLevel     types.RiskLevel
	getAllErr       error
	getByRiskErr   error
	upsertErr      error
}

func (m *mockToolClassificationStore) Get(ctx context.Context, toolName string) (*types.ToolClassification, error) {
	_ = ctx
	for i := range m.classifications {
		if m.classifications[i].ToolName == toolName {
			tc := m.classifications[i]
			return &tc, nil
		}
	}
	return nil, errors.New("sql: no rows in result set")
}

func (m *mockToolClassificationStore) GetAll(ctx context.Context) ([]types.ToolClassification, error) {
	_ = ctx
	if m.getAllErr != nil {
		return nil, m.getAllErr
	}
	return append([]types.ToolClassification(nil), m.classifications...), nil
}

func (m *mockToolClassificationStore) GetByRiskLevel(ctx context.Context, level types.RiskLevel) ([]types.ToolClassification, error) {
	_ = ctx
	m.filterLevel = level
	if m.getByRiskErr != nil {
		return nil, m.getByRiskErr
	}
	var result []types.ToolClassification
	for _, tc := range m.classifications {
		if tc.RiskLevel == level {
			result = append(result, tc)
		}
	}
	return result, nil
}

func (m *mockToolClassificationStore) Upsert(ctx context.Context, classification *types.ToolClassification) error {
	_ = ctx
	if m.upsertErr != nil {
		return m.upsertErr
	}
	copy := *classification
	m.upserted = append(m.upserted, &copy)
	return nil
}

func (m *mockToolClassificationStore) Delete(ctx context.Context, toolName string) error {
	_ = ctx
	_ = toolName
	return nil
}

func setupToolTest(t *testing.T, store *mockToolClassificationStore) (*bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	origLoad := loadToolClassificationStoreFunc
	origOut := stdout
	origErr := stderr
	t.Cleanup(func() {
		loadToolClassificationStoreFunc = origLoad
		stdout = origOut
		stderr = origErr
	})

	loadToolClassificationStoreFunc = func(ctx context.Context) (storepkg.ToolClassificationStore, func() error, error) {
		_ = ctx
		return store, func() error { return nil }, nil
	}

	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	stdout = outBuf
	stderr = errBuf
	return outBuf, errBuf
}

func TestToolCommandDispatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		wantErr   bool
		errSubstr string
	}{
		{
			name:      "no subcommand",
			args:      []string{},
			wantErr:   true,
			errSubstr: "requires a subcommand",
		},
		{
			name:      "unknown subcommand",
			args:      []string{"invalid"},
			wantErr:   true,
			errSubstr: "unknown tool subcommand",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := toolCommand(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("expected error containing %q, got %q", tc.errSubstr, err.Error())
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestToolCommandRoutesToSubcommands(t *testing.T) {
	store := &mockToolClassificationStore{}
	outBuf, _ := setupToolTest(t, store)

	if err := toolCommand([]string{"list"}); err != nil {
		t.Fatalf("tool list: %v", err)
	}

	got := outBuf.String()
	if !strings.Contains(got, "TOOL_NAME") || !strings.Contains(got, "RISK_LEVEL") {
		t.Fatalf("expected table header in tool list output, got %q", got)
	}
}

func TestToolListCommandTableFormat(t *testing.T) {
	now := time.Now().UTC()
	store := &mockToolClassificationStore{
		classifications: []types.ToolClassification{
			{
				ToolName:       "file.read",
				RiskLevel:      types.RiskReadOnly,
				Reason:         "reads files",
				AutoClassified: true,
				UpdatedBy:      "auto-classify",
				UpdatedAt:      now,
			},
			{
				ToolName:       "file.delete",
				RiskLevel:      types.RiskDestructive,
				Reason:         "deletes files",
				AutoClassified: false,
				UpdatedBy:      "admin",
				UpdatedAt:      now,
			},
		},
	}
	outBuf, _ := setupToolTest(t, store)

	if err := toolListCommand(nil); err != nil {
		t.Fatalf("tool list table: %v", err)
	}

	got := outBuf.String()
	if !strings.Contains(got, "TOOL_NAME") {
		t.Fatalf("missing table header: %q", got)
	}
	if !strings.Contains(got, "file.read") || !strings.Contains(got, "read_only") {
		t.Fatalf("missing file.read row: %q", got)
	}
	if !strings.Contains(got, "file.delete") || !strings.Contains(got, "destructive") {
		t.Fatalf("missing file.delete row: %q", got)
	}
	if !strings.Contains(got, "reads files") || !strings.Contains(got, "deletes files") {
		t.Fatalf("missing reason values: %q", got)
	}
}

func TestToolListCommandFilterByRiskLevel(t *testing.T) {
	now := time.Now().UTC()
	store := &mockToolClassificationStore{
		classifications: []types.ToolClassification{
			{
				ToolName:       "file.read",
				RiskLevel:      types.RiskReadOnly,
				Reason:         "reads files",
				AutoClassified: true,
				UpdatedBy:      "auto-classify",
				UpdatedAt:      now,
			},
			{
				ToolName:       "file.delete",
				RiskLevel:      types.RiskDestructive,
				Reason:         "deletes files",
				AutoClassified: false,
				UpdatedBy:      "admin",
				UpdatedAt:      now,
			},
		},
	}
	outBuf, _ := setupToolTest(t, store)

	if err := toolListCommand([]string{"--risk-level", "read_only"}); err != nil {
		t.Fatalf("tool list by risk level: %v", err)
	}

	if store.filterLevel != types.RiskReadOnly {
		t.Fatalf("expected filter level read_only, got %q", store.filterLevel)
	}

	got := outBuf.String()
	if !strings.Contains(got, "file.read") {
		t.Fatalf("expected file.read in filtered output, got %q", got)
	}
	if strings.Contains(got, "file.delete") {
		t.Fatalf("did not expect file.delete in read_only filtered output, got %q", got)
	}
}

func TestToolListCommandJSONFormat(t *testing.T) {
	now := time.Now().UTC()
	store := &mockToolClassificationStore{
		classifications: []types.ToolClassification{
			{
				ToolName:       "db.query",
				RiskLevel:      types.RiskReadOnly,
				Reason:         "database query",
				AutoClassified: true,
				UpdatedBy:      "auto-classify",
				UpdatedAt:      now,
			},
		},
	}
	outBuf, _ := setupToolTest(t, store)

	if err := toolListCommand([]string{"--format", "json"}); err != nil {
		t.Fatalf("tool list json: %v", err)
	}

	var result []types.ToolClassification
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("decode json output: %v (output=%q)", err, outBuf.String())
	}
	if len(result) != 1 || result[0].ToolName != "db.query" {
		t.Fatalf("unexpected json result: %+v", result)
	}
}

func TestToolListCommandInvalidRiskLevel(t *testing.T) {
	store := &mockToolClassificationStore{}
	setupToolTest(t, store)

	err := toolListCommand([]string{"--risk-level", "bogus"})
	if err == nil {
		t.Fatal("expected error for invalid risk level")
	}
	if !strings.Contains(err.Error(), "invalid risk level") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolListCommandInvalidFormat(t *testing.T) {
	store := &mockToolClassificationStore{}
	setupToolTest(t, store)

	err := toolListCommand([]string{"--format", "yaml"})
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "invalid format") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolListCommandPositionalArgs(t *testing.T) {
	store := &mockToolClassificationStore{}
	setupToolTest(t, store)

	err := toolListCommand([]string{"extra"})
	if err == nil {
		t.Fatal("expected error for positional arguments")
	}
	if !strings.Contains(err.Error(), "does not accept positional arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolListCommandStoreError(t *testing.T) {
	store := &mockToolClassificationStore{
		getAllErr: errors.New("connection refused"),
	}
	setupToolTest(t, store)

	err := toolListCommand(nil)
	if err == nil {
		t.Fatal("expected error when store fails")
	}
	if !strings.Contains(err.Error(), "list tool classifications") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolListCommandStoreErrorByRiskLevel(t *testing.T) {
	store := &mockToolClassificationStore{
		getByRiskErr: errors.New("connection refused"),
	}
	setupToolTest(t, store)

	err := toolListCommand([]string{"--risk-level", "write"})
	if err == nil {
		t.Fatal("expected error when store GetByRiskLevel fails")
	}
	if !strings.Contains(err.Error(), "list tool classifications") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolListCommandLoadStoreError(t *testing.T) {
	origLoad := loadToolClassificationStoreFunc
	origOut := stdout
	origErr := stderr
	t.Cleanup(func() {
		loadToolClassificationStoreFunc = origLoad
		stdout = origOut
		stderr = origErr
	})

	loadToolClassificationStoreFunc = func(ctx context.Context) (storepkg.ToolClassificationStore, func() error, error) {
		_ = ctx
		return nil, nil, errors.New("db unreachable")
	}
	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}

	err := toolListCommand(nil)
	if err == nil {
		t.Fatal("expected error when load store fails")
	}
	if !strings.Contains(err.Error(), "db unreachable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolClassifyCommandSuccess(t *testing.T) {
	store := &mockToolClassificationStore{}
	outBuf, _ := setupToolTest(t, store)

	err := toolClassifyCommand([]string{"--risk-level", "write", "--reason", "modifies data", "db.update"})
	if err != nil {
		t.Fatalf("tool classify: %v", err)
	}

	if len(store.upserted) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(store.upserted))
	}
	tc := store.upserted[0]
	if tc.ToolName != "db.update" {
		t.Fatalf("expected tool name db.update, got %q", tc.ToolName)
	}
	if tc.RiskLevel != types.RiskWrite {
		t.Fatalf("expected risk level write, got %q", tc.RiskLevel)
	}
	if tc.Reason != "modifies data" {
		t.Fatalf("expected reason 'modifies data', got %q", tc.Reason)
	}
	if tc.AutoClassified {
		t.Fatal("expected AutoClassified=false for manual classification")
	}
	if tc.UpdatedBy != "admin" {
		t.Fatalf("expected updated by 'admin', got %q", tc.UpdatedBy)
	}

	got := outBuf.String()
	if !strings.Contains(got, "classified db.update as write") {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestToolClassifyCommandMissingToolName(t *testing.T) {
	store := &mockToolClassificationStore{}
	setupToolTest(t, store)

	err := toolClassifyCommand([]string{"--risk-level", "read_only"})
	if err == nil {
		t.Fatal("expected error for missing tool name")
	}
	if !strings.Contains(err.Error(), "requires exactly one argument") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolClassifyCommandMissingRiskLevel(t *testing.T) {
	store := &mockToolClassificationStore{}
	setupToolTest(t, store)

	err := toolClassifyCommand([]string{"db.query"})
	if err == nil {
		t.Fatal("expected error for missing risk level")
	}
	if !strings.Contains(err.Error(), "--risk-level is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolClassifyCommandInvalidRiskLevel(t *testing.T) {
	store := &mockToolClassificationStore{}
	setupToolTest(t, store)

	err := toolClassifyCommand([]string{"--risk-level", "dangerous", "db.drop"})
	if err == nil {
		t.Fatal("expected error for invalid risk level")
	}
	if !strings.Contains(err.Error(), "invalid risk level") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolClassifyCommandUpsertError(t *testing.T) {
	store := &mockToolClassificationStore{
		upsertErr: errors.New("constraint violation"),
	}
	setupToolTest(t, store)

	err := toolClassifyCommand([]string{"--risk-level", "write", "db.update"})
	if err == nil {
		t.Fatal("expected error when upsert fails")
	}
	if !strings.Contains(err.Error(), "classify tool") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolClassifyCommandLoadStoreError(t *testing.T) {
	origLoad := loadToolClassificationStoreFunc
	origOut := stdout
	origErr := stderr
	t.Cleanup(func() {
		loadToolClassificationStoreFunc = origLoad
		stdout = origOut
		stderr = origErr
	})

	loadToolClassificationStoreFunc = func(ctx context.Context) (storepkg.ToolClassificationStore, func() error, error) {
		_ = ctx
		return nil, nil, errors.New("db unreachable")
	}
	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}

	err := toolClassifyCommand([]string{"--risk-level", "write", "db.update"})
	if err == nil {
		t.Fatal("expected error when load store fails")
	}
	if !strings.Contains(err.Error(), "db unreachable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolAutoClassifyCommandReclassifiesAutoClassified(t *testing.T) {
	now := time.Now().UTC()
	store := &mockToolClassificationStore{
		classifications: []types.ToolClassification{
			{
				ToolName:       "file.read",
				RiskLevel:      types.RiskUnknown,
				Reason:         "old reason",
				AutoClassified: true,
				UpdatedBy:      "auto-classify",
				UpdatedAt:      now,
			},
			{
				ToolName:       "file.delete",
				RiskLevel:      types.RiskUnknown,
				Reason:         "old reason",
				AutoClassified: true,
				UpdatedBy:      "auto-classify",
				UpdatedAt:      now,
			},
		},
	}
	outBuf, _ := setupToolTest(t, store)

	if err := toolAutoClassifyCommand(nil); err != nil {
		t.Fatalf("tool auto-classify: %v", err)
	}

	if len(store.upserted) != 2 {
		t.Fatalf("expected 2 upserts for auto-classified tools, got %d", len(store.upserted))
	}

	for _, tc := range store.upserted {
		if tc.UpdatedBy != "auto-reclassify" {
			t.Fatalf("expected updated by 'auto-reclassify', got %q", tc.UpdatedBy)
		}
		if !tc.AutoClassified {
			t.Fatalf("expected AutoClassified=true for reclassified tool %q", tc.ToolName)
		}
	}

	got := outBuf.String()
	if !strings.Contains(got, "auto-reclassified 2 tools") {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestToolAutoClassifyCommandSkipsManuallyClassified(t *testing.T) {
	now := time.Now().UTC()
	store := &mockToolClassificationStore{
		classifications: []types.ToolClassification{
			{
				ToolName:       "db.drop",
				RiskLevel:      types.RiskDestructive,
				Reason:         "manually classified",
				AutoClassified: false,
				UpdatedBy:      "admin",
				UpdatedAt:      now,
			},
		},
	}
	outBuf, _ := setupToolTest(t, store)

	if err := toolAutoClassifyCommand(nil); err != nil {
		t.Fatalf("tool auto-classify: %v", err)
	}

	if len(store.upserted) != 0 {
		t.Fatalf("expected no upserts for manually classified tools, got %d", len(store.upserted))
	}

	got := outBuf.String()
	if !strings.Contains(got, "auto-reclassified 0 tools") {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestToolAutoClassifyCommandMixed(t *testing.T) {
	now := time.Now().UTC()
	store := &mockToolClassificationStore{
		classifications: []types.ToolClassification{
			{
				ToolName:       "file.read",
				RiskLevel:      types.RiskUnknown,
				Reason:         "old reason",
				AutoClassified: true,
				UpdatedBy:      "auto-classify",
				UpdatedAt:      now,
			},
			{
				ToolName:       "db.drop",
				RiskLevel:      types.RiskDestructive,
				Reason:         "manually classified",
				AutoClassified: false,
				UpdatedBy:      "admin",
				UpdatedAt:      now,
			},
		},
	}
	outBuf, _ := setupToolTest(t, store)

	if err := toolAutoClassifyCommand(nil); err != nil {
		t.Fatalf("tool auto-classify: %v", err)
	}

	if len(store.upserted) != 1 {
		t.Fatalf("expected 1 upsert (only auto-classified), got %d", len(store.upserted))
	}
	if store.upserted[0].ToolName != "file.read" {
		t.Fatalf("expected file.read to be reclassified, got %q", store.upserted[0].ToolName)
	}

	got := outBuf.String()
	if !strings.Contains(got, "auto-reclassified 1 tools") {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestToolAutoClassifyCommandGetAllError(t *testing.T) {
	store := &mockToolClassificationStore{
		getAllErr: errors.New("connection refused"),
	}
	setupToolTest(t, store)

	err := toolAutoClassifyCommand(nil)
	if err == nil {
		t.Fatal("expected error when GetAll fails")
	}
	if !strings.Contains(err.Error(), "list classifications") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolAutoClassifyCommandUpsertError(t *testing.T) {
	now := time.Now().UTC()
	store := &mockToolClassificationStore{
		classifications: []types.ToolClassification{
			{
				ToolName:       "file.read",
				RiskLevel:      types.RiskUnknown,
				Reason:         "old reason",
				AutoClassified: true,
				UpdatedBy:      "auto-classify",
				UpdatedAt:      now,
			},
		},
		upsertErr: errors.New("write failed"),
	}
	setupToolTest(t, store)

	err := toolAutoClassifyCommand(nil)
	if err == nil {
		t.Fatal("expected error when upsert fails during auto-classify")
	}
	if !strings.Contains(err.Error(), "reclassify tool") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolAutoClassifyCommandLoadStoreError(t *testing.T) {
	origLoad := loadToolClassificationStoreFunc
	origOut := stdout
	origErr := stderr
	t.Cleanup(func() {
		loadToolClassificationStoreFunc = origLoad
		stdout = origOut
		stderr = origErr
	})

	loadToolClassificationStoreFunc = func(ctx context.Context) (storepkg.ToolClassificationStore, func() error, error) {
		_ = ctx
		return nil, nil, errors.New("db unreachable")
	}
	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}

	err := toolAutoClassifyCommand(nil)
	if err == nil {
		t.Fatal("expected error when load store fails")
	}
	if !strings.Contains(err.Error(), "db unreachable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolCommandRoutedViaRunDispatch(t *testing.T) {
	origTool := toolHandler
	origOut := stdout
	origErr := stderr
	t.Cleanup(func() {
		toolHandler = origTool
		stdout = origOut
		stderr = origErr
	})

	var called bool
	toolHandler = func(args []string) error {
		called = true
		if len(args) != 1 || args[0] != "list" {
			t.Fatalf("expected args [list], got %v", args)
		}
		return nil
	}
	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}

	if err := run([]string{"tool", "list"}); err != nil {
		t.Fatalf("run tool list: %v", err)
	}
	if !called {
		t.Fatal("expected tool handler to be called via run dispatch")
	}
}

func TestToolClassifyAllRiskLevels(t *testing.T) {
	levels := []struct {
		level string
		risk  types.RiskLevel
	}{
		{"read_only", types.RiskReadOnly},
		{"write", types.RiskWrite},
		{"destructive", types.RiskDestructive},
		{"unknown", types.RiskUnknown},
	}

	for _, lv := range levels {
		lv := lv
		t.Run(lv.level, func(t *testing.T) {
			store := &mockToolClassificationStore{}
			setupToolTest(t, store)

			err := toolClassifyCommand([]string{"--risk-level", lv.level, "tool." + lv.level})
			if err != nil {
				t.Fatalf("classify with level %s: %v", lv.level, err)
			}
			if len(store.upserted) != 1 {
				t.Fatalf("expected 1 upsert, got %d", len(store.upserted))
			}
			if store.upserted[0].RiskLevel != lv.risk {
				t.Fatalf("expected risk level %s, got %s", lv.risk, store.upserted[0].RiskLevel)
			}
		})
	}
}

func TestToolListCommandEmptyResults(t *testing.T) {
	store := &mockToolClassificationStore{}
	outBuf, _ := setupToolTest(t, store)

	if err := toolListCommand(nil); err != nil {
		t.Fatalf("tool list empty: %v", err)
	}

	got := outBuf.String()
	if !strings.Contains(got, "TOOL_NAME") {
		t.Fatalf("expected table header even with empty results, got %q", got)
	}
}

func TestToolListCommandJSONEmptyResults(t *testing.T) {
	store := &mockToolClassificationStore{}
	outBuf, _ := setupToolTest(t, store)

	if err := toolListCommand([]string{"--format", "json"}); err != nil {
		t.Fatalf("tool list json empty: %v", err)
	}

	var result []types.ToolClassification
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("decode json empty output: %v (output=%q)", err, outBuf.String())
	}
	if result == nil {
		// json.Encoder.Encode with nil slice produces "null", which is fine
		return
	}
	if len(result) != 0 {
		t.Fatalf("expected empty list, got %d items", len(result))
	}
}
