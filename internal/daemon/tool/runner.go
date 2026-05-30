package tool

import "context"

// Runner executes a tool and returns the result. Each driver type
// (builtin/process/http) has its own Runner implementation.
type Runner interface {
	// Execute runs the tool synchronously and returns the result.
	Execute(ctx context.Context, def *ToolDefinition, params map[string]any) (Result, error)
}

// Result is the domain-level execution result. Defined here to avoid
// circular dependency with pkg/tool.
type Result struct {
	Output   any
	IsError  bool
	ErrorMsg string
}
