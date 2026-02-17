package identity

import "context"

// IdentityType describes the authentication mechanism used for a request identity.
type IdentityType string

const (
	// IdentityMTLS identifies requests authenticated with mTLS certificate identity.
	IdentityMTLS IdentityType = "mtls"
	// IdentityAPIKey identifies requests authenticated with API key credentials.
	IdentityAPIKey IdentityType = "apikey"
	// IdentityOIDC identifies requests authenticated with OIDC bearer tokens.
	IdentityOIDC IdentityType = "oidc"
)

const (
	// ScopeRead allows read-only operations.
	ScopeRead = "read"
	// ScopeWrite allows write operations.
	ScopeWrite = "write"
	// ScopeAdmin allows administrative operations.
	ScopeAdmin = "admin"
)

// Identity represents an authenticated caller.
type Identity struct {
	Type     IdentityType      `json:"type"`
	ID       string            `json:"id"`
	Scopes   []string          `json:"scopes"`
	Metadata map[string]string `json:"metadata"`
}

// HasScope returns true when the identity contains the requested scope.
func (id *Identity) HasScope(scope string) bool {
	if id == nil {
		return false
	}
	for _, s := range id.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

type identityContextKey struct{}

// WithIdentity attaches an authenticated identity to context.
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, id)
}

// FromContext fetches the authenticated identity from context, if present.
func FromContext(ctx context.Context) (*Identity, bool) {
	id, ok := ctx.Value(identityContextKey{}).(*Identity)
	if !ok || id == nil {
		return nil, false
	}
	return id, true
}
