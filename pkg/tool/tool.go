package tool

// ToolInfo describes a tool known to the system.
type ToolInfo struct {
	Name        string
	Description string
	Parameters  any
}

// Result is the outcome of a tool execution.
type Result struct {
	Output   any
	IsError  bool
	ErrorMsg string
}

// StreamChunk represents a single chunk in a streaming tool execution.
type StreamChunk struct {
	Data  any
	Done  bool
	Error error
}
