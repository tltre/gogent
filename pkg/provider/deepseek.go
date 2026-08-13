package provider

import (
	"os"
	"time"
	"net/http"
)

// DeepSeek REST API endpoints and defaults.
const (
	// DeepSeekBaseURL is the DeepSeek OpenAI-compatible API base URL.
	DeepSeekBaseURL = "https://api.deepseek.com/v1"

	// DeepSeekEnvAPIKey is the environment variable holding the DeepSeek API key.
	DeepSeekEnvAPIKey = "DEEPSEEK_API_KEY"

	// DeepSeekEnvModel optionally overrides the default model via environment.
	DeepSeekEnvModel = "DEEPSEEK_MODEL"

	// DefaultDeepSeekModel is used when neither configuration nor the
	// DEEPSEEK_MODEL environment variable provides a model.
	DefaultDeepSeekModel = "deepseek-chat"
)

// init registers the "deepseek" engine into the engine registry.
func init() {
	_ = RegisterEngine("deepseek", func() (IProvider, error) {
		return NewDeepSeek(DeepSeekConfig{}), nil
	})
}

// DeepSeekConfig holds construction parameters for DeepSeekProvider.
// All fields are optional — zero values fall back to defaults.
type DeepSeekConfig struct {
	APIKey     string        // default: DEEPSEEK_API_KEY env
	Model      string        // default: DEEPSEEK_MODEL env, then DefaultDeepSeekModel
	Timeout    time.Duration // default 60s
	HTTPClient *http.Client  // override transport (tests, proxy, etc.)
}

// DeepSeekProvider is an OpenAI-compatible provider for DeepSeek. It reuses
// the full OpenAI wire implementation (request conversion, SSE streaming,
// tool calls) and only differs in base URL, API key env var, and its
// capability declaration.
type DeepSeekProvider struct {
	*OpenAIProvider
	model string
}

var _ IProvider = (*DeepSeekProvider)(nil)

// NewDeepSeek creates a DeepSeekProvider from the given config.
func NewDeepSeek(cfg DeepSeekConfig) *DeepSeekProvider {
	model := cfg.Model
	if model == "" {
		model = os.Getenv(DeepSeekEnvModel)
	}
	if model == "" {
		model = DefaultDeepSeekModel
	}
	return &DeepSeekProvider{
		OpenAIProvider: NewOpenAI(OpenAIConfig{
			BaseURL:    DeepSeekBaseURL,
			APIKey:     cfg.APIKey,
			APIKeyEnv:  DeepSeekEnvAPIKey,
			Model:      model,
			Timeout:    cfg.Timeout,
			HTTPClient: cfg.HTTPClient,
		}),
		model: model,
	}
}

// ModelInfo returns the static capability declaration of this provider.
func (p *DeepSeekProvider) ModelInfo() ModelInfo {
	return ModelInfo{
		Name:           p.model,
		Provider:       "deepseek",
		DisplayName:    "DeepSeek",
		ContextSize:    65536,
		SupportsTool:   true,
		SupportsVision: false,
		Models: []string{
			"deepseek-chat", "deepseek-reasoner",
		},
	}
}
