package proxy

import (
	"strings"

	"github.com/cruvero/mcp-gateway/internal/registration"
)

// NamespaceMode controls how tool name conflicts are resolved across backends.
type NamespaceMode string

const (
	// NamespaceModeReject rejects ambiguous tool calls when multiple backends
	// expose the same tool name. Tool names are never modified.
	NamespaceModeReject NamespaceMode = "reject"
	// NamespaceModeAlways always prefixes tool names with the server name,
	// regardless of whether a conflict exists.
	NamespaceModeAlways NamespaceMode = "namespace_always"
	// NamespaceModeOnConflict only prefixes tool names when multiple backends
	// expose the same bare tool name.
	NamespaceModeOnConflict NamespaceMode = "namespace_on_conflict"
)

// NamespaceResolver applies and resolves server-namespaced tool names.
type NamespaceResolver struct {
	mode      NamespaceMode
	separator string
	index     *registration.CapabilityIndex
}

// NewNamespaceResolver creates a resolver with the given mode, separator, and
// capability index. Invalid mode values default to NamespaceModeReject.
func NewNamespaceResolver(mode, separator string, index *registration.CapabilityIndex) *NamespaceResolver {
	m := NamespaceMode(strings.TrimSpace(mode))
	switch m {
	case NamespaceModeAlways, NamespaceModeOnConflict, NamespaceModeReject:
	default:
		m = NamespaceModeReject
	}
	sep := separator
	if sep == "" {
		sep = "."
	}
	return &NamespaceResolver{
		mode:      m,
		separator: sep,
		index:     index,
	}
}

// Mode returns the configured namespace mode.
func (r *NamespaceResolver) Mode() NamespaceMode {
	if r == nil {
		return NamespaceModeReject
	}
	return r.mode
}

// ApplyNamespace returns the tool name with an optional server prefix based on
// the configured mode.
//
//   - reject: returns toolName unchanged
//   - namespace_always: always prefixes as serverName<sep>toolName
//   - namespace_on_conflict: prefixes only when IsConflict(toolName) is true
func (r *NamespaceResolver) ApplyNamespace(toolName, serverName string) string {
	if r == nil {
		return toolName
	}
	tool := strings.TrimSpace(toolName)
	server := strings.TrimSpace(serverName)
	if tool == "" || server == "" {
		return tool
	}

	displayName := strings.TrimPrefix(server, "mcp-")

	switch r.mode {
	case NamespaceModeAlways:
		return displayName + r.separator + tool
	case NamespaceModeOnConflict:
		if r.IsConflict(tool) {
			return displayName + r.separator + tool
		}
		return tool
	default:
		return tool
	}
}

// ResolveNamespace splits a potentially namespaced tool name into a server hint
// and bare tool name. If the name does not contain the separator, serverHint is
// empty and toolName is returned as-is.
func (r *NamespaceResolver) ResolveNamespace(namespacedName string) (serverHint, toolName string) {
	if r == nil {
		return "", namespacedName
	}
	name := strings.TrimSpace(namespacedName)
	idx := strings.Index(name, r.separator)
	if idx <= 0 {
		return "", name
	}
	return name[:idx], name[idx+len(r.separator):]
}

// IsConflict returns true when the given bare tool name is exposed by more than
// one backend server in the capability index.
func (r *NamespaceResolver) IsConflict(toolName string) bool {
	if r == nil || r.index == nil {
		return false
	}
	candidates := r.index.LookupTool(strings.TrimSpace(toolName))
	return len(candidates) > 1
}
