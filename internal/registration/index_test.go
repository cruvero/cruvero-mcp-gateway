package registration

import (
	"fmt"
	"sync"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestCapabilityIndexAddLookupTool(t *testing.T) {
	t.Parallel()

	idx := NewCapabilityIndex()
	server := indexTestServer("s1", types.StatusActive, []string{"tool.alpha"}, []string{"urn:docs/"})
	idx.Add(server)

	servers := idx.LookupTool("tool.alpha")
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	if servers[0].ID != "s1" {
		t.Fatalf("expected server id s1, got %q", servers[0].ID)
	}
}

func TestCapabilityIndexAddMultipleServersSameTool(t *testing.T) {
	t.Parallel()

	idx := NewCapabilityIndex()
	idx.Add(indexTestServer("s1", types.StatusActive, []string{"tool.alpha"}, nil))
	idx.Add(indexTestServer("s2", types.StatusActive, []string{"tool.alpha"}, nil))

	servers := idx.LookupTool("tool.alpha")
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}
}

func TestCapabilityIndexRemove(t *testing.T) {
	t.Parallel()

	idx := NewCapabilityIndex()
	idx.Add(indexTestServer("s1", types.StatusActive, []string{"tool.alpha"}, []string{"urn:docs/"}))
	idx.Add(indexTestServer("s2", types.StatusActive, []string{"tool.alpha"}, []string{"urn:docs/"}))

	idx.Remove("s1")

	toolServers := idx.LookupTool("tool.alpha")
	if len(toolServers) != 1 || toolServers[0].ID != "s2" {
		t.Fatalf("expected only s2 after remove, got %+v", toolServers)
	}

	resourceServers := idx.LookupResource("urn:docs/item")
	if len(resourceServers) != 1 || resourceServers[0].ID != "s2" {
		t.Fatalf("expected only s2 resource after remove, got %+v", resourceServers)
	}
}

func TestCapabilityIndexRebuild(t *testing.T) {
	t.Parallel()

	idx := NewCapabilityIndex()
	servers := []types.ServerRecord{
		indexTestServer("active-1", types.StatusActive, []string{"tool.alpha"}, []string{"urn:docs/"}),
		indexTestServer("stale-1", types.StatusStale, []string{"tool.beta"}, []string{"urn:beta/"}),
		indexTestServer("active-2", types.StatusActive, []string{"tool.gamma"}, []string{"urn:gamma/"}),
	}

	idx.Rebuild(servers)

	if got := len(idx.LookupTool("tool.alpha")); got != 1 {
		t.Fatalf("expected tool.alpha to have 1 server, got %d", got)
	}
	if got := len(idx.LookupTool("tool.beta")); got != 0 {
		t.Fatalf("expected tool.beta to have 0 active servers, got %d", got)
	}
	if got := len(idx.LookupTool("tool.gamma")); got != 1 {
		t.Fatalf("expected tool.gamma to have 1 server, got %d", got)
	}
}

func TestCapabilityIndexLookupUnknownTool(t *testing.T) {
	t.Parallel()

	idx := NewCapabilityIndex()
	idx.Add(indexTestServer("s1", types.StatusActive, []string{"tool.alpha"}, nil))

	if servers := idx.LookupTool("tool.unknown"); len(servers) != 0 {
		t.Fatalf("expected no servers for unknown tool, got %d", len(servers))
	}
}

func TestCapabilityIndexListToolsSorted(t *testing.T) {
	t.Parallel()

	idx := NewCapabilityIndex()
	idx.Add(indexTestServer("s1", types.StatusActive, []string{"tool.zeta", "tool.alpha"}, nil))
	idx.Add(indexTestServer("s2", types.StatusActive, []string{"tool.alpha", "tool.beta"}, nil))

	tools := idx.ListTools()
	expected := []string{"tool.alpha", "tool.beta", "tool.zeta"}
	if len(tools) != len(expected) {
		t.Fatalf("expected %d tools, got %d", len(expected), len(tools))
	}
	for i := range expected {
		if tools[i] != expected[i] {
			t.Fatalf("expected tool %q at index %d, got %q", expected[i], i, tools[i])
		}
	}
}

func TestCapabilityIndexLookupResourceLongestPrefix(t *testing.T) {
	t.Parallel()

	idx := NewCapabilityIndex()
	idx.Add(indexTestServer("s1", types.StatusActive, nil, []string{"urn:docs/"}))
	idx.Add(indexTestServer("s2", types.StatusActive, nil, []string{"urn:docs/private/"}))

	servers := idx.LookupResource("urn:docs/private/file-1")
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	if servers[0].ID != "s2" {
		t.Fatalf("expected longest prefix match to return s2, got %q", servers[0].ID)
	}
}

func TestCapabilityIndexListResourcesSorted(t *testing.T) {
	t.Parallel()

	idx := NewCapabilityIndex()
	idx.Add(indexTestServer("s1", types.StatusActive, nil, []string{"urn:z/", "urn:a/"}))
	idx.Add(indexTestServer("s2", types.StatusActive, nil, []string{"urn:a/", "urn:b/"}))

	resources := idx.ListResources()
	expected := []string{"urn:a/", "urn:b/", "urn:z/"}
	if len(resources) != len(expected) {
		t.Fatalf("expected %d resources, got %d", len(expected), len(resources))
	}
	for i := range expected {
		if resources[i] != expected[i] {
			t.Fatalf("expected resource %q at index %d, got %q", expected[i], i, resources[i])
		}
	}
}

func TestCapabilityIndexLookupReturnsCopies(t *testing.T) {
	t.Parallel()

	idx := NewCapabilityIndex()
	idx.Add(indexTestServer("s1", types.StatusActive, []string{"tool.alpha"}, nil))

	servers := idx.LookupTool("tool.alpha")
	if len(servers) != 1 {
		t.Fatalf("expected one result, got %d", len(servers))
	}
	servers[0].ID = "mutated"
	servers[0].Capabilities.Tools[0] = "tool.changed"

	again := idx.LookupTool("tool.alpha")
	if len(again) != 1 {
		t.Fatalf("expected one result on second lookup, got %d", len(again))
	}
	if again[0].ID != "s1" {
		t.Fatalf("expected stored id to remain s1, got %q", again[0].ID)
	}
	if again[0].Capabilities.Tools[0] != "tool.alpha" {
		t.Fatalf("expected stored capability to remain tool.alpha, got %q", again[0].Capabilities.Tools[0])
	}
}

func TestCapabilityIndexConcurrentAddLookup(t *testing.T) {
	t.Parallel()

	idx := NewCapabilityIndex()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			idx.Add(indexTestServer(fmt.Sprintf("s%d", i), types.StatusActive, []string{"tool.shared"}, nil))
		}()
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = idx.LookupTool("tool.shared")
		}()
	}

	wg.Wait()
	if got := len(idx.LookupTool("tool.shared")); got == 0 {
		t.Fatal("expected at least one server for tool.shared")
	}
}

func TestToolCountReturnsDistinctToolCount(t *testing.T) {
	t.Parallel()

	idx := NewCapabilityIndex()
	if got := idx.ToolCount(); got != 0 {
		t.Fatalf("expected 0 tools on empty index, got %d", got)
	}

	idx.Add(indexTestServer("s1", types.StatusActive, []string{"tool.alpha", "tool.beta"}, nil))
	if got := idx.ToolCount(); got != 2 {
		t.Fatalf("expected 2 tools after add, got %d", got)
	}

	idx.Add(indexTestServer("s2", types.StatusActive, []string{"tool.alpha", "tool.gamma"}, nil))
	if got := idx.ToolCount(); got != 3 {
		t.Fatalf("expected 3 tools after second add (shared tool.alpha), got %d", got)
	}

	idx.Remove("s2")
	if got := idx.ToolCount(); got != 2 {
		t.Fatalf("expected 2 tools after removing s2 (tool.gamma gone, tool.alpha stays), got %d", got)
	}
}

func TestToolCountAfterRebuild(t *testing.T) {
	t.Parallel()

	idx := NewCapabilityIndex()
	idx.Add(indexTestServer("s1", types.StatusActive, []string{"old.tool"}, nil))

	servers := []types.ServerRecord{
		indexTestServer("s2", types.StatusActive, []string{"new.a", "new.b", "new.c"}, nil),
	}
	idx.Rebuild(servers)
	if got := idx.ToolCount(); got != 3 {
		t.Fatalf("expected 3 tools after rebuild, got %d", got)
	}
}

func TestToolCountNilIndex(t *testing.T) {
	t.Parallel()

	var idx *CapabilityIndex
	if got := idx.ToolCount(); got != 0 {
		t.Fatalf("expected 0 on nil index, got %d", got)
	}
}

func indexTestServer(id string, status types.ServerStatus, tools []string, resources []string) types.ServerRecord {
	return types.ServerRecord{
		ID:     id,
		Name:   "svc-" + id,
		Status: status,
		Capabilities: types.Capability{
			Tools:     tools,
			Resources: resources,
		},
	}
}
