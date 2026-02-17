package testutil

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
	"golang.org/x/crypto/bcrypt"
)

// TestServerRecord returns a valid active server fixture.
func TestServerRecord() types.ServerRecord {
	now := time.Now().UTC()
	return types.ServerRecord{
		ID:            "11111111-1111-4111-8111-111111111111",
		Name:          "fixture-server",
		SPIFFEID:      "spiffe://example.org/ns/default/sa/fixture",
		Version:       "1.0.0",
		Host:          "fixture.default.svc.cluster.local",
		Port:          8443,
		Capabilities:  types.Capability{Tools: []string{"tool.echo"}, Resources: []string{"resource://fixture"}, Prompts: []string{"prompt.fixture"}},
		Status:        types.StatusActive,
		PolicyProfile: "default",
		LastHeartbeat: &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// TestRegistration returns a valid registration payload fixture.
func TestRegistration() types.Registration {
	return types.Registration{
		ServiceName:   "fixture-server",
		Version:       "1.0.0",
		ListenAddress: "https://fixture.default.svc.cluster.local:8443",
		Capabilities: types.Capability{
			Tools:     []string{"tool.echo"},
			Resources: []string{"resource://fixture"},
			Prompts:   []string{"prompt.fixture"},
		},
		Labels: map[string]string{"team": "platform"},
	}
}

// TestAPIKey returns a persisted API key fixture and its plaintext value.
func TestAPIKey() (types.APIKey, string) {
	plaintext, lookupHash, bcryptHash, err := generateFixtureAPIKey()
	if err != nil {
		panic(fmt.Errorf("test fixture api key generation failed: %w", err))
	}

	now := time.Now().UTC()
	expires := now.Add(24 * time.Hour)
	return types.APIKey{
		ID:            "22222222-2222-4222-8222-222222222222",
		KeyLookupHash: lookupHash,
		KeyBcryptHash: bcryptHash,
		Name:          "fixture-apikey",
		Scopes:        []string{"read", "write"},
		ClientID:      "fixture-client",
		ExpiresAt:     &expires,
		CreatedAt:     now,
	}, plaintext
}

func generateFixtureAPIKey() (string, string, string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", "", fmt.Errorf("read random bytes: %w", err)
	}

	plaintext := "mcpgw_" + base64.RawURLEncoding.EncodeToString(bytes)
	sum := sha256.Sum256([]byte(plaintext))
	lookupHash := hex.EncodeToString(sum[:])

	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), 12)
	if err != nil {
		return "", "", "", fmt.Errorf("bcrypt hash: %w", err)
	}

	return plaintext, lookupHash, string(hash), nil
}

// TestPolicyProfiles returns sample policy fixtures keyed by profile name.
func TestPolicyProfiles() map[string]*types.PolicyProfile {
	return map[string]*types.PolicyProfile{
		"default": {
			Name:            "default",
			RateLimit:       10,
			RateBurst:       20,
			ToolAllowlist:   []string{},
			ToolDenylist:    []string{},
			EnforcementMode: types.ModeEnforce,
		},
		"premium": {
			Name:            "premium",
			RateLimit:       100,
			RateBurst:       200,
			ToolAllowlist:   []string{},
			ToolDenylist:    []string{},
			EnforcementMode: types.ModeEnforce,
		},
		"admin": {
			Name:            "admin",
			RateLimit:       1_000_000,
			RateBurst:       1_000_000,
			ToolAllowlist:   []string{},
			ToolDenylist:    []string{},
			EnforcementMode: types.ModeEnforce,
		},
	}
}
