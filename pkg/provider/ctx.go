package provider

import "context"

// Context keys for per-request provider/model selection (v0.14.7).
//
// The Interface layer collects the user's selection into agentcore.Input;
// the AgentCore writes them into the context; ProviderManager.Generate/Stream
// read the provider name to dispatch, and engine implementations read the
// model to override their default.

type providerNameCtxKey struct{}

type modelNameCtxKey struct{}

// WithProviderName returns a context carrying the provider engine name
// ("openai", "deepseek", ...) to use for the request.
func WithProviderName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, providerNameCtxKey{}, name)
}

// ProviderNameFrom returns the provider engine name carried in the context,
// or "" if none was set.
func ProviderNameFrom(ctx context.Context) string {
	if v, ok := ctx.Value(providerNameCtxKey{}).(string); ok {
		return v
	}
	return ""
}

// WithModel returns a context carrying the model name to use for the request.
func WithModel(ctx context.Context, model string) context.Context {
	return context.WithValue(ctx, modelNameCtxKey{}, model)
}

// ModelFrom returns the model name carried in the context, or "" if none was
// set (engine falls back to its default model).
func ModelFrom(ctx context.Context) string {
	if v, ok := ctx.Value(modelNameCtxKey{}).(string); ok {
		return v
	}
	return ""
}
