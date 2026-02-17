package ratelimit

import (
	"testing"

	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestDefaultProfileResolverResolve(t *testing.T) {
	t.Parallel()

	defaultProfile := &types.PolicyProfile{Name: "default", RateLimit: 10, RateBurst: 20}
	premiumProfile := &types.PolicyProfile{Name: "premium", RateLimit: 50, RateBurst: 100}

	resolver := NewDefaultProfileResolver(map[string]*types.PolicyProfile{
		"default": defaultProfile,
		"premium": premiumProfile,
	}, defaultProfile)

	t.Run("resolves policy_profile metadata", func(t *testing.T) {
		id := &identity.Identity{
			ID: "client-1",
			Metadata: map[string]string{
				"policy_profile": "premium",
			},
		}
		got := resolver.Resolve(id)
		if got != premiumProfile {
			t.Fatalf("expected premium profile, got %#v", got)
		}
	})

	t.Run("falls back when profile missing", func(t *testing.T) {
		id := &identity.Identity{
			ID: "client-1",
			Metadata: map[string]string{
				"policy_profile": "unknown",
			},
		}
		got := resolver.Resolve(id)
		if got != defaultProfile {
			t.Fatalf("expected default profile, got %#v", got)
		}
	})

	t.Run("falls back for nil identity", func(t *testing.T) {
		got := resolver.Resolve(nil)
		if got != defaultProfile {
			t.Fatalf("expected default profile, got %#v", got)
		}
	})
}
