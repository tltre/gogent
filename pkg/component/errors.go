package component

import "errors"

var (
	ErrComponentNotFound      = errors.New("component not found")
	ErrComponentAlreadyExists = errors.New("component already exists")
	ErrDependencyNotMet       = errors.New("dependency not met")
	ErrCircularDependency     = errors.New("circular dependency detected")
	ErrNotInitialized         = errors.New("component not initialized")
	ErrAlreadyInitialized     = errors.New("component already initialized")
)
