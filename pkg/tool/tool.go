package tool

import "context"

type ToolInfo struct {
	Name        string
	Description string
	Parameters  any
}

type Result struct {
	Output   any
	IsError  bool
	ErrorMsg string
}

type StreamChunk struct {
	Data  any
	Done  bool
	Error error
}

type ITool interface {
	Info() ToolInfo
	Execute(ctx context.Context, params map[string]any) (Result, error)
	Stream(ctx context.Context, params map[string]any) (<-chan StreamChunk, error)
}

//type ToolManager interface {
//	Register(tool ITool) error
//	Get(name string) ITool
//	List() []ToolInfo
//	Execute(ctx context.Context, name string, params map[string]any) (Result, error)
//	Stream(ctx context.Context, name string, params map[string]any) (<-chan StreamChunk, error)
//}
