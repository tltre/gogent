package tool

import "context"

// Service is the tool-calling surface exposed to agent cores. It deliberately
// omits lifecycle methods (Start/Stop/dial) so custom IAgentCore
// implementations cannot tamper with daemon connectivity — the framework
// (App) owns lifecycle via the ToolManager.
type Service interface {
	// List returns the accepted tool declarations (after manifest
	// registration), used by agents to declare available tools to the model.
	List() []ToolInfo

	// Execute sends a tool execution request to the daemon and returns the
	// final result.
	Execute(ctx context.Context, name string, params map[string]any) (Result, error)
}

// Compile-time assertion: ToolManager satisfies Service.
var _ Service = (*ToolManager)(nil)
