package agentcore

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/tltre/gogent/internal/otel"
	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/contextmanager"
	"github.com/tltre/gogent/pkg/hook"
	"github.com/tltre/gogent/pkg/memory"
	"github.com/tltre/gogent/pkg/provider"
	"github.com/tltre/gogent/pkg/tool"
)

// ReAct loop limits (v0.15.3). Hardcoded for now; config exposure is a
// follow-up (design decision D3).
const (
	// maxReActIterations caps the reasoning → acting → observing loop to
	// prevent infinite tool-call loops.
	maxReActIterations = 10

	// maxToolResultSize truncates tool results before they are fed back to
	// the model (head + tail kept, middle elided) to protect the context
	// window (design decision D5).
	maxToolResultSize = 8 * 1024
)

// ReactAgent is the default IAgentCore implementation for the "react" agent
// type. It runs the full ReAct loop (v0.15.3): it declares available tools to
// the provider, iterates reasoning → tool calls → observation until the model
// stops calling tools (or the iteration cap is hit), persists conversation
// history via ContextManager, and archives tool interactions to Memory.
//
// ContextManager is the agent's single input source (BuildInput assembles
// system prompt + recalled memory + history + user messages).
type ReactAgent struct {
	runtime *AgentRuntime
	manager *provider.ProviderManager
}

var _ IAgentCore = (*ReactAgent)(nil)

// NewReactAgent creates a ReactAgent. Dependencies are resolved lazily from
// the Registry in SetAgentRuntime.
func NewReactAgent() *ReactAgent {
	return &ReactAgent{}
}

// init registers the "react" agent type into the agent type registry.
func init() {
	_ = RegisterAgentType("react", func() IAgentCore {
		return NewReactAgent()
	})
}

// SetAgentRuntime stores the runtime reference and resolves the
// ProviderManager from the Registry.
func (a *ReactAgent) SetAgentRuntime(runtime *AgentRuntime) {
	a.runtime = runtime
	if comp := runtime.Reg().GetDefault(component.ComponentProvider); comp != nil {
		if mgr, ok := comp.(*provider.ProviderManager); ok {
			a.manager = mgr
		}
	}
}

// Run executes the full ReAct loop and returns the final output.
func (a *ReactAgent) Run(ctx context.Context, input Input) (Output, error) {
	if a.manager == nil {
		return Output{}, fmt.Errorf("react agent: no ProviderManager available")
	}

	ctx = provider.WithProviderName(ctx, input.ProviderName)
	ctx = provider.WithModel(ctx, input.ModelName)

	// v0.15.x: observe the whole ReAct run; LLM calls and tool executions are
	// recorded as child spans/events (ToolManager emits tool.exec).
	tracer := otel.Tracer("gogent.agent")
	ctx, span := tracer.Start(ctx, "agent.run",
		trace.WithAttributes(
			attribute.String("provider", provider.ProviderNameFrom(ctx)),
			attribute.String("model", provider.ModelFrom(ctx)),
			attribute.String("session", input.SessionID),
		))
	defer span.End()
	start := time.Now()

	output, err := a.runReAct(ctx, input)
	span.SetAttributes(attribute.Int64("dur_ms", time.Since(start).Milliseconds()))
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return output, err
	}
	span.SetStatus(codes.Ok, "")
	return output, nil
}

// runReAct is the core ReAct loop (moved out of Run so the agent.run span wraps
// the whole iteration).
func (a *ReactAgent) runReAct(ctx context.Context, input Input) (Output, error) {
	tracer := otel.Tracer("gogent.agent")

	// 1. Tool declarations (from the framework-managed tool service).
	var toolDefs []provider.ToolDefinition
	if svc := a.runtime.ToolService(); svc != nil {
		for _, t := range svc.List() {
			toolDefs = append(toolDefs, provider.ToolDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			})
		}
	}

	// 2. Assemble the input — ContextManager is the single input source.
	var messages []provider.ProviderMessage
	cm := a.contextManager()
	if cm != nil {
		built := cm.BuildInput(input.SessionID, toContextMessages(input.Messages))
		messages = contextMessagesToProvider(built, toolDefs)
	} else {
		messages = toProviderMessages(input.Messages)
		if len(toolDefs) > 0 && len(messages) > 0 {
			messages[len(messages)-1].Tools = toolDefs
		}
	}

	// 3. ReAct loop: reason → act → observe → iterate.
	var actions []Action
	finalContent := ""
	finishReason := ""
	iterations := 0
	maxReached := false

	for iterations < maxReActIterations {
		select {
		case <-ctx.Done():
			return Output{}, ctx.Err()
		default:
		}
		iterations++

		// v0.15.x: observe each LLM call within the ReAct loop.
		a.triggerHook(ctx, hook.EventBeforeLLM)
		llmStart := time.Now()
		_, llmSpan := tracer.Start(ctx, "agent.llm.generate",
			trace.WithAttributes(attribute.Int("msg_count", len(messages))))
		resp, err := a.manager.Generate(ctx, messages)
		llmSpan.SetAttributes(
			attribute.Int64("dur_ms", time.Since(llmStart).Milliseconds()),
			attribute.Int("tool_calls", len(resp.ToolCalls)),
		)
		if err != nil {
			llmSpan.SetStatus(codes.Error, err.Error())
			llmSpan.End()
			a.triggerHook(ctx, hook.EventAfterLLM)
			return Output{}, err
		}
		llmSpan.SetStatus(codes.Ok, "")
		llmSpan.End()
		a.triggerHook(ctx, hook.EventAfterLLM)

		if len(resp.ToolCalls) == 0 {
			finalContent = resp.Content
			finishReason = resp.FinishReason
			break
		}

		// Echo the assistant message (with tool_calls) back to the provider.
		messages = append(messages, provider.ProviderMessage{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute each requested tool and feed the observation back.
		for _, tc := range resp.ToolCalls {
			a.triggerHook(ctx, hook.EventBeforeTool)
			result, execErr := a.executeTool(ctx, tc)
			a.triggerHook(ctx, hook.EventAfterTool)

			content := truncate(formatToolResult(tc, result, execErr), maxToolResultSize)
			messages = append(messages, provider.ProviderMessage{
				Role:       "tool",
				Content:    content,
				ToolCallID: tc.ID,
			})

			// Archive the tool interaction to long-term memory (C5):
			// process artifacts are stored, not the conversation history.
			a.archiveToolCall(ctx, tc, result, execErr)

			actions = append(actions, Action{
				ToolName: tc.Name,
				Params:   tc.Args,
				Result:   result.Output,
			})
		}
	}

	if iterations >= maxReActIterations {
		maxReached = true
	}

	// 4. Persist conversation history (only user + final assistant).
	if cm != nil && input.SessionID != "" {
		if last := lastUserContent(input.Messages); last != "" {
			_ = cm.AddMessage(input.SessionID, contextmanager.ContextMessage{Role: "user", Content: last})
		}
		if finalContent != "" {
			_ = cm.AddMessage(input.SessionID, contextmanager.ContextMessage{Role: "assistant", Content: finalContent})
		}
	}

	return Output{
		Response: Message{Role: "assistant", Content: finalContent},
		Actions:  actions,
		Metadata: map[string]any{
			"provider":            provider.ProviderNameFrom(ctx),
			"model":               provider.ModelFrom(ctx),
			"finish":              finishReason,
			"iterations":          iterations,
			"maxIterationsReached": maxReached,
		},
	}, nil
}

// Stream keeps the v0.14.7 single-turn streaming behavior (design decision
// D1: streaming ReAct is a follow-up).
func (a *ReactAgent) Stream(ctx context.Context, input Input) (<-chan Event, error) {
	if a.manager == nil {
		ch := make(chan Event, 1)
		ch <- Event{Type: EventError, Error: fmt.Errorf("react agent: no ProviderManager available")}
		close(ch)
		return ch, nil
	}

	ctx = provider.WithProviderName(ctx, input.ProviderName)
	ctx = provider.WithModel(ctx, input.ModelName)

	stream, err := a.manager.Stream(ctx, toProviderMessages(input.Messages))
	if err != nil {
		ch := make(chan Event, 1)
		ch <- Event{Type: EventError, Error: err}
		close(ch)
		return ch, nil
	}

	ch := make(chan Event, 100)
	go func() {
		defer close(ch)
		var content string
		for chunk := range stream {
			content += chunk.Delta
			ch <- Event{Type: EventStreaming, Payload: chunk.Delta}
			if chunk.Done {
				break
			}
		}
		ch <- Event{
			Type: EventAfterRun,
			Payload: Output{
				Response: Message{Role: "assistant", Content: content},
			},
		}
	}()
	return ch, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// executeTool runs a single tool call through the framework tool service.
func (a *ReactAgent) executeTool(ctx context.Context, tc provider.ToolCall) (tool.Result, error) {
	svc := a.runtime.ToolService()
	if svc == nil {
		return tool.Result{}, fmt.Errorf("no tool service available")
	}
	return svc.Execute(ctx, tc.Name, tc.Args)
}

// formatToolResult renders a tool result (or error) as text for the model.
func formatToolResult(tc provider.ToolCall, result tool.Result, execErr error) string {
	if execErr != nil {
		return fmt.Sprintf("tool %s(%v) failed: %v", tc.Name, tc.Args, execErr)
	}
	if result.IsError {
		return fmt.Sprintf("tool %s(%v) error: %s", tc.Name, tc.Args, result.ErrorMsg)
	}
	return fmt.Sprintf("tool %s(%v) → %v", tc.Name, tc.Args, result.Output)
}

// truncate keeps the head and tail of s, eliding the middle when it exceeds
// max bytes (design decision D5).
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	half := max / 2
	head := s[:half]
	tail := s[len(s)-half:]
	return head + "\n...[truncated]...\n" + tail
}

// archiveToolCall stores a tool interaction in long-term memory (C5): the
// artifact is searchable across sessions without polluting conversation
// history.
func (a *ReactAgent) archiveToolCall(ctx context.Context, tc provider.ToolCall, result tool.Result, execErr error) {
	mem := a.memory()
	if mem == nil {
		return
	}
	content := formatToolResult(tc, result, execErr)
	_ = mem.Add(ctx, memory.MemoryItem{
		ID:      "tool-" + tc.ID,
		Content: truncate(content, maxToolResultSize),
		Extra: map[string]any{
			"kind":  "tool-call",
			"tool":  tc.Name,
		},
	})
}

// triggerHook fires a hook event through the HookManager, if registered.
func (a *ReactAgent) triggerHook(ctx context.Context, eventType hook.EventType) {
	comp := a.runtime.Reg().GetDefault(component.ComponentHook)
	hm, ok := comp.(*hook.HookManager)
	if !ok || hm == nil {
		return
	}
	_, _ = hm.Trigger(ctx, hook.Event{Type: eventType, Name: "react"})
}

// contextManager resolves the ContextManager component from the Registry.
func (a *ReactAgent) contextManager() *contextmanager.ContextManagerComponent {
	comp := a.runtime.Reg().GetDefault(component.ComponentContextManager)
	cm, ok := comp.(*contextmanager.ContextManagerComponent)
	if !ok {
		return nil
	}
	return cm
}

// memory resolves the Memory component from the Registry.
func (a *ReactAgent) memory() *memory.MemoryComponent {
	comp := a.runtime.Reg().GetDefault(component.ComponentMemory)
	mc, ok := comp.(*memory.MemoryComponent)
	if !ok {
		return nil
	}
	return mc
}

// lastUserContent returns the content of the last user message, or "".
func lastUserContent(msgs []Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Content
		}
	}
	return ""
}

// toContextMessages converts agentcore messages to contextmanager messages.
func toContextMessages(msgs []Message) []contextmanager.ContextMessage {
	result := make([]contextmanager.ContextMessage, 0, len(msgs))
	for _, m := range msgs {
		result = append(result, contextmanager.ContextMessage{Role: m.Role, Content: m.Content})
	}
	return result
}

// contextMessagesToProvider converts contextmanager messages to provider
// messages, attaching tool declarations to the final user message.
func contextMessagesToProvider(msgs []contextmanager.ContextMessage, toolDefs []provider.ToolDefinition) []provider.ProviderMessage {
	result := make([]provider.ProviderMessage, 0, len(msgs))
	for i, m := range msgs {
		pm := provider.ProviderMessage{Role: m.Role, Content: m.Content}
		if i == len(msgs)-1 && len(toolDefs) > 0 {
			pm.Tools = toolDefs
		}
		result = append(result, pm)
	}
	return result
}

// toProviderMessages converts agentcore messages to provider messages.
func toProviderMessages(msgs []Message) []provider.ProviderMessage {
	result := make([]provider.ProviderMessage, 0, len(msgs))
	for _, m := range msgs {
		pm := provider.ProviderMessage{Role: m.Role, Content: m.Content}
		for _, t := range m.Tools {
			pm.Tools = append(pm.Tools, provider.ToolDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			})
		}
		result = append(result, pm)
	}
	return result
}
