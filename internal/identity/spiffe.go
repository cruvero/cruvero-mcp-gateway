package identity

import (
	"crypto/x509"
	"fmt"
	"net/url"
	"path"
	"strings"
)

// ExtractSPIFFEID extracts the first SPIFFE URI SAN from the leaf certificate.
func ExtractSPIFFEID(certs []*x509.Certificate) (string, error) {
	if len(certs) == 0 {
		return "", fmt.Errorf("extract spiffe id: no certificates provided")
	}

	leaf := certs[0]
	for _, uri := range leaf.URIs {
		if uri != nil && strings.EqualFold(uri.Scheme, "spiffe") {
			spiffeID := uri.String()
			if _, _, err := ParseSPIFFEID(spiffeID); err != nil {
				return "", fmt.Errorf("extract spiffe id: %w", err)
			}
			return spiffeID, nil
		}
	}

	return "", fmt.Errorf("extract spiffe id: no spiffe URI SAN found")
}

// ParseSPIFFEID parses and validates a SPIFFE ID in the form spiffe://trust-domain/workload-path.
func ParseSPIFFEID(raw string) (trustDomain, workloadID string, err error) {
	if strings.TrimSpace(raw) == "" {
		return "", "", fmt.Errorf("parse spiffe id: empty id")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("parse spiffe id: %w", err)
	}

	if !strings.EqualFold(parsed.Scheme, "spiffe") {
		return "", "", fmt.Errorf("parse spiffe id: invalid scheme %q", parsed.Scheme)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return "", "", fmt.Errorf("parse spiffe id: missing trust domain")
	}

	workloadPath := strings.TrimSpace(parsed.EscapedPath())
	if workloadPath == "" || workloadPath == "/" {
		return "", "", fmt.Errorf("parse spiffe id: missing workload path")
	}

	cleanPath := path.Clean(workloadPath)
	if cleanPath == "." || cleanPath == "/" || strings.Contains(cleanPath, "..") {
		return "", "", fmt.Errorf("parse spiffe id: invalid workload path %q", workloadPath)
	}

	cleanPath = strings.TrimPrefix(cleanPath, "/")
	if cleanPath == "" {
		return "", "", fmt.Errorf("parse spiffe id: missing workload path")
	}

	return parsed.Host, cleanPath, nil
}

// ValidateSPIFFEID validates a SPIFFE ID and enforces allowlist prefixes.
func ValidateSPIFFEID(id string, allowedPrefixes []string) error {
	if _, _, err := ParseSPIFFEID(id); err != nil {
		return fmt.Errorf("validate spiffe id: %w", err)
	}

	if len(allowedPrefixes) == 0 {
		return nil
	}

	for _, prefix := range allowedPrefixes {
		trimmed := strings.TrimSpace(prefix)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(id, trimmed) {
			return nil
		}
	}

	return fmt.Errorf("validate spiffe id: %q does not match allowed prefixes", id)
}
