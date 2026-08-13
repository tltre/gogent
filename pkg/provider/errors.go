package provider

import (
	"errors"
	"fmt"
)

// Sentinel errors for the provider package.
var (
	// ErrUnknownEngine is returned when CreateEngine is called with an engine
	// name that is not registered in the engine registry.
	ErrUnknownEngine = errors.New("unknown provider engine")

	// ErrEngineAlreadyRegistered is returned when RegisterEngine is called with
	// an engine name that is already registered.
	ErrEngineAlreadyRegistered = errors.New("engine already registered")

	// ErrProviderNotFound is returned when a provider instance is not found
	// in the ProviderManager.
	ErrProviderNotFound = errors.New("provider not found")

	// ErrProviderAlreadyExists is returned when registering a provider name
	// that is already present in the ProviderManager.
	ErrProviderAlreadyExists = errors.New("provider already exists")

	// ErrAPIKeyMissing is returned when an engine requires an API key but none
	// is available (neither configured nor resolvable from the environment).
	ErrAPIKeyMissing = errors.New("api key missing")
)

// ProviderError is a structured error returned by provider engine
// implementations when the upstream API rejects a request. It carries the
// HTTP status code, the provider's original error type, and whether the
// failure is transient (retryable).
type ProviderError struct {
	Code      int    // HTTP status code
	Type      string // provider-specific error type (e.g. "insufficient_quota")
	Message   string // human-readable message from the provider
	Retryable bool   // true for transient failures (429, 5xx)
}

func (e *ProviderError) Error() string {
	if e.Type != "" {
		return fmt.Sprintf("provider error %d (%s): %s", e.Code, e.Type, e.Message)
	}
	return fmt.Sprintf("provider error %d: %s", e.Code, e.Message)
}

// IsRetryable reports whether an error represents a transient provider
// failure suitable for retry. It unwraps *ProviderError; other errors are
// considered non-retryable.
func IsRetryable(err error) bool {
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe.Retryable
	}
	return false
}
