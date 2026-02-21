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
			results, total := idx.Search(tt.query, tt.category, tt.limit)

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

	results, total := idx.Search("issue", "", 10)
	if total != 1 || len(results) != 1 {
		t.Fatalf("v1: expected 1 result, got total=%d results=%d", total, len(results))
	}

	toolsV2 := []ToolDefinition{
		{Name: "mcp.slack.send_message", Description: "Send message", InputSchema: json.RawMessage(`{}`)},
	}
	idx.Index(toolsV2)

	results, total = idx.Search("issue", "", 10)
	if total != 0 || len(results) != 0 {
		t.Fatalf("v2: expected 0 results after rebuild, got total=%d results=%d", total, len(results))
	}

	results, total = idx.Search("message", "", 10)
	if total != 1 || len(results) != 1 {
		t.Fatalf("v2: expected 1 result for message, got total=%d results=%d", total, len(results))
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
