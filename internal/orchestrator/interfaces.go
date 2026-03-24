package orchestrator

import "context"

// ToolDiscoverer finds tools by keyword and retrieves full schemas.
type ToolDiscoverer interface {
	SearchTools(ctx context.Context, query string, limit int) ([]CandidateTool, error)
	GetToolSchemas(ctx context.Context, names []string) ([]CandidateTool, error)
}

// ToolExecutor invokes a tool by name with arguments and returns its output.
type ToolExecutor interface {
	ExecuteTool(ctx context.Context, toolName string, args map[string]any) (output string, isError bool, err error)
}

// AuditLogger records orchestration events for audit trails.
type AuditLogger interface {
	LogOrchestration(ctx context.Context, eventType string, details map[string]any)
}

// ToolActivator registers tools into the caller's MCP session.
type ToolActivator interface {
	ActivateTools(ctx context.Context, toolNames []string)
}
