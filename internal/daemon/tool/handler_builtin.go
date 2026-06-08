package tool

import "context"

// RegisterBuiltinHandlers registers handlers for all 5 built-in tools.
// calculator, think, todo are real implementations (v0.12.4).
// filesystem.read and shell require sandbox routing; if sandbox is not
// configured, they return a clear configuration error.
func RegisterBuiltinHandlers(br *BuiltinRunner) {
	calc := &calculator{}
	br.RegisterHandler("calculator", calc.exec)
	br.RegisterHandler("think", execThink)
	br.RegisterHandler("todo", execTodo)
	br.RegisterHandler("filesystem.read", needsSandbox("filesystem.read"))
	br.RegisterHandler("shell", needsSandbox("shell"))
}

// needsSandbox returns a handler that tells the user sandbox is required.
// This is the fallback path — when sandbox routing is active in ExecuteTool
// (v0.13.2+), this handler is not called.
func needsSandbox(toolName string) HandlerFunc {
	return func(_ context.Context, params map[string]any) (Result, error) {
		return Result{
			Output:  toolName + " requires a sandbox backend. Configure sandbox.yaml and set E2B_API_KEY.",
			IsError: true,
		}, nil
	}
}
