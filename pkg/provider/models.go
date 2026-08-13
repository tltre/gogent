package provider

// ModelInfo describes the static capabilities of a provider instance.
//
// It serves as the capability declaration ("what this engine can do"), not
// the end-user preference ("which model do I pick"). The Models field lists
// the models an engine supports so the Interface layer can offer them to the
// end user at runtime.
type ModelInfo struct {
	Name           string   // default model name (may be empty; users choose at runtime)
	Provider       string   // engine name, e.g. "openai"
	DisplayName    string   // brand name visible to end users, e.g. "OpenAI"
	ContextSize    int      // context window size in tokens
	SupportsTool   bool     // whether tool calling is supported
	SupportsVision bool     // whether image input is supported
	Models         []string // available models (capability declaration)
}
