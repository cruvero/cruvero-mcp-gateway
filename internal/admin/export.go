package admin

// CSV export functionality is implemented directly in handler.go:HandleAuditExport.
// This file serves as a documentation anchor for the export feature.
//
// Export endpoint: GET /admin/audit/export
// - Applies same filters as the audit page
// - Capped at 10,000 rows
// - Returns Content-Disposition: attachment with CSV content
