package provider

import "context"

// ProviderMessage is a single chat message exchanged with a provider.
//
// v0.15.1 additions support the ReAct multi-turn tool-calling loop:
//   - ToolCalls: assistant messages echo the tool calls the model requested
//     in the previous turn (OpenAI "tool_calls" field).
//   - ToolCallID: tool result messages link back to the tool call they are
//     the result of (OpenAI "tool_call_id" field).
type ProviderMessage struct {
	Role    string
	Content string
	Tools   []ToolDefinition

	// ToolCalls, when Role == "assistant", carries the tool calls the model
	// requested so the provider can continue the multi-turn conversation.
	ToolCalls []ToolCall

	// ToolCallID, when Role == "tool", links this result message to the
	// originating tool call.
	ToolCallID string
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

type IProvider interface {
	Generate(ctx context.Context, messages []ProviderMessage) (Response, error)
	Stream(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error)
	ModelInfo() ModelInfo
}
