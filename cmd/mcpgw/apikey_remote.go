package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"
)

type remoteCreateRequest struct {
	Name     string   `json:"name"`
	Scopes   []string `json:"scopes"`
	Expires  string   `json:"expires,omitempty"`
	ClientID string   `json:"client_id,omitempty"`
	Profile  string   `json:"profile,omitempty"`
}

type remoteCreateResponse struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	ClientID      string   `json:"client_id"`
	Scopes        []string `json:"scopes"`
	PolicyProfile string   `json:"profile"`
	ExpiresAt     *string  `json:"expires_at"`
	CreatedAt     string   `json:"created_at"`
	APIKey        string   `json:"api_key"`
}

type remoteKeyView struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	ClientID      string   `json:"client_id"`
	Scopes        []string `json:"scopes"`
	PolicyProfile string   `json:"profile"`
	ExpiresAt     *string  `json:"expires_at"`
	CreatedAt     string   `json:"created_at"`
}

func remoteAPIKeyCreate(baseURL, name, scopesRaw, expires, clientID, profile string) error {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return fmt.Errorf("apikey create requires --name")
	}

	scopes := parseScopes(scopesRaw)
	if len(scopes) == 0 {
		return fmt.Errorf("apikey create requires at least one scope")
	}

	reqBody := remoteCreateRequest{
		Name:     trimmedName,
		Scopes:   scopes,
		Expires:  strings.TrimSpace(expires),
		ClientID: strings.TrimSpace(clientID),
		Profile:  strings.TrimSpace(profile),
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal create request: %w", err)
	}

	resp, err := apiKeyHTTPRequest(http.MethodPost, baseURL+"/v1/apikeys", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("create api key: server returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result remoteCreateResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("decode create response: %w", err)
	}

	expiresDisplay := "never"
	if result.ExpiresAt != nil {
		expiresDisplay = *result.ExpiresAt
	}

	_, _ = fmt.Fprintf(stdout, "ID: %s\n", result.ID)
	_, _ = fmt.Fprintf(stdout, "Name: %s\n", result.Name)
	_, _ = fmt.Fprintf(stdout, "Client ID: %s\n", result.ClientID)
	_, _ = fmt.Fprintf(stdout, "Expires At: %s\n", expiresDisplay)
	_, _ = fmt.Fprintf(stdout, "API Key: %s\n", result.APIKey)
	return nil
}

func remoteAPIKeyList(baseURL, format string) error {
	resp, err := apiKeyHTTPRequest(http.MethodGet, baseURL+"/v1/apikeys", nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("list api keys: server returned %d: %s", resp.StatusCode, string(respBody))
	}

	var keys []remoteKeyView
	if err := json.Unmarshal(respBody, &keys); err != nil {
		return fmt.Errorf("decode list response: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(keys)
	case "table", "":
		return remoteAPIKeyListTable(keys)
	default:
		return fmt.Errorf("invalid format %q: expected table or json", format)
	}
}

func remoteAPIKeyListTable(keys []remoteKeyView) error {
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tNAME\tCLIENT_ID\tSCOPES\tEXPIRES_AT\tCREATED_AT")
	for _, key := range keys {
		expiresAt := "never"
		if key.ExpiresAt != nil {
			expiresAt = *key.ExpiresAt
		}
		_, _ = fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\n",
			key.ID,
			key.Name,
			key.ClientID,
			strings.Join(key.Scopes, ","),
			expiresAt,
			key.CreatedAt,
		)
	}
	return tw.Flush()
}

func remoteAPIKeyRevoke(baseURL, id string) error {
	resp, err := apiKeyHTTPRequest(http.MethodDelete, baseURL+"/v1/apikeys/"+id, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("revoke api key: server returned %d: %s", resp.StatusCode, string(respBody))
	}

	_, _ = fmt.Fprintf(stdout, "revoked api key %s\n", id)
	return nil
}

var loadTokenFunc = loadAndRefreshToken

func apiKeyHTTPRequest(method, url string, body io.Reader) (*http.Response, error) {
	tokens, err := loadTokenFunc()
	if err != nil {
		return nil, fmt.Errorf("load auth tokens: %w (run 'mcpgw auth login' first)", err)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClientForAuth.Do(req) // #nosec G107 G704 -- url is the user-configured gateway URL
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("unauthorized: access token may be expired (run 'mcpgw auth login')")
	}

	return resp, nil
}
