package tool

import "context"

// RegisterBuiltinHandlers registers stub handlers for all 5 built-in tools.
// v0.12.3: placeholders. v0.12.4: replace with real implementations.
func RegisterBuiltinHandlers(br *BuiltinRunner) {
	br.RegisterHandler("calculator", stubCalculator)
	br.RegisterHandler("think", stubThink)
	br.RegisterHandler("todo", stubTodo)
	br.RegisterHandler("filesystem.read", stubFilesystemRead)
	br.RegisterHandler("shell", stubShell)
}

func stubCalculator(_ context.Context, params map[string]any) (Result, error) {
	expr, _ := params["expr"].(string)
	return Result{
		Output:  "calculator stub: evaluated \"" + expr + "\" = <v0.12.4>",
		IsError: false,
	}, nil
}

func stubThink(_ context.Context, params map[string]any) (Result, error) {
	thought, _ := params["thought"].(string)
	return Result{
		Output:  "think stub: processed \"" + thought + "\" → <v0.12.4>",
		IsError: false,
	}, nil
}

func stubTodo(_ context.Context, params map[string]any) (Result, error) {
	action, _ := params["action"].(string)
	item, _ := params["item"].(string)
	return Result{
		Output:  "todo stub: " + action + " \"" + item + "\" → <v0.12.4>",
		IsError: false,
	}, nil
}

func stubFilesystemRead(_ context.Context, params map[string]any) (Result, error) {
	path, _ := params["path"].(string)
	return Result{
		Output:  "filesystem.read stub: read \"" + path + "\" → <v0.12.4>",
		IsError: true,
	}, nil
}

func stubShell(_ context.Context, params map[string]any) (Result, error) {
	cmd, _ := params["cmd"].(string)
	return Result{
		Output:  "shell stub: executed \"" + cmd + "\" → <v0.12.4>",
		IsError: true,
	}, nil
}
