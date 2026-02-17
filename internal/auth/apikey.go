package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const (
	// APIKeyPrefix is the fixed prefix for generated gateway API keys.
	APIKeyPrefix = "mcpgw_"
	apiKeyBytes  = 32
	bcryptCost   = 12
)

// GenerateAPIKey returns a new plaintext API key and corresponding lookup/bcrypt hashes.
func GenerateAPIKey() (plaintext string, lookupHash string, bcryptHash string, err error) {
	randomBytes := make([]byte, apiKeyBytes)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", "", fmt.Errorf("generate api key: read random bytes: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(randomBytes)
	plaintext = APIKeyPrefix + encoded
	lookupHash = LookupHashAPIKey(plaintext)

	bcryptHash, err = HashAPIKey(plaintext)
	if err != nil {
		return "", "", "", fmt.Errorf("generate api key: %w", err)
	}

	return plaintext, lookupHash, bcryptHash, nil
}

// LookupHashAPIKey computes the deterministic SHA-256 lookup hash for a plaintext API key.
func LookupHashAPIKey(plaintext string) string {
	hash := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(hash[:])
}

// HashAPIKey computes a bcrypt hash for secure API key verification.
func HashAPIKey(plaintext string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash api key: %w", err)
	}
	return string(hash), nil
}

// VerifyAPIKey verifies a plaintext API key against a bcrypt hash.
func VerifyAPIKey(plaintext, hash string) bool {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)); err != nil {
		return false
	}
	return true
}
