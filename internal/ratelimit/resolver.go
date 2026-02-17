package ratelimit

import (
	"strings"

	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/types"
)

// ProfileResolver resolves a request identity to a policy profile.
type ProfileResolver interface {
	Resolve(identity *identity.Identity) *types.PolicyProfile
}

// DefaultProfileResolver resolves profiles from an in-memory profile map.
type DefaultProfileResolver struct {
	profiles       map[string]*types.PolicyProfile
	defaultProfile *types.PolicyProfile
}

// NewDefaultProfileResolver creates a default profile resolver.
func NewDefaultProfileResolver(
	profiles map[string]*types.PolicyProfile,
	defaultProfile *types.PolicyProfile,
) *DefaultProfileResolver {
	out := make(map[string]*types.PolicyProfile, len(profiles))
	for name, profile := range profiles {
		out[name] = profile
	}

	return &DefaultProfileResolver{
		profiles:       out,
		defaultProfile: defaultProfile,
	}
}

// Resolve resolves identity metadata policy_profile and falls back to default.
func (r *DefaultProfileResolver) Resolve(id *identity.Identity) *types.PolicyProfile {
	if r == nil {
		return nil
	}

	if id != nil {
		profileName := strings.TrimSpace(id.Metadata["policy_profile"])
		if profileName != "" {
			if profile, ok := r.profiles[profileName]; ok && profile != nil {
				return profile
			}
		}
	}

	return r.defaultProfile
}
