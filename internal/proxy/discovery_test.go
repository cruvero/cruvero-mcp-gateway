package proxy

import (
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestExtractSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "single sentence with period", input: "Does X.", want: "Does X."},
		{name: "two sentences", input: "Does X. Also does Y.", want: "Does X."},
		{name: "no period short", input: "Does X", want: "Does X"},
		{name: "over 100 chars with period in first sentence",
			input: "Short first. " + string(make([]byte, 120)),
			want:  "Short first.",
		},
		{name: "over 100 chars no period",
			input: string(repeatChar('a', 120)),
			want:  string(repeatChar('a', 97)) + "...",
		},
		{name: "period not followed by space is not boundary",
			input: "Uses v1.2 for processing",
			want:  "Uses v1.2 for processing",
		},
		{name: "period at end of string only",
			input: "Single statement ending.",
			want:  "Single statement ending.",
		},
		{name: "exactly 100 chars",
			input: string(repeatChar('b', 100)),
			want:  string(repeatChar('b', 100)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractSummary(tt.input)
			if got != tt.want {
				t.Errorf("extractSummary(%q) = %q, want %q", truncateForLog(tt.input), got, tt.want)
			}
		})
	}
}

func TestCategoryFromFederatedName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		input string
		want  string
	}{
		{name: "standard federated name", input: "mcp.github.create_issue", want: "github"},
		{name: "no prefix", input: "toolname", want: ""},
		{name: "empty", input: "", want: ""},
		{name: "mcp prefix only", input: "mcp.", want: ""},
		{name: "mcp prefix with server only", input: "mcp.github", want: ""},
		{name: "nested dots", input: "mcp.server.ns.tool", want: "server"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := categoryFromFederatedName(tt.input)
			if got != tt.want {
				t.Errorf("categoryFromFederatedName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDiscoveryIndex_Search(t *testing.T) {
	t.Parallel()

	roHint := mcp.ToBoolPtr(true)
	tools := []ToolDefinition{
		{Name: "mcp.github.create_issue", Description: "Create a new issue in a GitHub repository.", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &mcp.ToolAnnotation{Title: "Create Issue", ReadOnlyHint: roHint}},
		{Name: "mcp.github.list_issues", Description: "List issues in a GitHub repository.", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &mcp.ToolAnnotation{Title: "List Issues"}},
		{Name: "mcp.github.close_issue", Description: "Close an existing issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "mcp.slack.send_message", Description: "Send a message to a Slack channel.", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &mcp.ToolAnnotation{Title: "Send Message"}},
		{Name: "mcp.slack.list_channels", Description: "List available Slack channels.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "mcp.jira.create_ticket", Description: "Create a Jira ticket for issue tracking.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}

	idx := NewDiscoveryIndex()
	idx.Index(tools)

	tests := []struct {
		name            string
		query           string
		category        string
		limit           int
		wantMinResults  int
		wantMaxResults  int
		wantTotal       int
		wantFirstName   string
		checkDeferred   bool
	}{
		{
			name: "exact name match", query: "mcp.github.create_issue",
			wantMinResults: 1, wantFirstName: "mcp.github.create_issue", checkDeferred: true,
		},
		{
			name: "name contains match", query: "issue",
			wantMinResults: 3, // create_issue, list_issues, close_issue + jira description
		},
		{
			name: "description keyword", query: "Slack",
			wantMinResults: 2,
		},
		{
			name: "title match", query: "Create Issue",
			wantMinResults: 1,
		},
		{
			name: "category filter github", query: "issue", category: "github",
			wantMinResults: 2, wantMaxResults: 3,
		},
		{
			name: "category filter slack", query: "message", category: "slack",
			wantMinResults: 1, wantMaxResults: 1,
		},
		{
			name: "limit clamped to default", query: "issue", limit: 0,
			wantMinResults: 1,
		},
		{
			name: "limit clamped to max", query: "issue", limit: 100,
			wantMinResults: 1,
		},
		{
			name: "custom limit", query: "issue", limit: 1,
			wantMaxResults: 1,
		},
		{
			name: "no matches", query: "nonexistent",
			wantMinResults: 0, wantMaxResults: 0, wantTotal: 0,
		},
		{
			name: "empty query", query: "",
			wantMinResults: 0, wantMaxResults: 0, wantTotal: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			results, total := idx.Search(tt.query, tt.category, 0, tt.limit)

			if tt.wantTotal > 0 && total != tt.wantTotal {
				t.Errorf("total = %d, want %d", total, tt.wantTotal)
			}
			if len(results) < tt.wantMinResults {
				t.Errorf("got %d results, want at least %d", len(results), tt.wantMinResults)
			}
			if tt.wantMaxResults > 0 && len(results) > tt.wantMaxResults {
				t.Errorf("got %d results, want at most %d", len(results), tt.wantMaxResults)
			}
			if tt.wantFirstName != "" && len(results) > 0 && results[0].Name != tt.wantFirstName {
				t.Errorf("first result name = %q, want %q", results[0].Name, tt.wantFirstName)
			}
			if tt.checkDeferred {
				for _, r := range results {
					if !r.DeferLoading {
						t.Errorf("result %q should have DeferLoading=true", r.Name)
					}
					if string(r.InputSchema) != `{}` {
						t.Errorf("result %q should have stripped schema, got %s", r.Name, string(r.InputSchema))
					}
				}
			}
		})
	}
}

func TestDiscoveryIndex_GetTools(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "mcp.github.create_issue", Description: "Create issue", InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`)},
		{Name: "mcp.github.list_issues", Description: "List issues", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "mcp.slack.send_message", Description: "Send message", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}

	idx := NewDiscoveryIndex()
	idx.Index(tools)

	tests := []struct {
		name      string
		names     []string
		wantCount int
	}{
		{name: "single found", names: []string{"mcp.github.create_issue"}, wantCount: 1},
		{name: "multiple found", names: []string{"mcp.github.create_issue", "mcp.slack.send_message"}, wantCount: 2},
		{name: "some not found", names: []string{"mcp.github.create_issue", "nonexistent"}, wantCount: 1},
		{name: "none found", names: []string{"nonexistent"}, wantCount: 0},
		{name: "empty input", names: nil, wantCount: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			results := idx.GetTools(tt.names)
			if len(results) != tt.wantCount {
				t.Errorf("got %d tools, want %d", len(results), tt.wantCount)
			}
		})
	}

	// Verify full definition is returned (not stripped)
	t.Run("full definition preserved", func(t *testing.T) {
		t.Parallel()
		results := idx.GetTools([]string{"mcp.github.create_issue"})
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if string(results[0].InputSchema) == "{}" {
			t.Error("expected full input schema, got stripped version")
		}
	})

	// Verify max 20 clamping
	t.Run("over 20 clamped", func(t *testing.T) {
		t.Parallel()
		names := make([]string, 25)
		for i := range names {
			names[i] = "mcp.github.create_issue"
		}
		results := idx.GetTools(names)
		if len(results) > maxGetToolsNames {
			t.Errorf("got %d results, want at most %d", len(results), maxGetToolsNames)
		}
	})
}

func TestDiscoveryIndex_Rebuild(t *testing.T) {
	t.Parallel()

	idx := NewDiscoveryIndex()

	toolsV1 := []ToolDefinition{
		{Name: "mcp.github.create_issue", Description: "Create issue", InputSchema: json.RawMessage(`{}`)},
	}
	idx.Index(toolsV1)

	results, total := idx.Search("issue", "", 0, 10)
	if total != 1 || len(results) != 1 {
		t.Fatalf("v1: expected 1 result, got total=%d results=%d", total, len(results))
	}

	toolsV2 := []ToolDefinition{
		{Name: "mcp.slack.send_message", Description: "Send message", InputSchema: json.RawMessage(`{}`)},
	}
	idx.Index(toolsV2)

	results, total = idx.Search("issue", "", 0, 10)
	if total != 0 || len(results) != 0 {
		t.Fatalf("v2: expected 0 results after rebuild, got total=%d results=%d", total, len(results))
	}

	results, total = idx.Search("message", "", 0, 10)
	if total != 1 || len(results) != 1 {
		t.Fatalf("v2: expected 1 result for message, got total=%d results=%d", total, len(results))
	}
}

func TestDiscoveryIndex_ApplyMetadata_OverridesCategory(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "mcp.github.create_issue", Description: "Create a new issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	idx := NewDiscoveryIndex()
	idx.Index(tools)

	// Before metadata, category is inferred as "github".
	results, _ := idx.Browse("github", 0, 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 tool in github category, got %d", len(results))
	}

	// Apply metadata overriding category.
	idx.ApplyMetadata([]ToolMetadata{
		{ToolName: "mcp.github.create_issue", Category: "issue-management"},
	})

	results, _ = idx.Browse("github", 0, 10)
	if len(results) != 0 {
		t.Fatalf("expected 0 tools in github category after override, got %d", len(results))
	}
	results, _ = idx.Browse("issue-management", 0, 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 tool in issue-management category, got %d", len(results))
	}
}

func TestDiscoveryIndex_ApplyMetadata_OverridesSummary(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "mcp.github.create_issue", Description: "Create a new issue in a GitHub repository. Supports labels and assignees.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	idx := NewDiscoveryIndex()
	idx.Index(tools)

	idx.ApplyMetadata([]ToolMetadata{
		{ToolName: "mcp.github.create_issue", Summary: "Custom summary for issue creation"},
	})

	results, _ := idx.Search("issue", "", 0, 10)
	if len(results) == 0 {
		t.Fatal("expected search results")
	}
	if results[0].Description != "Custom summary for issue creation" {
		t.Fatalf("expected custom summary, got %q", results[0].Description)
	}
}

func TestDiscoveryIndex_ApplyMetadata_TagsSearchable(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "mcp.github.create_issue", Description: "Create issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "mcp.slack.send_message", Description: "Send message.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	idx := NewDiscoveryIndex()
	idx.Index(tools)

	idx.ApplyMetadata([]ToolMetadata{
		{ToolName: "mcp.github.create_issue", Tags: []string{"vcs", "tracking"}},
	})

	// Search by tag keyword.
	results, total := idx.Search("tracking", "", 0, 10)
	if total != 1 {
		t.Fatalf("expected 1 match for tag 'tracking', got %d", total)
	}
	if results[0].Name != "mcp.github.create_issue" {
		t.Fatalf("expected mcp.github.create_issue, got %q", results[0].Name)
	}
}

func TestDiscoveryIndex_ApplyMetadata_PriorityAffectsOrder(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "mcp.github.create_issue", Description: "Create issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "mcp.jira.create_issue", Description: "Create issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	idx := NewDiscoveryIndex()
	idx.Index(tools)

	// Without priority, both score the same; alphabetical wins.
	results, _ := idx.Search("issue", "", 0, 10)
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	if results[0].Name != "mcp.github.create_issue" {
		t.Fatalf("expected github first (alphabetical), got %q", results[0].Name)
	}

	// Apply higher priority to jira.
	idx.ApplyMetadata([]ToolMetadata{
		{ToolName: "mcp.jira.create_issue", Priority: 10},
	})

	results, _ = idx.Search("issue", "", 0, 10)
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	if results[0].Name != "mcp.jira.create_issue" {
		t.Fatalf("expected jira first with higher priority, got %q", results[0].Name)
	}
}

func TestDiscoveryIndex_ApplyMetadata_UnknownToolSkipped(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "mcp.github.create_issue", Description: "Create issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	idx := NewDiscoveryIndex()
	idx.Index(tools)

	// Should not panic.
	idx.ApplyMetadata([]ToolMetadata{
		{ToolName: "nonexistent.tool", Category: "test"},
	})

	// Original tool should be unaffected.
	results, _ := idx.Search("issue", "", 0, 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestDiscoveryIndex_ApplyMetadata_Idempotent(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "mcp.github.create_issue", Description: "Create issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	idx := NewDiscoveryIndex()
	idx.Index(tools)

	metadata := []ToolMetadata{
		{ToolName: "mcp.github.create_issue", Category: "custom", Tags: []string{"tag1"}, Priority: 5},
	}

	idx.ApplyMetadata(metadata)
	stats1 := idx.Stats()
	idx.ApplyMetadata(metadata) // Apply again.
	stats2 := idx.Stats()

	if stats1.TotalTools != stats2.TotalTools {
		t.Fatalf("expected same total tools after idempotent apply")
	}
	if stats1.Categories["custom"] != stats2.Categories["custom"] {
		t.Fatalf("expected same category count")
	}
}

func TestDiscoveryIndex_Browse(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "mcp.github.create_issue", Description: "Create issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "mcp.github.list_issues", Description: "List issues.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "mcp.slack.send_message", Description: "Send message.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "mcp.jira.create_ticket", Description: "Create ticket.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	idx := NewDiscoveryIndex()
	idx.Index(tools)

	t.Run("all tools", func(t *testing.T) {
		t.Parallel()
		results, total := idx.Browse("", 0, 10)
		if total != 4 {
			t.Fatalf("expected 4 total, got %d", total)
		}
		if len(results) != 4 {
			t.Fatalf("expected 4 results, got %d", len(results))
		}
		// Should be alphabetically sorted.
		if results[0].Name != "mcp.github.create_issue" {
			t.Fatalf("expected first result mcp.github.create_issue, got %q", results[0].Name)
		}
	})

	t.Run("category filter", func(t *testing.T) {
		t.Parallel()
		results, total := idx.Browse("github", 0, 10)
		if total != 2 {
			t.Fatalf("expected 2 github tools, got %d", total)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
	})

	t.Run("pagination offset", func(t *testing.T) {
		t.Parallel()
		results, total := idx.Browse("", 2, 2)
		if total != 4 {
			t.Fatalf("expected 4 total, got %d", total)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
	})

	t.Run("offset beyond total", func(t *testing.T) {
		t.Parallel()
		results, total := idx.Browse("", 10, 10)
		if total != 4 {
			t.Fatalf("expected 4 total, got %d", total)
		}
		if len(results) != 0 {
			t.Fatalf("expected 0 results, got %d", len(results))
		}
	})

	t.Run("results have deferred loading", func(t *testing.T) {
		t.Parallel()
		results, _ := idx.Browse("", 0, 1)
		if len(results) < 1 {
			t.Fatal("expected at least 1 result")
		}
		if !results[0].DeferLoading {
			t.Fatal("expected DeferLoading=true")
		}
		if string(results[0].InputSchema) != "{}" {
			t.Fatalf("expected stripped schema, got %s", string(results[0].InputSchema))
		}
	})
}

func TestDiscoveryIndex_Stats(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "mcp.github.create_issue", Description: "Create issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "mcp.github.list_issues", Description: "List issues.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "mcp.slack.send_message", Description: "Send message.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	idx := NewDiscoveryIndex()
	idx.Index(tools)

	stats := idx.Stats()
	if stats.TotalTools != 3 {
		t.Fatalf("expected 3 total tools, got %d", stats.TotalTools)
	}
	if stats.Categories["github"] != 2 {
		t.Fatalf("expected 2 github tools, got %d", stats.Categories["github"])
	}
	if stats.Categories["slack"] != 1 {
		t.Fatalf("expected 1 slack tool, got %d", stats.Categories["slack"])
	}
}

func TestDiscoveryIndex_Stats_Empty(t *testing.T) {
	t.Parallel()

	idx := NewDiscoveryIndex()
	stats := idx.Stats()
	if stats.TotalTools != 0 {
		t.Fatalf("expected 0 total tools, got %d", stats.TotalTools)
	}
	if len(stats.Categories) != 0 {
		t.Fatalf("expected 0 categories, got %d", len(stats.Categories))
	}
}

func TestDiscoveryIndex_MetadataPreservedAcrossRebuild(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "mcp.github.create_issue", Description: "Create issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	idx := NewDiscoveryIndex()
	idx.Index(tools)

	idx.ApplyMetadata([]ToolMetadata{
		{ToolName: "mcp.github.create_issue", Category: "custom-cat", Tags: []string{"important"}, Priority: 5},
	})

	// Rebuild with same tools — metadata should survive.
	idx.Index(tools)

	results, _ := idx.Browse("custom-cat", 0, 10)
	if len(results) != 1 {
		t.Fatalf("expected metadata category to survive rebuild, got %d results", len(results))
	}

	// Tags should still be searchable after rebuild.
	results, total := idx.Search("important", "", 0, 10)
	if total != 1 {
		t.Fatalf("expected tag 'important' to survive rebuild, got %d matches", total)
	}
	if results[0].Name != "mcp.github.create_issue" {
		t.Fatalf("expected mcp.github.create_issue, got %q", results[0].Name)
	}
}

func TestDiscoveryIndex_SearchPagination(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "mcp.github.create_issue", Description: "Manage issues.", InputSchema: json.RawMessage(`{}`)},
		{Name: "mcp.github.list_issues", Description: "Manage issues.", InputSchema: json.RawMessage(`{}`)},
		{Name: "mcp.github.close_issue", Description: "Manage issues.", InputSchema: json.RawMessage(`{}`)},
	}
	idx := NewDiscoveryIndex()
	idx.Index(tools)

	// Page 1: offset=0, limit=2.
	results, total := idx.Search("issue", "", 0, 2)
	if total != 3 {
		t.Fatalf("expected 3 total matches, got %d", total)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results on page 1, got %d", len(results))
	}

	// Page 2: offset=2, limit=2.
	results, total = idx.Search("issue", "", 2, 2)
	if total != 3 {
		t.Fatalf("expected 3 total matches, got %d", total)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result on page 2, got %d", len(results))
	}

	// Beyond range.
	results, total = idx.Search("issue", "", 10, 2)
	if total != 3 {
		t.Fatalf("expected 3 total matches, got %d", total)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results beyond range, got %d", len(results))
	}
}

func TestDiscoveryIndex_ApplyMetadata_SummarySearchable(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "mcp.github.create_issue", Description: "Create a new issue.", InputSchema: json.RawMessage(`{}`)},
	}
	idx := NewDiscoveryIndex()
	idx.Index(tools)

	// Apply metadata with a summary containing a unique keyword.
	idx.ApplyMetadata([]ToolMetadata{
		{ToolName: "mcp.github.create_issue", Summary: "Open a bug tracker ticket"},
	})

	// The unique keyword "tracker" should now be searchable.
	results, total := idx.Search("tracker", "", 0, 10)
	if total != 1 {
		t.Fatalf("expected 1 match for 'tracker' in metadata summary, got %d", total)
	}
	if results[0].Name != "mcp.github.create_issue" {
		t.Fatalf("expected mcp.github.create_issue, got %q", results[0].Name)
	}
}

func repeatChar(c byte, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return b
}

func truncateForLog(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}
