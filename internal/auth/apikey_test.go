package auth

import (
	"strings"
	"testing"
)

func TestGenerateAPIKeyFormat(t *testing.T) {
	t.Parallel()

	plaintext, lookupHash, bcryptHash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}

	if !strings.HasPrefix(plaintext, APIKeyPrefix) {
		t.Fatalf("expected api key prefix %q, got %q", APIKeyPrefix, plaintext)
	}
	if len(plaintext) <= len(APIKeyPrefix) {
		t.Fatalf("expected non-empty key body after prefix")
	}
	if len(lookupHash) != 64 {
		t.Fatalf("expected 64-char sha256 hex lookup hash, got %d", len(lookupHash))
	}
	if bcryptHash == "" {
		t.Fatal("expected bcrypt hash")
	}
}

func TestLookupHashAPIKeyDeterministic(t *testing.T) {
	t.Parallel()

	key := APIKeyPrefix + "same-key"
	hash1 := LookupHashAPIKey(key)
	hash2 := LookupHashAPIKey(key)
	if hash1 != hash2 {
		t.Fatalf("expected deterministic lookup hash, got %q and %q", hash1, hash2)
	}

	hash3 := LookupHashAPIKey(APIKeyPrefix + "different")
	if hash1 == hash3 {
		t.Fatal("expected different keys to produce different lookup hashes")
	}
}

func TestHashAndVerifyAPIKeyRoundTrip(t *testing.T) {
	t.Parallel()

	key := APIKeyPrefix + "test-round-trip"
	hash, err := HashAPIKey(key)
	if err != nil {
		t.Fatalf("hash api key: %v", err)
	}

	if !VerifyAPIKey(key, hash) {
		t.Fatal("expected hash verification to succeed")
	}
	if VerifyAPIKey(key+"-wrong", hash) {
		t.Fatal("expected hash verification to fail for wrong plaintext")
	}
}

func TestGenerateAPIKeysAreUnique(t *testing.T) {
	t.Parallel()

	key1, lookup1, hash1, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate first api key: %v", err)
	}

	key2, lookup2, hash2, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate second api key: %v", err)
	}

	if key1 == key2 {
		t.Fatal("expected generated plaintext API keys to be unique")
	}
	if lookup1 == lookup2 {
		t.Fatal("expected generated lookup hashes to be unique")
	}
	if hash1 == hash2 {
		t.Fatal("expected generated bcrypt hashes to be unique")
	}
}
