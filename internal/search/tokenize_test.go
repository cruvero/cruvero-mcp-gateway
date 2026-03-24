package search

import (
	"testing"
)

func TestTokenize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "tool name with dots and underscores",
			input: "k8s.list_pods",
			want:  []string{"k8s", "list", "pods"},
		},
		{
			name:  "natural language query",
			input: "kubernetes pods list",
			want:  []string{"kubernetes", "pods", "list"},
		},
		{
			name:  "mixed delimiters",
			input: "mcp.server-name_tool",
			want:  []string{"mcp", "server", "name", "tool"},
		},
		{
			name:  "uppercase preserved as lower",
			input: "GitHub.Create_Issue",
			want:  []string{"github", "create", "issue"},
		},
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "only delimiters",
			input: "._-  ._",
			want:  nil,
		},
		{
			name:  "single token",
			input: "kubernetes",
			want:  []string{"kubernetes"},
		},
		{
			name:  "hyphens in name",
			input: "my-mcp-server",
			want:  []string{"my", "mcp", "server"},
		},
		{
			name:  "tabs and multiple spaces",
			input: "  create\t issue  ",
			want:  []string{"create", "issue"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Tokenize(tt.input)
			if !slicesEqual(got, tt.want) {
				t.Errorf("Tokenize(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
