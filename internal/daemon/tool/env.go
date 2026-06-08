package tool

import (
	"github.com/tltre/gogent/internal/credentials"
)

// EnvResolver is a backward-compatible alias for credentials.Resolver.
type EnvResolver = credentials.Resolver

// NewEnvResolver creates a credentials resolver.
func NewEnvResolver(creds map[string]string) *EnvResolver {
	return credentials.NewResolver(creds)
}

// IsEnvVarRef checks if a value is already a ${...} reference.
func IsEnvVarRef(val string) bool {
	return credentials.IsEnvVarRef(val)
}

// EnvVarRef creates a ${tool-name.KEY} reference string.
func EnvVarRef(toolName, key string) string {
	return credentials.EnvVarRef(toolName, key)
}
