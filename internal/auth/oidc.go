package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/cruvero/mcp-gateway/internal/identity"
)

// OIDCValidator validates bearer tokens against an OIDC provider.
type OIDCValidator struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
}

type oidcClaims struct {
	Subject string   `json:"sub"`
	Email   string   `json:"email"`
	Groups  []string `json:"groups"`
	Scope   string   `json:"scope"`
}

// NewOIDCValidator discovers an OIDC provider and creates a token verifier.
func NewOIDCValidator(ctx context.Context, issuerURL, audience string) (*OIDCValidator, error) {
	if strings.TrimSpace(issuerURL) == "" {
		return nil, fmt.Errorf("new oidc validator: issuer URL is required")
	}
	if strings.TrimSpace(audience) == "" {
		return nil, fmt.Errorf("new oidc validator: audience is required")
	}

	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("new oidc validator: discover provider: %w", err)
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: audience})
	return &OIDCValidator{provider: provider, verifier: verifier}, nil
}

// Validate verifies an OIDC token and maps claims to identity.
func (v *OIDCValidator) Validate(ctx context.Context, rawToken string) (*identity.Identity, error) {
	if v == nil || v.verifier == nil {
		return nil, fmt.Errorf("validate oidc token: validator is not configured")
	}
	if strings.TrimSpace(rawToken) == "" {
		return nil, fmt.Errorf("validate oidc token: token is required")
	}

	idToken, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("validate oidc token: verify token: %w", err)
	}

	var claims oidcClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("validate oidc token: parse claims: %w", err)
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return nil, fmt.Errorf("validate oidc token: subject claim is required")
	}

	scopes := mergeScopes(claims.Groups, claims.Scope)
	return &identity.Identity{
		Type:   identity.IdentityOIDC,
		ID:     claims.Subject,
		Scopes: scopes,
		Metadata: map[string]string{
			"auth_method": "oidc",
			"email":       claims.Email,
			"issuer":      idToken.Issuer,
		},
	}, nil
}

func mergeScopes(groups []string, scopeClaim string) []string {
	seen := make(map[string]struct{})
	scopes := make([]string, 0, len(groups)+1)

	for _, group := range groups {
		trimmed := strings.TrimSpace(group)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		scopes = append(scopes, trimmed)
	}

	for _, scope := range strings.Fields(scopeClaim) {
		trimmed := strings.TrimSpace(scope)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		scopes = append(scopes, trimmed)
	}

	return scopes
}
