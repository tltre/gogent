package contextmanager

import "unicode"

// Token estimation without a tokenizer dependency.
//
// Gogent has no tokenizer (adding tiktoken-style deps is out of scope), so we
// approximate token counts with a CJK-aware heuristic that errs on the side of
// overestimating (conservative for context-window decisions). Real token counts
// from provider usage are the future calibration source (ContextMessage.Tokens).
//
// Heuristic: Han characters ≈ 1 token each, all other characters ≈ 4 per token,
// plus a small per-message overhead.

// charsPerToken is the average ASCII/other-character-to-token ratio.
const charsPerToken = 4

// messageOverheadTokens models per-message framing cost (role labels, delimiters).
const messageOverheadTokens = 4

// EstimateTokens returns a conservative estimate of the token count of text.
func EstimateTokens(text string) int {
	cjk, other := 0, 0
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			cjk++
		} else {
			other++
		}
	}
	tokens := cjk + (other+charsPerToken-1)/charsPerToken // ceil for other
	if tokens == 0 {
		return 0
	}
	return tokens
}

// estimateMessages returns the estimated tokens for a slice of messages,
// adding per-message overhead. A message with empty content still costs the
// overhead (a role line is present).
func estimateMessages(msgs []ContextMessage) int {
	total := 0
	for _, m := range msgs {
		total += messageOverheadTokens + EstimateTokens(m.Content)
	}
	return total
}

// estimateTextTokens is a convenience for estimating a single rendered string
// (e.g. the assembled system prompt) with no per-message overhead.
func estimateTextTokens(text string) int {
	return EstimateTokens(text)
}