package agentcore

import (
	"context"
	"strings"
	"testing"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/contextmanager"
	"github.com/tltre/gogent/pkg/memory"
	"github.com/tltre/gogent/pkg/provider"
	"github.com/tltre/gogent/pkg/tool"
)

// reactLoopProvider is a scripted provider: it returns responses from a
// script in order, then a terminal "stop" response.
type reactLoopProvider struct {
	name     string
	script   []provider.Response
	calls    int
	lastMsgs []provider.ProviderMessage
}

func (p *reactLoopProvider) Generate(_ context.Context, msgs []provider.ProviderMessage) (provider.Response, error) {
	p.calls++
	p.lastMsgs = msgs
	if p.calls-1 < len(p.script) {
		return p.script[p.calls-1], nil
	}
	return provider.Response{Content: "final-" + p.name, FinishReason: "stop"}, nil
}

func (p *reactLoopProvider) Stream(_ context.Context, _ []provider.ProviderMessage) (<-chan provider.StreamChunk, error) {
	ch := make(chan provider.StreamChunk, 1)
	ch <- provider.StreamChunk{Delta: "s", Done: true}
	close(ch)
	return ch, nil
}

func (p *reactLoopProvider) ModelInfo() provider.ModelInfo {
	return provider.ModelInfo{Name: "m-" + p.name, Provider: p.name, DisplayName: p.name}
}

// reactLoopService is a scripted tool.Service.
type reactLoopService struct {
	tools    []tool.ToolInfo
	calls    []string
	lastArgs map[string]any
	err      error // if set, Execute returns this error
}

func (s *reactLoopService) List() []tool.ToolInfo { return s.tools }

func (s *reactLoopService) Execute(_ context.Context, name string, params map[string]any) (tool.Result, error) {
	s.calls = append(s.calls, name)
	s.lastArgs = params
	if s.err != nil {
		return tool.Result{}, s.err
	}
	return tool.Result{Output: "result-of-" + name}, nil
}

// newReactLoopSetup builds a runtime with ProviderManager, optional
// ContextManager/Memory, and injects the tool service.
func newReactLoopSetup(t *testing.T, withCM, withMemory bool) (*AgentRuntime, *reactLoopProvider, *reactLoopService) {
	t.Helper()

	prov := &reactLoopProvider{name: "loop"}
	mgr := provider.NewManagerComponent("provider-manager")
	if err := mgr.Register("loop", prov); err != nil {
		t.Fatalf("Register() = %v", err)
	}

	reg := component.NewRegistry()
	if err := reg.Register(mgr); err != nil {
		t.Fatalf("Register manager = %v", err)
	}
	reg.SetDefault(component.ComponentProvider, mgr.GetName())

	if withCM {
		cm := contextmanager.NewComponent("contextmanager-default", contextmanager.NewDefaultContextManager())
		if err := reg.Register(cm); err != nil {
			t.Fatalf("Register cm = %v", err)
		}
		reg.SetDefault(component.ComponentContextManager, cm.GetName())
	}
	if withMemory {
		mem := memory.NewComponent("memory-default", memory.NewDefaultMemory())
		if err := reg.Register(mem); err != nil {
			t.Fatalf("Register mem = %v", err)
		}
		reg.SetDefault(component.ComponentMemory, mem.GetName())
	}

	runtime := NewComponent("agent-main", NewReactAgent())
	if err := reg.Register(runtime); err != nil {
		t.Fatalf("Register runtime = %v", err)
	}
	reg.SetDefault(component.ComponentAgentCore, runtime.GetName())

	if err := reg.InitializeAll(context.Background()); err != nil {
		t.Fatalf("InitializeAll() = %v", err)
	}

	svc := &reactLoopService{tools: []tool.ToolInfo{{Name: "calc", Description: "calc"}}}
	runtime.SetToolService(svc)

	return runtime, prov, svc
}

// ---------------------------------------------------------------------------
// Multi-turn ReAct loop
// ---------------------------------------------------------------------------

func TestReactLoopToolIteration(t *testing.T) {
	runtime, prov, svc := newReactLoopSetup(t, false, false)

	// Round 1: model requests a tool call. Round 2: model answers.
	prov.script = []provider.Response{
		{
			Content: "",
			ToolCalls: []provider.ToolCall{
				{ID: "call_1", Name: "calc", Args: map[string]any{"expr": "1+1"}},
			},
			FinishReason: "tool_calls",
		},
		{Content: "the answer is 2", FinishReason: "stop"},
	}

	out, err := runtime.Run(context.Background(), Input{
		Messages:     []Message{{Role: "user", Content: "what is 1+1?"}},
		ProviderName: "loop",
	})
	if err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if out.Response.Content != "the answer is 2" {
		t.Errorf("Content = %q, want 'the answer is 2'", out.Response.Content)
	}
	if len(svc.calls) != 1 || svc.calls[0] != "calc" {
		t.Fatalf("tool calls = %v, want [calc]", svc.calls)
	}
	if len(out.Actions) != 1 || out.Actions[0].ToolName != "calc" {
		t.Fatalf("Actions = %+v, want one calc action", out.Actions)
	}

	// The provider must have seen: user + assistant(tool_calls) + tool result.
	msgs := prov.lastMsgs
	if len(msgs) != 3 {
		t.Fatalf("final messages len = %d, want 3 (user+assistant+tool), got %+v", len(msgs), msgs)
	}
	if msgs[1].Role != "assistant" || len(msgs[1].ToolCalls) != 1 {
		t.Errorf("msgs[1] = %+v, want assistant with tool_calls echo", msgs[1])
	}
	if msgs[2].Role != "tool" || msgs[2].ToolCallID != "call_1" {
		t.Errorf("msgs[2] = %+v, want tool result linked to call_1", msgs[2])
	}
	if !strings.Contains(msgs[2].Content, "result-of-calc") {
		t.Errorf("tool result content = %q, want result-of-calc", msgs[2].Content)
	}
}

func TestReactLoopToolDeclaration(t *testing.T) {
	runtime, prov, _ := newReactLoopSetup(t, false, false)
	prov.script = []provider.Response{{Content: "ok", FinishReason: "stop"}}

	if _, err := runtime.Run(context.Background(), Input{
		Messages:     []Message{{Role: "user", Content: "hi"}},
		ProviderName: "loop",
	}); err != nil {
		t.Fatalf("Run() = %v", err)
	}

	// First message must carry the tool declaration.
	first := prov.lastMsgs[0]
	if len(first.Tools) != 1 || first.Tools[0].Name != "calc" {
		t.Errorf("tool declaration missing on first message: %+v", first.Tools)
	}
}

func TestReactLoopToolErrorBackfill(t *testing.T) {
	runtime, prov, svc := newReactLoopSetup(t, false, false)
	svc.err = context.DeadlineExceeded // simulate tool execution failure
	prov.script = []provider.Response{
		{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "calc", Args: map[string]any{}}}, FinishReason: "tool_calls"},
		{Content: "recovered", FinishReason: "stop"},
	}

	out, err := runtime.Run(context.Background(), Input{
		Messages:     []Message{{Role: "user", Content: "go"}},
		ProviderName: "loop",
	})
	if err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if out.Response.Content != "recovered" {
		t.Errorf("Content = %q, want recovered (loop continued after tool error)", out.Response.Content)
	}
	// The tool result fed back must mention the failure (D4).
	toolMsg := prov.lastMsgs[2]
	if !strings.Contains(toolMsg.Content, "failed") {
		t.Errorf("tool result content = %q, want error backfill", toolMsg.Content)
	}
}

func TestReactLoopMaxIterations(t *testing.T) {
	runtime, prov, _ := newReactLoopSetup(t, false, false)
	// Model always requests tools → loop must cap at maxReActIterations.
	script := make([]provider.Response, 0, maxReActIterations+1)
	for i := 0; i < maxReActIterations+1; i++ {
		script = append(script, provider.Response{
			ToolCalls:   []provider.ToolCall{{ID: "c", Name: "calc", Args: map[string]any{}}},
			FinishReason: "tool_calls",
		})
	}
	prov.script = script

	out, err := runtime.Run(context.Background(), Input{
		Messages:     []Message{{Role: "user", Content: "loop"}},
		ProviderName: "loop",
	})
	if err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if maxReached, _ := out.Metadata["maxIterationsReached"].(bool); !maxReached {
		t.Errorf("maxIterationsReached = %v, want true", out.Metadata["maxIterationsReached"])
	}
	if iters, _ := out.Metadata["iterations"].(int); iters != maxReActIterations {
		t.Errorf("iterations = %d, want %d", iters, maxReActIterations)
	}
}

func TestTruncate(t *testing.T) {
	big := strings.Repeat("a", 10000)
	got := truncate(big, 1000)
	if len(got) >= len(big) {
		t.Errorf("truncate did not shrink: %d bytes", len(got))
	}
	if !strings.Contains(got, "...[truncated]...") {
		t.Error("truncate missing elision marker")
	}
	// head + tail preserved.
	if !strings.HasPrefix(got, "aaaa") || !strings.HasSuffix(got, "aaaa") {
		t.Error("truncate should keep head and tail")
	}
}

// ---------------------------------------------------------------------------
// Context integration: BuildInput + history persistence + memory archive
// ---------------------------------------------------------------------------

func TestReactLoopHistoryPersisted(t *testing.T) {
	runtime, prov, _ := newReactLoopSetup(t, true, false)
	prov.script = []provider.Response{{Content: "hi there", FinishReason: "stop"}}

	sid := runtime.Reg().GetDefault(component.ComponentContextManager).(*contextmanager.ContextManagerComponent).NewSession()

	if _, err := runtime.Run(context.Background(), Input{
		Messages:     []Message{{Role: "user", Content: "hello"}},
		ProviderName: "loop",
		SessionID:    sid,
	}); err != nil {
		t.Fatalf("Run() = %v", err)
	}

	cm := runtime.Reg().GetDefault(component.ComponentContextManager).(*contextmanager.ContextManagerComponent)
	history := cm.GetMessages(sid)
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2 (user + assistant), got %+v", len(history), history)
	}
	if history[0].Role != "user" || history[0].Content != "hello" {
		t.Errorf("history[0] = %+v, want user hello", history[0])
	}
	if history[1].Role != "assistant" || history[1].Content != "hi there" {
		t.Errorf("history[1] = %+v, want assistant hi there", history[1])
	}
}

func TestReactLoopBuildInputUsed(t *testing.T) {
	runtime, prov, _ := newReactLoopSetup(t, true, false)
	prov.script = []provider.Response{{Content: "ok", FinishReason: "stop"}}

	sid := runtime.Reg().GetDefault(component.ComponentContextManager).(*contextmanager.ContextManagerComponent).NewSession()
	// Seed history.
	cm := runtime.Reg().GetDefault(component.ComponentContextManager).(*contextmanager.ContextManagerComponent)
	_ = cm.AddMessage(sid, contextmanager.ContextMessage{Role: "user", Content: "earlier turn"})
	_ = cm.AddMessage(sid, contextmanager.ContextMessage{Role: "assistant", Content: "earlier reply"})

	if _, err := runtime.Run(context.Background(), Input{
		Messages:     []Message{{Role: "user", Content: "now"}},
		ProviderName: "loop",
		SessionID:    sid,
	}); err != nil {
		t.Fatalf("Run() = %v", err)
	}

	// Provider must have seen system + earlier history + current user.
	msgs := prov.lastMsgs
	if len(msgs) != 4 {
		t.Fatalf("messages len = %d, want 4 (system+2 history+user), got %+v", len(msgs), msgs)
	}
	if msgs[0].Role != "system" {
		t.Errorf("msgs[0].Role = %q, want system (BuildInput)", msgs[0].Role)
	}
	if msgs[1].Content != "earlier turn" || msgs[2].Content != "earlier reply" {
		t.Errorf("history not loaded: %+v", msgs[1:3])
	}
	if msgs[3].Content != "now" {
		t.Errorf("current user msg = %q, want now", msgs[3].Content)
	}
}

func TestReactLoopToolArchivedToMemory(t *testing.T) {
	runtime, prov, _ := newReactLoopSetup(t, false, true)
	prov.script = []provider.Response{
		{ToolCalls: []provider.ToolCall{{ID: "call_x", Name: "calc", Args: map[string]any{"expr": "2*2"}}}, FinishReason: "tool_calls"},
		{Content: "done", FinishReason: "stop"},
	}

	if _, err := runtime.Run(context.Background(), Input{
		Messages:     []Message{{Role: "user", Content: "calc"}},
		ProviderName: "loop",
	}); err != nil {
		t.Fatalf("Run() = %v", err)
	}

	mem := runtime.Reg().GetDefault(component.ComponentMemory).(*memory.MemoryComponent)
	items, err := mem.Query(context.Background(), memory.Query{TopK: 10})
	if err != nil {
		t.Fatalf("Query() = %v", err)
	}
	found := false
	for _, it := range items {
		if it.ID == "tool-call_x" {
			found = true
			if !strings.Contains(it.Content, "calc") {
				t.Errorf("archived content = %q, want tool mention", it.Content)
			}
		}
	}
	if !found {
		t.Errorf("tool interaction not archived to memory; items = %+v", items)
	}
}
