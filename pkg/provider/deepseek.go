package provider

import (
	"context"
	"net/http"
	"os"
	"time"
)

// DeepSeek REST API endpoints and defaults.
const (
	// DeepSeekBaseURL is the DeepSeek OpenAI-compatible API base URL.
	DeepSeekBaseURL = "https://api.deepseek.com/v1"

	// DeepSeekEnvAPIKey is the environment variable holding the DeepSeek API key.
	DeepSeekEnvAPIKey = "DEEPSEEK_API_KEY"

	// DeepSeekEnvModel optionally overrides the default model via environment.
	DeepSeekEnvModel = "DEEPSEEK_MODEL"

	// DeepSeekEnvBaseURL overrides the API base URL via environment (proxy /
	// gateway / mock server in tests).
	DeepSeekEnvBaseURL = "DEEPSEEK_BASE_URL"
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
	Model      string        // default: DEEPSEEK_MODEL env, then the first model from /models (never hardcoded)
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
	// No hardcoded fallback: empty model resolves lazily to the first model
	// from /models (see OpenAIProvider.resolveDefaultModel).
	baseURL := DeepSeekBaseURL
	if env := os.Getenv(DeepSeekEnvBaseURL); env != "" {
		baseURL = env
	}
	p := &DeepSeekProvider{
		OpenAIProvider: NewOpenAI(OpenAIConfig{
			BaseURL:    baseURL,
			APIKey:     cfg.APIKey,
			APIKeyEnv:  DeepSeekEnvAPIKey,
			Model:      model,
			Timeout:    cfg.Timeout,
			HTTPClient: cfg.HTTPClient,
		}),
		model: model,
	}
	p.providerName = "deepseek"
	return p
}

// ModelInfo returns the capability declaration of this provider. The model
// list and the default model are fetched dynamically from the DeepSeek
// /models endpoint via the shared OpenAI implementation (never hardcoded).
// ContextSize is resolved from the centralized model catalog; 0 when unknown.
func (p *DeepSeekProvider) ModelInfo() ModelInfo {
	return ModelInfo{
		Name:           p.OpenAIProvider.resolveDefaultModel(context.Background()),
		Provider:       "deepseek",
		DisplayName:    "DeepSeek",
		SupportsTool:   true,
		SupportsVision: false,
		Models:         p.OpenAIProvider.fetchModels(context.Background()),
		ContextSize:    p.OpenAIProvider.catalogContextSize("deepseek"),
	}
}
