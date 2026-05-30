package tool

import "context"

// RegisterBuiltinHandlers registers handlers for all 5 built-in tools.
// calculator, think, todo are real implementations (v0.12.4).
// filesystem.read and shell remain stubs pending sandbox routing (v0.12.8).
//
// See v0.12.x roadmap for built-in tool enhancement plans.
func RegisterBuiltinHandlers(br *BuiltinRunner) {
	calc := &calculator{}
	br.RegisterHandler("calculator", calc.exec)
	br.RegisterHandler("think", execThink)
	br.RegisterHandler("todo", execTodo)
	br.RegisterHandler("filesystem.read", stubFilesystemRead)
	br.RegisterHandler("shell", stubShell)
}

// --- stubs for v0.12.8 ---

func stubFilesystemRead(_ context.Context, params map[string]any) (Result, error) {
	path, _ := params["path"].(string)
	return Result{
		Output:  "filesystem.read stub: read \"" + path + "\" → <v0.12.8>",
		IsError: true,
	}, nil
}

func stubShell(_ context.Context, params map[string]any) (Result, error) {
	cmd, _ := params["cmd"].(string)
	return Result{
		Output:  "shell stub: executed \"" + cmd + "\" → <v0.12.8>",
		IsError: true,
	}, nil
}
