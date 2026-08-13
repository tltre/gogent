package provider

// NewOpenAICompat creates an OpenAIProvider pointed at any OpenAI-compatible
// endpoint (DeepSeek, Groq, Mistral, Ollama, vLLM, ...). The implementation is
// identical to OpenAI's — only the base URL differs.
//
// This is an internal base for engine implementations, not a registered
// engine itself: specific compatible vendors are registered as their own
// engines (each with a built-in base URL) so the blacklist mechanism can
// enable/disable them independently.
func NewOpenAICompat(baseURL string) *OpenAIProvider {
	return NewOpenAI(OpenAIConfig{BaseURL: baseURL})
}
