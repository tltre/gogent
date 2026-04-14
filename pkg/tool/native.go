package tool

import (
	"context"
)

type NativeTool struct {
	info      ToolInfo
	executeFn func(ctx context.Context, params map[string]any) (Result, error)
	streamFn  func(ctx context.Context, params map[string]any) (<-chan StreamChunk, error)
}

func NewNativeTool(info ToolInfo, executeFn func(ctx context.Context, params map[string]any) (Result, error)) *NativeTool {
	return &NativeTool{
		info:      info,
		executeFn: executeFn,
	}
}

func (t *NativeTool) Info() ToolInfo {
	return t.info
}

func (t *NativeTool) Execute(ctx context.Context, params map[string]any) (Result, error) {
	if t.executeFn != nil {
		return t.executeFn(ctx, params)
	}
	return Result{}, nil
}

func (t *NativeTool) Stream(ctx context.Context, params map[string]any) (<-chan StreamChunk, error) {
	if t.streamFn != nil {
		return t.streamFn(ctx, params)
	}
	ch := make(chan StreamChunk)
	go func() {
		result, err := t.Execute(ctx, params)
		ch <- StreamChunk{Data: result.Output, Done: true, Error: err}
		close(ch)
	}()
	return ch, nil
}

func (t *NativeTool) SetStream(fn func(ctx context.Context, params map[string]any) (<-chan StreamChunk, error)) *NativeTool {
	t.streamFn = fn
	return t
}
