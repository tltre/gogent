package contextmanager

import (
	"time"
)

type ContextMessage struct {
	Role      string
	Content   string
	Timestamp time.Time
	Extra     map[string]any
}

type Summary struct {
	Content   string
	KeyPoints []string
	Timestamp time.Time
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
	//  @param sessionId
	//  @param messages user messages for this run
	//  @return []ContextMessage complete input (system + history + user)
	//
	BuildInput(sessionId string, messages []ContextMessage) []ContextMessage
}
