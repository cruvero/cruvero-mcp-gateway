package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	tokenCacheDir      = ".mcpgw"
	tokenCacheFile     = "token.json"
	tokenRefreshWindow = 5 * time.Minute
)

// CachedTokens stores OAuth2 tokens on disk.
type CachedTokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	IDToken      string    `json:"id_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
	GatewayURL   string    `json:"gateway_url,omitempty"`
}

// IsExpired reports whether the access token has expired.
func (t *CachedTokens) IsExpired() bool {
	if t == nil {
		return true
	}
	return time.Now().After(t.ExpiresAt)
}

// NeedsRefresh reports whether the access token should be refreshed.
func (t *CachedTokens) NeedsRefresh() bool {
	if t == nil {
		return true
	}
	return time.Now().Add(tokenRefreshWindow).After(t.ExpiresAt)
}

func tokenCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("token cache: resolve home dir: %w", err)
	}
	return filepath.Join(home, tokenCacheDir, tokenCacheFile), nil
}

// SaveTokens writes tokens to the cache file atomically.
func SaveTokens(tokens *CachedTokens) error {
	if tokens == nil {
		return fmt.Errorf("save tokens: tokens is nil")
	}

	cachePath, err := tokenCachePath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(cachePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("save tokens: create dir: %w", err)
	}

	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("save tokens: marshal: %w", err)
	}

	tmpFile := cachePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return fmt.Errorf("save tokens: write temp: %w", err)
	}

	if err := os.Rename(tmpFile, cachePath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("save tokens: rename: %w", err)
	}

	return nil
}

// LoadTokens reads tokens from the cache file.
func LoadTokens() (*CachedTokens, error) {
	cachePath, err := tokenCachePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("load tokens: not logged in (no cached tokens)")
		}
		return nil, fmt.Errorf("load tokens: read: %w", err)
	}

	var tokens CachedTokens
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("load tokens: unmarshal: %w", err)
	}

	return &tokens, nil
}

// DeleteTokens removes the cached token file.
func DeleteTokens() error {
	cachePath, err := tokenCachePath()
	if err != nil {
		return err
	}

	if err := os.Remove(cachePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("delete tokens: %w", err)
	}
	return nil
}

// RefreshAccessToken uses the refresh token to obtain a new access token.
func RefreshAccessToken(tokens *CachedTokens, tokenURL, clientID string) (*CachedTokens, error) {
	if tokens == nil || strings.TrimSpace(tokens.RefreshToken) == "" {
		return nil, fmt.Errorf("refresh token: no refresh token available")
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", tokens.RefreshToken)
	form.Set("client_id", clientID)

	resp, err := http.PostForm(tokenURL, form)
	if err != nil {
		return nil, fmt.Errorf("refresh token: post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh token: idp returned status %d", resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("refresh token: decode: %w", err)
	}

	refreshToken := tokenResp.RefreshToken
	if refreshToken == "" {
		refreshToken = tokens.RefreshToken
	}

	updated := &CachedTokens{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: refreshToken,
		IDToken:      tokenResp.IDToken,
		TokenType:    tokenResp.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		GatewayURL:   tokens.GatewayURL,
	}

	if err := SaveTokens(updated); err != nil {
		return nil, fmt.Errorf("refresh token: save: %w", err)
	}

	return updated, nil
}
