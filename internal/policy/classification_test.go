package policy

import (
	"testing"

	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestAutoClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		toolName    string
		description string
		wantLevel   types.RiskLevel
	}{
		{name: "destructive delete repo", toolName: "github.delete_repo", wantLevel: types.RiskDestructive},
		{name: "destructive drop table", toolName: "db.drop_table", wantLevel: types.RiskDestructive},
		{name: "destructive remove file", toolName: "fs-remove-file", wantLevel: types.RiskDestructive},
		{name: "destructive k8s kill pod", toolName: "k8s/kill/pod", wantLevel: types.RiskDestructive},
		{name: "read-only get pods", toolName: "k8s.get_pods", wantLevel: types.RiskReadOnly},
		{name: "read-only list users", toolName: "admin.list_users", wantLevel: types.RiskReadOnly},
		{name: "read-only search", toolName: "search-code", wantLevel: types.RiskReadOnly},
		{name: "write create PR", toolName: "github.create_pull_request", wantLevel: types.RiskWrite},
		{name: "write update record", toolName: "db.update_record", wantLevel: types.RiskWrite},
		{name: "write deploy service", toolName: "deploy-service", wantLevel: types.RiskWrite},
		{name: "unknown custom tool", toolName: "custom_tool_v2", wantLevel: types.RiskUnknown},
		{name: "empty name", toolName: "", wantLevel: types.RiskUnknown},
		{name: "description fallback destructive", toolName: "my_tool", description: "This will delete everything", wantLevel: types.RiskDestructive},
		{name: "description fallback read", toolName: "my_tool", description: "Lists all available items", wantLevel: types.RiskReadOnly},
		{name: "description fallback write", toolName: "my_tool", description: "Creates a new resource", wantLevel: types.RiskWrite},
		{name: "case insensitive", toolName: "GitHub.DELETE_Repo", wantLevel: types.RiskDestructive},
		{name: "dot separator", toolName: "mcp.server.get.status", wantLevel: types.RiskReadOnly},
		{name: "hyphen separator", toolName: "api-list-items", wantLevel: types.RiskReadOnly},
		{name: "underscore separator", toolName: "data_export_results", wantLevel: types.RiskReadOnly},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			level, reason := AutoClassify(tt.toolName, tt.description)
			if level != tt.wantLevel {
				t.Fatalf("AutoClassify(%q, %q) = %q (reason: %s), want %q", tt.toolName, tt.description, level, reason, tt.wantLevel)
			}
			if reason == "" {
				t.Fatal("expected non-empty reason")
			}
		})
	}
}

func TestRiskLevelIsValid(t *testing.T) {
	t.Parallel()

	valid := []types.RiskLevel{types.RiskReadOnly, types.RiskWrite, types.RiskDestructive, types.RiskUnknown}
	for _, level := range valid {
		if !level.IsValid() {
			t.Fatalf("expected %q to be valid", level)
		}
	}

	if types.RiskLevel("bogus").IsValid() {
		t.Fatal("expected bogus risk level to be invalid")
	}
}
