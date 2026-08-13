package provider

import "errors"

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
)
