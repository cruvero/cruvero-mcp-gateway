package proxy

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/registration"
)

func TestListCatalogToolsExcludesMetaTools(t *testing.T) {
	t.Parallel()

	var listCalls atomic.Int64
	record, client, cleanup := buildToolBackendClient(t, "server-1", "tool.echo", "echo tool", &listCalls)
	defer cleanup()

	index := registration.NewCapabilityIndex()
	index.Add(record)

	proxyServer := NewProxyServer(index, &config.Config{}, nil, 0, nil)
	proxyServer.clients[record.ID] = client

	entries, err := proxyServer.ListCatalogTools(context.Background())
	if err != nil {
		t.Fatalf("list catalog tools: %v", err)
	}

	for _, e := range entries {
		if _, isMeta := metaToolNames[e.Name]; isMeta {
			t.Fatalf("meta-tool %q should not appear in catalog", e.Name)
		}
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 catalog entry, got %d", len(entries))
	}
	if entries[0].Name != "mcp.server-1.tool.echo" {
		t.Fatalf("expected mcp.server-1.tool.echo, got %q", entries[0].Name)
	}
}

func TestListCatalogToolsIncludesServerMetadata(t *testing.T) {
	t.Parallel()

	var listCalls atomic.Int64
	record, client, cleanup := buildToolBackendClient(t, "mcp-k8s", "list_pods", "list pods", &listCalls)
	defer cleanup()

	index := registration.NewCapabilityIndex()
	index.Add(record)

	proxyServer := NewProxyServer(index, &config.Config{}, nil, 0, nil)
	proxyServer.clients[record.ID] = client

	entries, err := proxyServer.ListCatalogTools(context.Background())
	if err != nil {
		t.Fatalf("list catalog tools: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.ServerID != record.ID {
		t.Fatalf("expected server ID %q, got %q", record.ID, entry.ServerID)
	}
	wantServerName := "k8s" // catalog strips "mcp-" prefix
	if entry.ServerName != wantServerName {
		t.Fatalf("expected server name %q, got %q", wantServerName, entry.ServerName)
	}
}

func TestListCatalogToolsEmptyIndex(t *testing.T) {
	t.Parallel()

	index := registration.NewCapabilityIndex()
	proxyServer := NewProxyServer(index, &config.Config{}, nil, 0, nil)

	entries, err := proxyServer.ListCatalogTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries for empty index, got %d", len(entries))
	}
}

func TestListCatalogToolsNilProxyServer(t *testing.T) {
	t.Parallel()

	var ps *ProxyServer
	_, err := ps.ListCatalogTools(context.Background())
	if err == nil {
		t.Fatal("expected error for nil proxy server")
	}
}

func TestToolCacheGetWithMeta(t *testing.T) {
	t.Parallel()

	t.Run("hit returns definition and metadata", func(t *testing.T) {
		t.Parallel()
		cache := NewToolCache(time.Minute)
		cache.Set("k8s.list_pods", ToolDefinition{Name: "k8s.list_pods"}, "server-1", "mcp-k8s")

		def, serverID, serverName, ok := cache.GetWithMeta("k8s.list_pods")
		if !ok {
			t.Fatal("expected cache hit")
		}
		if def.Name != "k8s.list_pods" {
			t.Fatalf("expected name k8s.list_pods, got %q", def.Name)
		}
		if serverID != "server-1" {
			t.Fatalf("expected server ID server-1, got %q", serverID)
		}
		if serverName != "mcp-k8s" {
			t.Fatalf("expected server name mcp-k8s, got %q", serverName)
		}
	})

	t.Run("miss returns false", func(t *testing.T) {
		t.Parallel()
		cache := NewToolCache(time.Minute)

		_, _, _, ok := cache.GetWithMeta("nonexistent")
		if ok {
			t.Fatal("expected cache miss")
		}
	})

	t.Run("expired returns false", func(t *testing.T) {
		t.Parallel()
		cache := NewToolCache(10 * time.Millisecond)
		cache.Set("tool.a", ToolDefinition{Name: "tool.a"}, "srv", "srv-name")

		time.Sleep(30 * time.Millisecond)

		_, _, _, ok := cache.GetWithMeta("tool.a")
		if ok {
			t.Fatal("expected expired entry to return false")
		}
	})

	t.Run("nil cache returns false", func(t *testing.T) {
		t.Parallel()
		var cache *ToolCache

		_, _, _, ok := cache.GetWithMeta("anything")
		if ok {
			t.Fatal("expected nil cache to return false")
		}
	})

	t.Run("empty name returns false", func(t *testing.T) {
		t.Parallel()
		cache := NewToolCache(time.Minute)

		_, _, _, ok := cache.GetWithMeta("")
		if ok {
			t.Fatal("expected empty name to return false")
		}
	})
}

func TestToolCacheSetWithServerName(t *testing.T) {
	t.Parallel()

	cache := NewToolCache(time.Minute)
	cache.Set("k8s.list_pods", ToolDefinition{Name: "k8s.list_pods"}, "server-1", "mcp-k8s")

	// Verify through GetWithMeta
	_, serverID, serverName, ok := cache.GetWithMeta("k8s.list_pods")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if serverID != "server-1" {
		t.Fatalf("expected server ID server-1, got %q", serverID)
	}
	if serverName != "mcp-k8s" {
		t.Fatalf("expected server name mcp-k8s, got %q", serverName)
	}

	// Verify through legacy Get (still works)
	def, ok := cache.Get("k8s.list_pods")
	if !ok {
		t.Fatal("expected cache hit via Get")
	}
	if def.Name != "k8s.list_pods" {
		t.Fatalf("expected name k8s.list_pods from Get, got %q", def.Name)
	}
}

