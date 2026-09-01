package contextmanager

import (
	"context"
	"time"
)

type ContextMessage struct {
	// ID is the session-local monotonically increasing message sequence
	// (assigned by DefaultContextManager.AddMessage). It is the stable
	// identifier used for compression checkpoints and view boundaries —
	// immune to future physical cleanup that would invalidate array indices.
	ID        int64
	Role      string
	Content   string
	Timestamp time.Time
	Extra     map[string]any

	// Tokens is the real token count of this message when known (from provider
	// usage). Reserved for future calibration; 0 means "estimate instead".
	Tokens int
}

type Summary struct {
	Content   string
	KeyPoints []string
	Timestamp time.Time
}

// CompressionConfig controls the context-window compression behavior of
// DefaultContextManager.
type CompressionConfig struct {
	// Enabled gates compression entirely. Default true.
	Enabled bool
	// SoftRatio: when estimated input exceeds usable×SoftRatio and no
	// compaction is running, start an async compaction (does not block).
	SoftRatio float64
	// HardRatio: when estimated input exceeds usable×HardRatio, block until a
	// running compaction completes, or start a synchronous one.
	HardRatio float64
	// MinTokens: absolute lower bound on estimated tokens before compression
	// triggers (avoids churning on short sessions).
	MinTokens int
	// Reserved: tokens reserved for model output (subtracted from the context
	// window to compute usable). Default min(20000, maxOutput).
	Reserved int
	// Model: summarizer model; empty = follow the session's current model.
	Model string
	// MaxFailures: consecutive compaction failures before the circuit breaker
	// disables compression for a session. Default 3.
	MaxFailures int
	// MaxCompactionPasses bounds the synchronous cascade loop in enforceLimit.
	MaxCompactionPasses int
}

// DefaultCompressionConfig returns the framework defaults.
func DefaultCompressionConfig() CompressionConfig {
	return CompressionConfig{
		Enabled:             true,
		SoftRatio:           0.7,
		HardRatio:           0.9,
		MinTokens:           4000,
		Reserved:            0, // resolved dynamically: min(20000, maxOutput)
		Model:               "",
		MaxFailures:         3,
		MaxCompactionPasses: 3,
	}
}

// Summarizer produces a summary of history for compaction. Implementations
// may be LLM-backed (default) or extractive/local. It is a pluggable extension
// point (like the opencode session.compacting plugin hook).
type Summarizer interface {
	// Summarize merges the conversation since the last checkpoint (msgs) with
	// the previous summary into a new Summary. model selects the summarizer
	// model ("" = follow the session's current model). previousSummary may be
	// nil for the first compaction.
	Summarize(ctx context.Context, msgs []ContextMessage, previousSummary *Summary, model string) (Summary, error)
}

type IContextManager interface {
	//
	// NewSession
	//  @Description: create a new session for current chat
	//	@return string sessionID
	//
	NewSession() string
	//
	// AddMessage
	//  @Description: add a new context message to specific session
	//  @param sessionId
	//  @param msg
	//  @return error
	//
	AddMessage(sessionId string, msg ContextMessage) error

	//
	// GetMessages
	//  @Description: get history context messages for specific session
	//  @param sessionId
	//  @return []ContextMessage
	//
	GetMessages(sessionId string) []ContextMessage

	//
	// GetSummary
	//  @Description: get summary for specific session
	//  @param sessionId
	//  @return Summary
	//  @return error
	//
	GetSummary(sessionId string) (Summary, error)

	//
	// BuildSystemPrompt
	//  @Description: construct system prompt by history messages and summary for agent
	//  @param sessionId
	//  @return string system prompt
	//
	BuildSystemPrompt(sessionId string) string

	//
	// Clear
	//  @Description: clear the context history for specific session
	//  @param sessionId
	//  @return error
	//
	Clear(sessionId string) error

	//
	// DeleteSession
	//  @Description: completely delete the session
	//  @param sessionID
	//
	DeleteSession(sessionID string)

	//
	// ListSessions
	//  @Description: list all active session IDs
	//  @return []string session IDs
	//
	ListSessions() []string

	//
	// BuildInput
	//  @Description: assemble the complete model input for an agent run:
	//  system prompt (with recalled memory) + session history + user messages.
	//  The agent treats ContextManager as its single input source.
	//  ctx carries the active provider/model (for context-window-aware history
	//  compression) and cancellation.
	//  @param ctx
	//  @param sessionId
	//  @param messages user messages for this run
	//  @return []ContextMessage complete input (system + history + user)
	//
	BuildInput(ctx context.Context, sessionId string, messages []ContextMessage) []ContextMessage
}
