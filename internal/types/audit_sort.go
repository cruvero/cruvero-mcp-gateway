package types

import "strings"

const (
	// AuditSortByCreatedAt sorts audit entries by creation timestamp.
	AuditSortByCreatedAt = "created_at"
	// AuditSortByEventType sorts audit entries by event type.
	AuditSortByEventType = "event_type"
	// AuditSortByUsername sorts audit entries by username.
	AuditSortByUsername = "username"
	// AuditSortByServerName sorts audit entries by server name.
	AuditSortByServerName = "server_name"

	// AuditSortDirAsc sorts in ascending order.
	AuditSortDirAsc = "asc"
	// AuditSortDirDesc sorts in descending order.
	AuditSortDirDesc = "desc"
)

// NormalizeAuditSortBy returns a safe, allowlisted audit sort column.
func NormalizeAuditSortBy(sortBy string) string {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case AuditSortByEventType:
		return AuditSortByEventType
	case AuditSortByUsername:
		return AuditSortByUsername
	case AuditSortByServerName:
		return AuditSortByServerName
	default:
		return AuditSortByCreatedAt
	}
}

// NormalizeAuditSortDir returns a safe audit sort direction.
func NormalizeAuditSortDir(sortDir string) string {
	if strings.ToLower(strings.TrimSpace(sortDir)) == AuditSortDirAsc {
		return AuditSortDirAsc
	}
	return AuditSortDirDesc
}
