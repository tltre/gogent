package contextmanager

import (
	"context"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/provider"
)

// This file implements the compression-summary engine: context-window limit
// enforcement (soft async / hard blocking) triggered from BuildInput, and the
// single-flight async compaction task with a failure circuit breaker.
//
// Reference behavior mirrors opencode v2's SessionCompaction: compression
// input is the previous summary plus the conversation since the last
// checkpoint; the output replaces the old summary; a tail of recent messages
// is preserved verbatim within a token budget.

const (
	// defaultCompactionBuffer reserves tokens for model output when the
	// provider reports no max-output limit (mirrors opencode's 20k buffer).
	defaultCompactionBuffer = 20000

	// tailBudgetMin/Max bound the token budget preserved for the verbatim tail
	// (mirrors opencode's MIN/MAX_PRESERVE_RECENT_TOKENS).
	tailBudgetMin = 2000
	tailBudgetMax = 15000
	// tailBudgetRatio scales the preserved-tail budget from the usable window.
	tailBudgetRatio = 0.25
)

// modelFromContext extracts the active model from the context. BuildInput is
// invoked with the provider/model injected by the agent (provider.WithModel).
func modelFromContext(ctx context.Context) string {
	return provider.ModelFrom(ctx)
}

// compressionCfg returns the effective config for a session (override or
// manager default).
func (c *DefaultContextManager) compressionCfg(s *sessionState) CompressionConfig {
	if s != nil && s.cfg != nil {
		return *s.cfg
	}
	return c.cfg
}

// providerManager resolves the ProviderManager through the registry for
// context-window lookups and the LLM summarizer.
func (c *DefaultContextManager) providerManager() *provider.ProviderManager {
	if c.Registry() == nil {
		return nil
	}
	comp := c.Registry().GetDefault(component.ComponentProvider)
	if comp == nil {
		return nil
	}
	if pm, ok := comp.(*provider.ProviderManager); ok {
		return pm
	}
	return nil
}

// usable returns the context window available for input for a session's model:
// context minus reserved output tokens. Returns 0 when the window is unknown
// (catalog unavailable) or compression is disabled — callers must treat 0 as
// "do not compress".
func (c *DefaultContextManager) usable(ctx context.Context, s *sessionState, cfg CompressionConfig) int {
	pm := c.providerManager()
	if pm == nil {
		return 0
	}
	model := s.lastModel
	if model == "" {
		model = modelFromContext(ctx)
	}
	limits := pm.ModelLimitsFor(provider.ProviderNameFrom(ctx), model)
	if limits.ContextSize <= 0 {
		return 0
	}
	reserved := cfg.Reserved
	if reserved <= 0 {
		maxOut := limits.MaxOutput
		if maxOut <= 0 {
			maxOut = defaultCompactionBuffer
		}
		if maxOut > defaultCompactionBuffer {
			maxOut = defaultCompactionBuffer
		}
		reserved = maxOut
	}
	if usable := limits.ContextSize - reserved; usable > 0 {
		return usable
	}
	return 0
}

// enforceLimit ensures the assembled input fits the model context window,
// triggering compression when thresholds are crossed.
//
//   - estimated <= soft → nothing
//   - soft < estimated <= hard, not compressing → start async compaction
//   - estimated > hard → block until a running compaction completes, or start a
//     synchronous one; cascade (bounded) if the session is still over after a
//     compaction completes (e.g. a huge message arrived mid-compaction).
func (c *DefaultContextManager) enforceLimit(ctx context.Context, sessionId string, userMsgs []ContextMessage) {
	c.mu.Lock()
	s := c.sessions[sessionId]
	if s == nil {
		c.mu.Unlock()
		return
	}
	cfg := c.compressionCfg(s)
	if !cfg.Enabled || cfg.MinTokens <= 0 {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	pm := c.providerManager()
	if pm == nil {
		return
	}

	// snapshot-based estimate: system prompt + visible history + user messages.
	for pass := 0; pass < cfg.MaxCompactionPasses; pass++ {
		usable := c.usable(ctx, s, cfg)
		if usable <= 0 {
			return // unknown window → never compress
		}
		est := c.estimateInput(sessionId, userMsgs)
		if est < cfg.MinTokens {
			return
		}
		hard := int(float64(usable) * cfg.HardRatio)
		soft := int(float64(usable) * cfg.SoftRatio)

		if est <= hard {
			if est > soft && !c.isCompressing(sessionId) {
				c.launchCompaction(ctx, sessionId, false) // async pre-trigger
			}
			return
		}

		// est > hard: ensure a compaction runs and wait for it.
		if c.isCompressing(sessionId) {
			c.waitCompaction(sessionId)
			continue // re-estimate after commit
		}
		c.launchCompaction(ctx, sessionId, true) // sync
		// fall through to re-estimate
	}
}

// estimateInput estimates the token count of the assembled input (system
// prompt + visible history + user messages).
func (c *DefaultContextManager) estimateInput(sessionId string, userMsgs []ContextMessage) int {
	sys := c.BuildSystemPrompt(sessionId)
	total := estimateTextTokens(sys)
	total += estimateMessages(c.visibleMessages(sessionId))
	total += estimateMessages(userMsgs)
	return total
}

// isCompressing reports whether a compaction is in flight for the session.
func (c *DefaultContextManager) isCompressing(sessionId string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s := c.sessions[sessionId]
	return s != nil && s.compressing
}

// waitCompaction blocks until the in-flight compaction for the session
// commits (its done channel closes).
func (c *DefaultContextManager) waitCompaction(sessionId string) {
	c.mu.RLock()
	s := c.sessions[sessionId]
	done := s.done
	c.mu.RUnlock()
	if done != nil {
		<-done
	}
}

// launchCompaction starts (or joins) a compaction for the session.
//
// sync=true blocks until the compaction commits; sync=false returns
// immediately (async). Single-flight: if a compaction is already running, a
// sync caller waits on it; an async caller returns without starting a second.
func (c *DefaultContextManager) launchCompaction(ctx context.Context, sessionId string, sync bool) {
	c.mu.Lock()
	s := c.sessions[sessionId]
	if s == nil {
		c.mu.Unlock()
		return
	}
	if s.compressing {
		done := s.done
		c.mu.Unlock()
		if sync {
			<-done
		}
		return
	}
	cfg := c.compressionCfg(s)
	if s.failures >= cfg.MaxFailures {
		c.mu.Unlock()
		return // circuit open: skip this round
	}

	// Set up compaction state: checkpoint = current message ID; snapshot the
	// new conversation since the last checkpoint (viewStart).
	s.compressing = true
	s.checkpoint = s.seq
	done := make(chan struct{})
	s.done = done
	start := s.viewStart
	prior := s.summary
	model := cfg.Model
	if model == "" {
		model = s.lastModel
	}
	// Copy the messages in [viewStart, checkpoint) — the new conversation
	// since the last compaction. Held under write lock; compaction consumer
	// never mutates them.
	newMsgs := make([]ContextMessage, 0)
	for _, m := range s.messages {
		if m.ID >= start && m.ID < s.checkpoint {
			newMsgs = append(newMsgs, m)
		}
	}
	c.mu.Unlock()

	// Compaction snapshot is complete; release the lock before any LLM call.
	completed := func(summary *Summary) {
		c.mu.Lock()
		defer c.mu.Unlock()
		defer close(done)
		cur := c.sessions[sessionId]
		if cur == nil || cur != s {
			return // session cleared/deleted mid-compaction
		}
		if summary != nil {
			cur.summary = summary
			cur.viewStart = cur.checkpoint
			cur.failures = 0
		} else {
			cur.failures++
		}
		cur.compressing = false
	}

	summarizer := c.summarizer
	if summarizer == nil {
		summarizer = c.resolveSummarizer()
	}

	if summarizer == nil {
		// No summarizer available (no provider manager). Fail fast.
		completed(nil)
		return
	}

	go func() {
		var summary *Summary
		if sm, err := summarizer.Summarize(ctx, newMsgs, prior, model); err == nil && sm.Content != "" {
			summary = &sm
		}
		completed(summary)
	}()

	if sync {
		<-done
	}
}

// resolveSummarizer returns the configured LLM summarizer, resolving it from
// the registry on first use. A user-injected summarizer (c.summarizer) takes
// priority.
func (c *DefaultContextManager) resolveSummarizer() Summarizer {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.summarizer != nil {
		return c.summarizer
	}
	if c.Registry() == nil {
		return nil
	}
	comp := c.Registry().GetDefault(component.ComponentProvider)
	if comp == nil {
		return nil
	}
	if _, ok := comp.(*provider.ProviderManager); !ok {
		return nil
	}
	c.summarizer = NewLLMSummarizer(c)
	return c.summarizer
}

// SetSummarizer injects a custom Summarizer (extension point).
func (c *DefaultContextManager) SetSummarizer(s Summarizer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.summarizer = s
}

// selectTail picks the tail of messages to preserve verbatim within a token
// budget, returning the index (into msgs) where the tail begins and the head
// to summarize. Mirrors opencode's select(): walk backwards accumulating
// estimated tokens until the budget is exhausted.
func selectTail(msgs []ContextMessage, usable int) (head []ContextMessage, tailStart int) {
	if usable <= 0 || len(msgs) == 0 {
		return msgs, len(msgs)
	}
	budget := tailBudgetMin
	if b := int(float64(usable) * tailBudgetRatio); b > budget {
		budget = b
	}
	if budget > tailBudgetMax {
		budget = tailBudgetMax
	}

	total := 0
	split := len(msgs) // tail starts at split; split==len → no tail
	for i := len(msgs) - 1; i >= 0; i-- {
		next := total + estimateMessages(msgs[i : i+1])
		if next > budget {
			break
		}
		total = next
		split = i
	}
	return msgs[:split], split
}