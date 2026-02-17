package identity

import (
	"context"
	"testing"
)

func TestWithIdentityRoundTrip(t *testing.T) {
	t.Parallel()

	identity := &Identity{
		Type:   IdentityAPIKey,
		ID:     "client-1",
		Scopes: []string{ScopeRead, ScopeWrite},
		Metadata: map[string]string{
			"source": "test",
		},
	}

	ctx := WithIdentity(context.Background(), identity)
	got, ok := FromContext(ctx)
	if !ok {
		t.Fatal("expected identity in context")
	}
	if got.ID != identity.ID || got.Type != identity.Type {
		t.Fatalf("unexpected identity from context: %+v", got)
	}
}

func TestFromContextEmpty(t *testing.T) {
	t.Parallel()

	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("expected no identity in empty context")
	}

	ctx := context.WithValue(context.Background(), identityContextKey{}, "bad-type")
	if _, ok := FromContext(ctx); ok {
		t.Fatal("expected false for invalid context value type")
	}
}

func TestIdentityHasScope(t *testing.T) {
	t.Parallel()

	identity := &Identity{Scopes: []string{ScopeRead, ScopeAdmin}}

	if !identity.HasScope(ScopeRead) {
		t.Fatal("expected scope read to exist")
	}
	if !identity.HasScope(ScopeAdmin) {
		t.Fatal("expected scope admin to exist")
	}
	if identity.HasScope(ScopeWrite) {
		t.Fatal("expected scope write to be missing")
	}

	var nilIdentity *Identity
	if nilIdentity.HasScope(ScopeRead) {
		t.Fatal("expected nil identity to have no scopes")
	}
}
