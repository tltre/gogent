package contextmanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/tltre/gogent/pkg/provider"
)

// LLMSummarizer is the default Summarizer: it compacts history with an LLM
// call through the ProviderManager. It merges the previous summary with the
// conversation since the last checkpoint (mirroring opencode v2's compaction
// prompt), producing a structured generic summary.
//
// The summary template is intentionally generic (not coding-agent specific):
// it captures topic threads, key facts, decisions, user preferences, pending
// work, and latest state — useful for any conversational agent.
type LLMSummarizer struct {
	cm *DefaultContextManager
}

// NewLLMSummarizer creates an LLM-backed summarizer bound to the manager
// (used to resolve the ProviderManager from the registry).
func NewLLMSummarizer(cm *DefaultContextManager) *LLMSummarizer {
	return &LLMSummarizer{cm: cm}
}

const genericSummaryTemplate = `Output exactly the Markdown structure shown inside <template> and keep the section order unchanged. Do not include the <template> tags in your response.
<template>
## Topic
- [one or two brief sentences describing the overall topic(s) of the conversation]

## Key Facts
- [important facts, decisions, constraints, and context needed to continue; otherwise "(none)"]

## User Preferences & Directives
- [explicit user preferences, instructions, or constraints; otherwise "(none)"]

## Pending Work
- [unfinished tasks or open questions; otherwise "(none)"]

## Latest State
- [the most recent state of the conversation; otherwise "(none)"]
</template>

Rules:
- Keep every section, even when empty.
- Use terse bullets, not prose paragraphs.
- Preserve exact identifiers, names, numbers, and specific details when known.
- Do not mention the summary process or that context was compacted.`

const genericSummaryUpdateInstructions = `The <prior-summary> summarizes everything that happened before the <conversation>. Construct a new summary that combines both. The <prior-summary> is discarded after this: anything you do not carry into the new summary is lost.

When combining:
- Carry forward objectives, user preferences, decisions, and pending work from the <prior-summary> even when the <conversation> does not mention them. Drop only what is finished and no longer needed.
- The <conversation> is more recent than the <prior-summary>. Where they conflict, the conversation wins: state the corrected fact and drop the old claim.
- Add new facts, decisions, and context from the conversation.
- Move completed work out of "Pending Work".
- Update "Latest State" and "Topic" to reflect the current conversation.`

// buildCompactionPrompt assembles the summarizer input: the new conversation
// (optionally with a preserved verbatim tail) plus the prior summary merge
// instructions when one exists.
func buildCompactionPrompt(newMsgs []ContextMessage, previousSummary *Summary) string {
	var conversation strings.Builder
	for _, m := range newMsgs {
		fmt.Fprintf(&conversation, "[%s]: %s\n", m.Role, m.Content)
	}
	conv := strings.TrimRight(conversation.String(), "\n")

	var b strings.Builder
	if conv != "" {
		b.WriteString("Here is the conversation so far:\n\n<conversation>\n")
		b.WriteString(conv)
		b.WriteString("\n</conversation>")
	}
	if previousSummary != nil && previousSummary.Content != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("Here is the summary of the conversation before the <conversation> above:\n\n<prior-summary>\n")
		b.WriteString(previousSummary.Content)
		b.WriteString("\n</prior-summary>")
	}
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	if previousSummary != nil && previousSummary.Content != "" {
		b.WriteString(genericSummaryUpdateInstructions)
	} else {
		b.WriteString("Create a new summary from the conversation history above so that a conversation can continue coherently.")
	}
	b.WriteString("\n\n")
	b.WriteString(genericSummaryTemplate)
	return b.String()
}

// Summarize implements Summarizer.
func (s *LLMSummarizer) Summarize(ctx context.Context, msgs []ContextMessage, previousSummary *Summary, model string) (Summary, error) {
	pm := s.cm.providerManager()
	if pm == nil {
		return Summary{}, fmt.Errorf("summarizer: no ProviderManager available")
	}
	prompt := buildCompactionPrompt(msgs, previousSummary)
	if strings.TrimSpace(prompt) == "" {
		return Summary{}, fmt.Errorf("summarizer: empty compaction input")
	}

	// Use the context's provider; force the model if one was selected for
	// compaction, else leave the ambient model (set by the caller).
	callCtx := ctx
	if model != "" {
		callCtx = provider.WithModel(callCtx, model)
	}

	resp, err := pm.Generate(callCtx, []provider.ProviderMessage{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return Summary{}, fmt.Errorf("summarizer: generate: %w", err)
	}
	if strings.TrimSpace(resp.Content) == "" {
		return Summary{}, fmt.Errorf("summarizer: empty summary from model")
	}
	return Summary{
		Content: strings.TrimSpace(resp.Content),
	}, nil
}