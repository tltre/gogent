package tool

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/tltre/gogent/internal/grpctransport"
	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
)

// ProcessToolConfig holds the configuration for a gRPC-based ProcessTool.
type ProcessToolConfig struct {
	Name        string
	Description string
	ToolName    string
	Pool        *grpctransport.Pool
	Target      string
}

// ProcessTool is a tool implementation that communicates with a remote
// tool service over gRPC.
type ProcessTool struct {
	cfg    *ProcessToolConfig
	client gogentv1.ToolServiceClient
}

// NewProcessTool creates a new ProcessTool. The gRPC client is lazily
// initialized on the first method call.
func NewProcessTool(cfg *ProcessToolConfig) *ProcessTool {
	return &ProcessTool{cfg: cfg}
}

// getClient lazily initializes and returns the gRPC ToolServiceClient.
func (t *ProcessTool) getClient() (gogentv1.ToolServiceClient, error) {
	if t.client != nil {
		return t.client, nil
	}
	conn, err := t.cfg.Pool.Get(t.cfg.Target)
	if err != nil {
		return nil, fmt.Errorf("tool: get connection: %w", err)
	}
	t.client = gogentv1.NewToolServiceClient(conn.ClientConn())
	return t.client, nil
}

// Info returns metadata about this tool.
func (t *ProcessTool) Info() ToolInfo {
	return ToolInfo{
		Name:        t.cfg.Name,
		Description: t.cfg.Description,
	}
}

// Execute sends a tool execution request to the remote tool service via gRPC.
func (t *ProcessTool) Execute(ctx context.Context, params map[string]any) (Result, error) {
	client, err := t.getClient()
	if err != nil {
		return Result{}, err
	}
	pbParams, err := structpb.NewStruct(params)
	if err != nil {
		return Result{}, fmt.Errorf("tool: convert params: %w", err)
	}
	req := &gogentv1.ExecuteToolRequest{
		Name:   t.cfg.ToolName,
		Params: pbParams,
	}
	resp, err := client.ExecuteTool(ctx, req)
	if err != nil {
		return Result{}, err
	}
	if resp.Result == nil {
		return Result{}, nil
	}
	return resultFromProto(resp.Result), nil
}

// Stream emulates streaming by delegating to Execute. The gRPC proto does not
// yet define a server-streaming RPC, so this falls back to single-shot execution.
func (t *ProcessTool) Stream(ctx context.Context, params map[string]any) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 100)
	go func() {
		defer close(ch)
		result, err := t.Execute(ctx, params)
		ch <- StreamChunk{Data: result.Output, Done: true, Error: err}
	}()
	return ch, nil
}

// --- conversion helpers: gRPC proto types → Go domain types ---

func resultFromProto(r *gogentv1.ToolResult) Result {
	res := Result{
		IsError:  r.IsError,
		ErrorMsg: r.ErrorMsg,
	}
	if r.Output != nil {
		res.Output = r.Output.AsInterface()
	}
	return res
}
