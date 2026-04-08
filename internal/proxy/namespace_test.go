package proxy

import (
	"testing"

	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func buildTestIndex(toolServers map[string][]string) *registration.CapabilityIndex {
	index := registration.NewCapabilityIndex()
	for tool, servers := range toolServers {
		for _, srv := range servers {
			index.Add(types.ServerRecord{
				ID:           srv,
				Name:         srv,
				Status:       types.StatusActive,
				Capabilities: types.Capability{Tools: []string{tool}},
			})
		}
	}
	return index
}

func TestNewNamespaceResolverDefaultMode(t *testing.T) {
	t.Parallel()
	r := NewNamespaceResolver("invalid_mode", ".", nil)
	if r.Mode() != NamespaceModeReject {
		t.Fatalf("expected reject mode for invalid input, got %s", r.Mode())
	}
}

func TestNewNamespaceResolverNilResolver(t *testing.T) {
	t.Parallel()
	var r *NamespaceResolver
	if r.Mode() != NamespaceModeReject {
		t.Fatalf("expected reject mode for nil resolver, got %s", r.Mode())
	}
	if got := r.ApplyNamespace("tool", "server"); got != "tool" {
		t.Fatalf("expected 'tool', got %q", got)
	}
	hint, name := r.ResolveNamespace("server.tool")
	if hint != "" || name != "server.tool" {
		t.Fatalf("expected empty hint and original name for nil resolver, got hint=%q name=%q", hint, name)
	}
	if r.IsConflict("tool") {
		t.Fatal("expected no conflict for nil resolver")
	}
}

func TestApplyNamespace(t *testing.T) {
	t.Parallel()

	index := buildTestIndex(map[string][]string{
		"list_pods":    {"k8s", "other-k8s"},
		"search_repos": {"github"},
	})

	tests := []struct {
		name       string
		mode       string
		separator  string
		toolName   string
		serverName string
		want       string
	}{
		{
			name:       "reject mode returns unchanged",
			mode:       "reject",
			separator:  ".",
			toolName:   "list_pods",
			serverName: "k8s",
			want:       "list_pods",
		},
		{
			name:       "always mode prefixes",
			mode:       "namespace_always",
			separator:  ".",
			toolName:   "list_pods",
			serverName: "k8s",
			want:       "k8s.list_pods",
		},
		{
			name:       "always mode strips mcp- prefix",
			mode:       "namespace_always",
			separator:  ".",
			toolName:   "search_repos",
			serverName: "mcp-github",
			want:       "github.search_repos",
		},
		{
			name:       "on_conflict mode prefixes when conflict exists",
			mode:       "namespace_on_conflict",
			separator:  ".",
			toolName:   "list_pods",
			serverName: "k8s",
			want:       "k8s.list_pods",
		},
		{
			name:       "on_conflict mode no prefix when no conflict",
			mode:       "namespace_on_conflict",
			separator:  ".",
			toolName:   "search_repos",
			serverName: "github",
			want:       "search_repos",
		},
		{
			name:       "custom separator",
			mode:       "namespace_always",
			separator:  "::",
			toolName:   "list_pods",
			serverName: "k8s",
			want:       "k8s::list_pods",
		},
		{
			name:       "empty tool name returns empty",
			mode:       "namespace_always",
			separator:  ".",
			toolName:   "",
			serverName: "k8s",
			want:       "",
		},
		{
			name:       "empty server name returns tool unchanged",
			mode:       "namespace_always",
			separator:  ".",
			toolName:   "list_pods",
			serverName: "",
			want:       "list_pods",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := NewNamespaceResolver(tt.mode, tt.separator, index)
			got := r.ApplyNamespace(tt.toolName, tt.serverName)
			if got != tt.want {
				t.Fatalf("ApplyNamespace(%q, %q) = %q, want %q", tt.toolName, tt.serverName, got, tt.want)
			}
		})
	}
}

func TestResolveNamespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		separator      string
		input          string
		wantServerHint string
		wantToolName   string
	}{
		{
			name:           "dot separator splits correctly",
			separator:      ".",
			input:          "k8s.list_pods",
			wantServerHint: "k8s",
			wantToolName:   "list_pods",
		},
		{
			name:           "custom separator",
			separator:      "::",
			input:          "k8s::list_pods",
			wantServerHint: "k8s",
			wantToolName:   "list_pods",
		},
		{
			name:           "no separator returns empty hint",
			separator:      ".",
			input:          "list_pods",
			wantServerHint: "",
			wantToolName:   "list_pods",
		},
		{
			name:           "separator at start returns empty hint",
			separator:      ".",
			input:          ".list_pods",
			wantServerHint: "",
			wantToolName:   ".list_pods",
		},
		{
			name:           "multiple separators splits on first",
			separator:      ".",
			input:          "k8s.ns.list_pods",
			wantServerHint: "k8s",
			wantToolName:   "ns.list_pods",
		},
		{
			name:           "empty input",
			separator:      ".",
			input:          "",
			wantServerHint: "",
			wantToolName:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := NewNamespaceResolver("reject", tt.separator, nil)
			hint, name := r.ResolveNamespace(tt.input)
			if hint != tt.wantServerHint {
				t.Fatalf("ResolveNamespace(%q): serverHint = %q, want %q", tt.input, hint, tt.wantServerHint)
			}
			if name != tt.wantToolName {
				t.Fatalf("ResolveNamespace(%q): toolName = %q, want %q", tt.input, name, tt.wantToolName)
			}
		})
	}
}

func TestIsConflict(t *testing.T) {
	t.Parallel()

	index := buildTestIndex(map[string][]string{
		"list_pods":    {"k8s-a", "k8s-b"},
		"search_repos": {"github"},
	})

	tests := []struct {
		name     string
		toolName string
		want     bool
	}{
		{
			name:     "conflicting tool",
			toolName: "list_pods",
			want:     true,
		},
		{
			name:     "unique tool",
			toolName: "search_repos",
			want:     false,
		},
		{
			name:     "unknown tool",
			toolName: "nonexistent",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := NewNamespaceResolver("reject", ".", index)
			got := r.IsConflict(tt.toolName)
			if got != tt.want {
				t.Fatalf("IsConflict(%q) = %v, want %v", tt.toolName, got, tt.want)
			}
		})
	}
}

func TestNamespaceResolverDefaultSeparator(t *testing.T) {
	t.Parallel()
	r := NewNamespaceResolver("namespace_always", "", nil)
	got := r.ApplyNamespace("tool", "server")
	if got != "server.tool" {
		t.Fatalf("expected 'server.tool' with default separator, got %q", got)
	}
}
