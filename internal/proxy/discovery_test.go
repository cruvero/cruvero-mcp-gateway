package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/cruvero/mcp-gateway/internal/search"
)

type failingSearchEngine struct{}

func (f failingSearchEngine) Index(_ context.Context, _ []search.Document) error { return nil }
func (f failingSearchEngine) Search(_ context.Context, _ string, _ int) ([]search.ScoredResult, error) {
	return nil, fmt.Errorf("boom")
}
func (f failingSearchEngine) Remove(_ string) {}
func (f failingSearchEngine) Ready() bool     { return true }

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
		name  string
		input string
		want  string
	}{
		{name: "new format", input: "github.create_issue", want: "github"},
		{name: "new format nested", input: "k8s.ns.list_pods", want: "k8s"},
		{name: "no dot", input: "toolname", want: ""},
		{name: "empty", input: "", want: ""},
		{name: "legacy mcp prefix only", input: "mcp.", want: ""},
		{name: "legacy mcp prefix with server only", input: "mcp.github", want: ""},
		{name: "legacy mcp format", input: "mcp.server.ns.tool", want: "server"},
		{name: "legacy mcp standard", input: "mcp.github.create_issue", want: "github"},
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
		{Name: "github.create_issue", Description: "Create a new issue in a GitHub repository.", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &mcp.ToolAnnotation{Title: "Create Issue", ReadOnlyHint: roHint}},
		{Name: "github.list_issues", Description: "List issues in a GitHub repository.", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &mcp.ToolAnnotation{Title: "List Issues"}},
		{Name: "github.close_issue", Description: "Close an existing issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "slack.send_message", Description: "Send a message to a Slack channel.", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &mcp.ToolAnnotation{Title: "Send Message"}},
		{Name: "slack.list_channels", Description: "List available Slack channels.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "jira.create_ticket", Description: "Create a Jira ticket for issue tracking.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}

	idx := NewDiscoveryIndex()
	idx.Index(tools)

	tests := []struct {
		name           string
		query          string
		category       string
		limit          int
		wantMinResults int
		wantMaxResults int
		wantTotal      int
		wantFirstName  string
		checkDeferred  bool
	}{
		{
			name: "exact name match", query: "github.create_issue",
			wantMinResults: 1, wantFirstName: "github.create_issue", checkDeferred: true,
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
		{Name: "github.create_issue", Description: "Create issue", InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`)},
		{Name: "github.list_issues", Description: "List issues", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "slack.send_message", Description: "Send message", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}

	idx := NewDiscoveryIndex()
	idx.Index(tools)

	tests := []struct {
		name      string
		names     []string
		wantCount int
	}{
		{name: "single found", names: []string{"github.create_issue"}, wantCount: 1},
		{name: "multiple found", names: []string{"github.create_issue", "slack.send_message"}, wantCount: 2},
		{name: "some not found", names: []string{"github.create_issue", "nonexistent"}, wantCount: 1},
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
		results := idx.GetTools([]string{"github.create_issue"})
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
			names[i] = "github.create_issue"
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
		{Name: "github.create_issue", Description: "Create issue", InputSchema: json.RawMessage(`{}`)},
	}
	idx.Index(toolsV1)

	results, total := idx.Search("issue", "", 0, 10)
	if total != 1 || len(results) != 1 {
		t.Fatalf("v1: expected 1 result, got total=%d results=%d", total, len(results))
	}

	toolsV2 := []ToolDefinition{
		{Name: "slack.send_message", Description: "Send message", InputSchema: json.RawMessage(`{}`)},
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

func TestDiscoveryIndex_EngineErrorFallbackCounter(t *testing.T) {
	t.Parallel()

	idx := NewDiscoveryIndex()
	idx.SetEngine(failingSearchEngine{})
	idx.Index([]ToolDefinition{
		{Name: "github.create_issue", Description: "Create issue", InputSchema: json.RawMessage(`{}`)},
	})

	results, total := idx.Search("issue", "", 0, 10)
	if total == 0 || len(results) == 0 {
		t.Fatalf("expected substring fallback results, got total=%d len=%d", total, len(results))
	}
	if got := idx.EngineErrorFallbacks(); got != 1 {
		t.Fatalf("expected engine fallback count 1, got %d", got)
	}
}

func TestDiscoveryIndex_ApplyMetadata_OverridesCategory(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "github.create_issue", Description: "Create a new issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
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
		{ToolName: "github.create_issue", Category: "issue-management"},
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
		{Name: "github.create_issue", Description: "Create a new issue in a GitHub repository. Supports labels and assignees.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	idx := NewDiscoveryIndex()
	idx.Index(tools)

	idx.ApplyMetadata([]ToolMetadata{
		{ToolName: "github.create_issue", Summary: "Custom summary for issue creation"},
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
		{Name: "github.create_issue", Description: "Create issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "slack.send_message", Description: "Send message.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	idx := NewDiscoveryIndex()
	idx.Index(tools)

	idx.ApplyMetadata([]ToolMetadata{
		{ToolName: "github.create_issue", Tags: []string{"vcs", "tracking"}},
	})

	// Search by tag keyword.
	results, total := idx.Search("tracking", "", 0, 10)
	if total != 1 {
		t.Fatalf("expected 1 match for tag 'tracking', got %d", total)
	}
	if results[0].Name != "github.create_issue" {
		t.Fatalf("expected github.create_issue, got %q", results[0].Name)
	}
}

func TestDiscoveryIndex_ApplyMetadata_PriorityAffectsOrder(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "github.create_issue", Description: "Create issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "jira.create_issue", Description: "Create issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	idx := NewDiscoveryIndex()
	idx.Index(tools)

	// Without priority, both score the same; alphabetical wins.
	results, _ := idx.Search("issue", "", 0, 10)
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	if results[0].Name != "github.create_issue" {
		t.Fatalf("expected github first (alphabetical), got %q", results[0].Name)
	}

	// Apply higher priority to jira.
	idx.ApplyMetadata([]ToolMetadata{
		{ToolName: "jira.create_issue", Priority: 10},
	})

	results, _ = idx.Search("issue", "", 0, 10)
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	if results[0].Name != "jira.create_issue" {
		t.Fatalf("expected jira first with higher priority, got %q", results[0].Name)
	}
}

func TestDiscoveryIndex_ApplyMetadata_UnknownToolSkipped(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "github.create_issue", Description: "Create issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
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
		{Name: "github.create_issue", Description: "Create issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	idx := NewDiscoveryIndex()
	idx.Index(tools)

	metadata := []ToolMetadata{
		{ToolName: "github.create_issue", Category: "custom", Tags: []string{"tag1"}, Priority: 5},
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
		{Name: "github.create_issue", Description: "Create issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "github.list_issues", Description: "List issues.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "slack.send_message", Description: "Send message.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "jira.create_ticket", Description: "Create ticket.", InputSchema: json.RawMessage(`{"type":"object"}`)},
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
		if results[0].Name != "github.create_issue" {
			t.Fatalf("expected first result github.create_issue, got %q", results[0].Name)
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

	t.Run("negative offset clamped to zero", func(t *testing.T) {
		t.Parallel()
		results, total := idx.Browse("", -5, 10)
		if total != 4 {
			t.Fatalf("expected 4 total, got %d", total)
		}
		if len(results) != 4 {
			t.Fatalf("expected 4 results with clamped offset, got %d", len(results))
		}
	})
}

func TestDiscoveryIndex_Stats(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "github.create_issue", Description: "Create issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "github.list_issues", Description: "List issues.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "slack.send_message", Description: "Send message.", InputSchema: json.RawMessage(`{"type":"object"}`)},
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
		{Name: "github.create_issue", Description: "Create issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	idx := NewDiscoveryIndex()
	idx.Index(tools)

	idx.ApplyMetadata([]ToolMetadata{
		{ToolName: "github.create_issue", Category: "custom-cat", Tags: []string{"important"}, Priority: 5},
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
	if results[0].Name != "github.create_issue" {
		t.Fatalf("expected github.create_issue, got %q", results[0].Name)
	}
}

func TestDiscoveryIndex_SearchPagination(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "github.create_issue", Description: "Manage issues.", InputSchema: json.RawMessage(`{}`)},
		{Name: "github.list_issues", Description: "Manage issues.", InputSchema: json.RawMessage(`{}`)},
		{Name: "github.close_issue", Description: "Manage issues.", InputSchema: json.RawMessage(`{}`)},
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
		{Name: "github.create_issue", Description: "Create a new issue.", InputSchema: json.RawMessage(`{}`)},
	}
	idx := NewDiscoveryIndex()
	idx.Index(tools)

	// Apply metadata with a summary containing a unique keyword.
	idx.ApplyMetadata([]ToolMetadata{
		{ToolName: "github.create_issue", Summary: "Open a bug tracker ticket"},
	})

	// The unique keyword "tracker" should now be searchable.
	results, total := idx.Search("tracker", "", 0, 10)
	if total != 1 {
		t.Fatalf("expected 1 match for 'tracker' in metadata summary, got %d", total)
	}
	if results[0].Name != "github.create_issue" {
		t.Fatalf("expected github.create_issue, got %q", results[0].Name)
	}
}

// bm25Corpus is the shared corpus used by BM25 integration tests.
var bm25Corpus = []ToolDefinition{
	{Name: "k8s.list_pods", Description: "List all pods in a Kubernetes namespace.", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &mcp.ToolAnnotation{Title: "List Pods"}},
	{Name: "k8s.get_deployment", Description: "Retrieve a Kubernetes deployment by name.", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &mcp.ToolAnnotation{Title: "Get Deployment"}},
	{Name: "k8s.delete_pod", Description: "Delete a pod from the cluster.", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &mcp.ToolAnnotation{Title: "Delete Pod"}},
	{Name: "docker.list_containers", Description: "List running Docker containers.", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &mcp.ToolAnnotation{Title: "List Containers"}},
	{Name: "slack.send_message", Description: "Send a message to a Slack channel.", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &mcp.ToolAnnotation{Title: "Send Message"}},
	{Name: "github.create_issue", Description: "Create a new issue in a GitHub repository.", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &mcp.ToolAnnotation{Title: "Create Issue"}},
	{Name: "pg.run_query", Description: "Execute a SQL query against a PostgreSQL database.", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &mcp.ToolAnnotation{Title: "Run Query"}},
}

func newBM25Index() *DiscoveryIndex {
	idx := NewDiscoveryIndex()
	idx.SetEngine(search.NewBM25Engine())
	idx.Index(bm25Corpus)
	return idx
}

func TestDiscoveryIndex_SearchWithBM25(t *testing.T) {
	t.Parallel()
	idx := newBM25Index()

	tests := []struct {
		name          string
		query         string
		wantFirstName string
		wantMinCount  int
	}{
		{
			name:          "kubernetes pods via synonym",
			query:         "kubernetes pods",
			wantFirstName: "k8s.list_pods",
			wantMinCount:  1,
		},
		{
			name:          "k8s pods direct",
			query:         "k8s pods",
			wantFirstName: "k8s.list_pods",
			wantMinCount:  1,
		},
		{
			name:          "exact tool name",
			query:         "k8s.list_pods",
			wantFirstName: "k8s.list_pods",
			wantMinCount:  1,
		},
		{
			name:          "kubernetes deployment",
			query:         "kubernetes deployment",
			wantFirstName: "k8s.get_deployment",
			wantMinCount:  1,
		},
		{
			name:          "slack message",
			query:         "Slack",
			wantFirstName: "slack.send_message",
			wantMinCount:  1,
		},
		{
			name:         "empty query",
			query:        "",
			wantMinCount: 0,
		},
		{
			name:         "no match",
			query:        "nonexistent",
			wantMinCount: 0,
		},
		{
			name:          "postgres synonym",
			query:         "postgres query",
			wantFirstName: "pg.run_query",
			wantMinCount:  1,
		},
		{
			name:          "remove synonym for delete",
			query:         "remove pod",
			wantFirstName: "k8s.delete_pod",
			wantMinCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			results, total := idx.Search(tt.query, "", 0, 50)
			if total < tt.wantMinCount {
				t.Errorf("total = %d, want at least %d", total, tt.wantMinCount)
			}
			if tt.wantFirstName != "" && len(results) > 0 && results[0].Name != tt.wantFirstName {
				t.Errorf("first result = %q, want %q", results[0].Name, tt.wantFirstName)
			}
			for _, r := range results {
				if !r.DeferLoading {
					t.Errorf("result %q should have DeferLoading=true", r.Name)
				}
				if string(r.InputSchema) != `{}` {
					t.Errorf("result %q should have stripped schema", r.Name)
				}
			}
		})
	}
}

func TestDiscoveryIndex_SearchBM25CategoryFilter(t *testing.T) {
	t.Parallel()
	idx := newBM25Index()

	// "list" matches k8s.list_pods and docker.list_containers.
	// With category "k8s", only k8s results should appear.
	results, total := idx.Search("list", "k8s", 0, 50)
	if total == 0 {
		t.Fatal("expected results for 'list' in k8s category")
	}
	for _, r := range results {
		cat := categoryFromFederatedName(r.Name)
		if cat != "k8s" {
			t.Errorf("result %q has category %q, expected k8s", r.Name, cat)
		}
	}
}

func TestDiscoveryIndex_SearchBM25Pagination(t *testing.T) {
	t.Parallel()
	idx := newBM25Index()

	// Search a broad term to get multiple results.
	_, total := idx.Search("kubernetes", "", 0, 50)
	if total < 2 {
		t.Fatalf("need at least 2 results for pagination test, got %d", total)
	}

	// Page 1: limit=1.
	page1, totalP1 := idx.Search("kubernetes", "", 0, 1)
	if len(page1) != 1 {
		t.Fatalf("page 1 expected 1 result, got %d", len(page1))
	}
	if totalP1 != total {
		t.Fatalf("total should be consistent: page1=%d, full=%d", totalP1, total)
	}

	// Page 2: offset=1, limit=1.
	page2, _ := idx.Search("kubernetes", "", 1, 1)
	if len(page2) != 1 {
		t.Fatalf("page 2 expected 1 result, got %d", len(page2))
	}
	if page1[0].Name == page2[0].Name {
		t.Error("page 1 and page 2 returned the same result")
	}
}

func TestDiscoveryIndex_EngineReindexOnMetadata(t *testing.T) {
	t.Parallel()
	idx := newBM25Index()

	// "tracker" shouldn't match anything initially.
	results, _ := idx.Search("tracker", "", 0, 10)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for 'tracker' before metadata, got %d", len(results))
	}

	// Apply metadata with a summary containing "tracker".
	idx.ApplyMetadata([]ToolMetadata{
		{ToolName: "github.create_issue", Summary: "Open a bug tracker ticket"},
	})

	// Now "tracker" should find the tool via re-indexed engine.
	results, total := idx.Search("tracker", "", 0, 10)
	if total == 0 {
		t.Fatal("expected results for 'tracker' after metadata summary update")
	}
	if results[0].Name != "github.create_issue" {
		t.Errorf("expected github.create_issue, got %q", results[0].Name)
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
