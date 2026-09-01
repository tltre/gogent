package contextmanager

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/provider"
)

// fakeSummarizer returns a canned summary and records its input.
type fakeSummarizer struct {
	mu       sync.Mutex
	calls    int
	lastMsgs []ContextMessage
	prior    *Summary
	block    chan struct{} // when non-nil, Summarize blocks until closed
	result   string
	err      error
}

func (f *fakeSummarizer) Summarize(_ context.Context, msgs []ContextMessage, prior *Summary, _ string) (Summary, error) {
	f.mu.Lock()
	f.calls++
	f.lastMsgs = msgs
	f.prior = prior
	block := f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	if f.err != nil {
		return Summary{}, f.err
	}
	return Summary{Content: f.result}, nil
}

// newCompressionCM builds a DefaultContextManager wired with a ProviderManager
// (specs catalog) and a fake summarizer, so BuildInput triggers compression.
// The test catalog gives context=200 → usable=100 (reserved resolves to 100).
func newCompressionCM(t *testing.T, summarize *fakeSummarizer) (*DefaultContextManager, *provider.ProviderManager) {
	t.Helper()
	cm := NewDefaultContextManager()
	if summarize != nil {
		cm.SetSummarizer(summarize)
	}
	pm := provider.NewManagerComponent("pm")
	specs := provider.NewModelSpecs()
	specs.Configure("http://127.0.0.1:1", t.TempDir()+"/m.json") // unreachable; populated manually
	specs.SetProvidersForTest(map[string]provider.TestLimit{
		"test-model": {Context: 200, Output: 100},
	})
	pm.SetSpecsForTest(specs)

	reg := component.NewRegistry()
	if err := reg.Register(pm); err != nil {
		t.Fatalf("register pm: %v", err)
	}
	reg.SetDefault(component.ComponentProvider, pm.GetName())
	cm.SetRegistry(reg)
	return cm, pm
}

func withModel(ctx context.Context, model string) context.Context {
	return provider.WithModel(provider.WithProviderName(ctx, "openai"), model)
}

// smallCfg returns a config where usable=100, hard=20, soft=10 so any
// realistic input exceeds hard → sync compaction.
func smallCfg() CompressionConfig {
	cfg := DefaultCompressionConfig()
	cfg.ContextSizeOverride = 200 // usable = 200 - reserved(100) = 100
	cfg.Reserved = 0
	cfg.MinTokens = 1
	cfg.SoftRatio = 0.1 // soft = 10
	cfg.HardRatio = 0.2 // hard = 20
	return cfg
}

func TestCompressionViewAndSummary(t *testing.T) {
	sum := &fakeSummarizer{result: "MERGED"}
	cm, _ := newCompressionCM(t, sum)

	sid := cm.NewSession()
	_ = cm.AddMessage(sid, ContextMessage{Role: "user", Content: "hello"})
	_ = cm.AddMessage(sid, ContextMessage{Role: "assistant", Content: "hi"})
	_ = cm.AddMessage(sid, ContextMessage{Role: "user", Content: "second turn"})

	ctx := withModel(context.Background(), "test-model")
	cm.SetCompressionConfig(smallCfg())

	// est > hard → sync compaction (result "MERGED").
	_ = cm.BuildInput(ctx, sid, []ContextMessage{{Role: "user", Content: "q3"}})

	view := cm.visibleMessages(sid)
	if len(view) == 0 {
		t.Fatal("empty view after compaction")
	}
	if view[0].Role != "user" || !strings.Contains(view[0].Content, "MERGED") {
		t.Errorf("view[0] = %+v, want summary user message", view[0])
	}
	// Physical history is never deleted.
	if n := len(cm.GetMessages(sid)); n != 3 {
		t.Errorf("physical messages = %d, want 3 (no deletion)", n)
	}
	// Messages hidden behind the summary are not in the view.
	for _, m := range view[1:] {
		if m.Content == "hello" || m.Content == "hi" {
			t.Errorf("hidden message still in view: %+v", m)
		}
	}

	// Second compaction must merge prior summary into the new conversation.
	_ = cm.BuildInput(ctx, sid, []ContextMessage{{Role: "user", Content: "q4"}})
	sum.mu.Lock()
	prior := sum.prior
	sum.mu.Unlock()
	if prior == nil || prior.Content != "MERGED" {
		t.Errorf("prior summary not passed to second compaction: %+v", prior)
	}
}

func TestCompressionSoftDoesNotBlock(t *testing.T) {
	sum := &fakeSummarizer{result: "S", block: make(chan struct{})}
	cm, _ := newCompressionCM(t, sum)
	sid := cm.NewSession()
	_ = cm.AddMessage(sid, ContextMessage{Role: "user", Content: strings.Repeat("padding ", 40)})

	// est (~200 tokens) > hard normally. To exercise the SOFT async path we
	// keep est below hard but above soft using a very high hard ratio.
	cfg := smallCfg()
	cfg.HardRatio = 0.99 // hard = 99
	cfg.SoftRatio = 0.05 // soft = 5
	cm.SetCompressionConfig(cfg)

	ctx := withModel(context.Background(), "test-model")
	done := make(chan struct{})
	go func() {
		_ = cm.BuildInput(ctx, sid, []ContextMessage{{Role: "user", Content: "x"}})
		close(done)
	}()
	select {
	case <-done:
		// returned without blocking on the blocked async summarizer
	case <-time.After(500 * time.Millisecond):
		t.Fatal("soft-trigger BuildInput blocked on async compaction")
	}
	close(sum.block)
	cm.waitCompaction(sid)
}

func TestCompressionHardBlocksSync(t *testing.T) {
	sum := &fakeSummarizer{result: "MERGED"}
	cm, _ := newCompressionCM(t, sum)
	sid := cm.NewSession()
	_ = cm.AddMessage(sid, ContextMessage{Role: "user", Content: strings.Repeat("content ", 60)})
	cm.SetCompressionConfig(smallCfg()) // hard=20, est>>hard

	ctx := withModel(context.Background(), "test-model")
	_ = cm.BuildInput(ctx, sid, []ContextMessage{{Role: "user", Content: "q"}})
	sum.mu.Lock()
	calls := sum.calls
	sum.mu.Unlock()
	if calls == 0 {
		t.Fatal("expected sync compaction on hard threshold")
	}
}

func TestCompressionCircuitBreaker(t *testing.T) {
	sum := &fakeSummarizer{result: ""} // empty result → treated as failure
	cm, _ := newCompressionCM(t, sum)
	sid := cm.NewSession()
	_ = cm.AddMessage(sid, ContextMessage{Role: "user", Content: strings.Repeat("content ", 60)})
	cfg := smallCfg()
	cfg.MaxFailures = 2
	cm.SetCompressionConfig(cfg)

	ctx := withModel(context.Background(), "test-model")
	for i := 0; i < 3; i++ {
		_ = cm.BuildInput(ctx, sid, []ContextMessage{{Role: "user", Content: "q"}})
	}
	sum.mu.Lock()
	calls := sum.calls
	sum.mu.Unlock()
	if calls != 2 {
		t.Errorf("Summarize calls = %d, want 2 (circuit open after 2 failures)", calls)
	}
}

func TestSelectTail(t *testing.T) {
	// Five ~1000-token messages (4000 ASCII chars each); budget floors at
	// tailBudgetMin=2000 → ~2 messages fit in the preserved tail.
	big := strings.Repeat("x", 4000)
	msgs := []ContextMessage{
		{Role: "user", Content: big},
		{Role: "user", Content: big},
		{Role: "user", Content: big},
		{Role: "user", Content: big},
		{Role: "user", Content: big},
	}
	head, start := selectTail(msgs, 1000)
	// budget = max(2000, 250) = 2000 tokens; each message ≈1004 tokens, so one
	// message fits the preserved tail → head = msgs[:4], start = 4.
	if start != 4 {
		t.Errorf("selectTail start = %d, want 4 (1 message preserved in tail)", start)
	}
	if len(head) != start {
		t.Errorf("head len = %d, want == start %d", len(head), start)
	}
	// Tiny usable yields the same floored budget.
	head2, start2 := selectTail(msgs, 1)
	if start2 != 4 || len(head2) != start2 {
		t.Errorf("tiny usable selectTail = head %d start %d, want start 4", len(head2), start2)
	}
}
