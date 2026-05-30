package tool

import (
	"context"
	"sync"
)

// HandlerFunc is a function that implements a built-in tool.
type HandlerFunc func(ctx context.Context, params map[string]any) (Result, error)

// BuiltinRunner executes built-in tools via registered handler functions.
// Thread-safe: RegisterHandler/UnregisterHandler can be called at runtime.
type BuiltinRunner struct {
	mu       sync.RWMutex
	handlers map[string]HandlerFunc
}

// NewBuiltinRunner creates a BuiltinRunner with no handlers.
func NewBuiltinRunner() *BuiltinRunner {
	return &BuiltinRunner{
		handlers: make(map[string]HandlerFunc),
	}
}

// RegisterHandler registers a handler function for a built-in tool.
// Thread-safe; can be called at runtime.
func (r *BuiltinRunner) RegisterHandler(name string, fn HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[name] = fn
}

// UnregisterHandler removes a handler (for hot-reload or tool removal).
func (r *BuiltinRunner) UnregisterHandler(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.handlers, name)
}

// Execute dispatches to the registered handler for the tool name.
// Thread-safe; called concurrently from multiple gRPC streams.
func (r *BuiltinRunner) Execute(ctx context.Context, def *ToolDefinition, params map[string]any) (Result, error) {
	r.mu.RLock()
	fn, ok := r.handlers[def.Name]
	r.mu.RUnlock()
	if !ok {
		return Result{IsError: true, ErrorMsg: "no handler registered for builtin tool: " + def.Name}, nil
	}
	return fn(ctx, params)
}
