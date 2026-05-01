package tool

import (
	"context"

	"github.com/tltre/gagent/internal/client"
)

type ProcessToolConfig struct {
	Name        string
	Description string
	ToolName    string
	Transport   client.Transport
}

type ProcessTool struct {
	config *ProcessToolConfig
	tr     *client.LazyTransport
}

func NewProcessTool(cfg *ProcessToolConfig) *ProcessTool {
	return &ProcessTool{
		config: cfg,
		tr:     client.WrapLazy(cfg.Transport),
	}
}

func (t *ProcessTool) Info() ToolInfo {
	return ToolInfo{
		Name:        t.config.Name,
		Description: t.config.Description,
	}
}

func (t *ProcessTool) Execute(ctx context.Context, params map[string]any) (Result, error) {
	type callParams struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	var result Result
	if err := t.tr.Call(ctx, "tools/call", callParams{
		Name:      t.config.ToolName,
		Arguments: params,
	}, &result); err != nil {
		return Result{IsError: true, ErrorMsg: err.Error()}, nil
	}
	return result, nil
}

func (t *ProcessTool) Stream(ctx context.Context, params map[string]any) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 100)
	go func() {
		defer close(ch)
		result, err := t.Execute(ctx, params)
		ch <- StreamChunk{Data: result.Output, Done: true, Error: err}
	}()
	return ch, nil
}
