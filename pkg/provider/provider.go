package provider

import "context"

type ProviderMessage struct {
	Role    string
	Content string
	Tools   []ToolDefinition
}

type ToolDefinition struct {
	Name        string
	Description string
	Parameters  any
}

type Response struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason string
	Usage        Usage
}

type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type StreamChunk struct {
	Delta    string
	ToolCall *ToolCall
	Done     bool
}

type ModelInfo struct {
	Name           string
	Provider       string
	ContextSize    int
	SupportsTool   bool
	SupportsVision bool
}

type IProvider interface {
	Generate(ctx context.Context, messages []ProviderMessage) (Response, error)
	Stream(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error)
	ModelInfo() ModelInfo
}
