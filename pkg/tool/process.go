package tool

import (
	"context"

	"github.com/yourorg/gagent/pkg/protocol/mcp"
)

type ProcessToolConfig struct {
	Name        string
	Description string
	Command     []string
	ToolName    string
}

type ProcessTool struct {
	config *ProcessToolConfig
	client *mcp.Client
}

func NewProcessTool(cfg *ProcessToolConfig) (*ProcessTool, error) {
	client, err := mcp.NewClient(cfg.Command...)
	if err != nil {
		return nil, err
	}
	return &ProcessTool{
		config: cfg,
		client: client,
	}, nil
}

func (t *ProcessTool) Info() ToolInfo {
	return ToolInfo{
		Name:        t.config.Name,
		Description: t.config.Description,
	}
}

func (t *ProcessTool) Execute(ctx context.Context, params map[string]any) (Result, error) {
	resp, err := t.client.CallTool(ctx, t.config.ToolName, params)
	if err != nil {
		return Result{IsError: true, ErrorMsg: err.Error()}, nil
	}

	if resp.IsError {
		return Result{IsError: true, ErrorMsg: resp.Error}, nil
	}

	return Result{Output: resp.Content}, nil
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
