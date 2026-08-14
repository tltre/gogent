package provider

// ModelInfo describes the static capabilities of a provider instance.
//
// It serves as the capability declaration ("what this engine can do"), not
// the end-user preference ("which model do I pick"). The Models field lists
// the models an engine supports so the Interface layer can offer them to the
// end user at runtime.
type ModelInfo struct {
	Name         string   // default model name (may be empty; users choose at runtime)
	Provider     string   // engine name, e.g. "openai"
	DisplayName  string   // brand name visible to end users, e.g. "OpenAI"
	ContextSize  int      // reserved — deferred; see note below
	SupportsTool bool     // whether tool calling is supported
	SupportsVision bool   // whether image input is supported
	Models       []string // available models, fetched dynamically from the provider /models endpoint (never hardcoded)
}

// Deprecated: ContextSize is reserved and intentionally NOT populated by
// engine implementations. Context-window-per-model data is deferred (the
// provider /models endpoints do not expose it uniformly; see docs/provider.md
// problem "contextSize deferred"). Keep the field for interface stability;
// treat 0 as "unknown".
