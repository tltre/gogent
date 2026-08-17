package main

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/tltre/gogent/internal/otel"
	"github.com/tltre/gogent/pkg/provider"
)

// DemoProvider is a rule-based provider that returns canned responses.
// Zero external dependencies — no LLM required.
type DemoProvider struct {
	rules  []Rule
	userName string
}

// Rule matches a user input pattern and returns a response.
type Rule struct {
	Pattern string
	Reply   func(input string, p *DemoProvider) string
}

func NewDemoProvider() *DemoProvider {
	return &DemoProvider{
		rules: []Rule{
			{
				Pattern: "hello",
				Reply: func(input string, p *DemoProvider) string {
					if p.userName != "" {
						return "Hello again, " + p.userName + "! How can I help you?"
					}
					return "Hello! Welcome to Gogent. What's your name?"
				},
			},
			{
				Pattern: "name is",
				Reply: func(input string, p *DemoProvider) string {
					parts := strings.SplitN(input, "name is", 2)
					name := strings.TrimSpace(parts[len(parts)-1])
					name = strings.TrimRight(name, ".!")
					p.userName = name
					return "Nice to meet you, " + name + "! I'm the Gogent demo agent."
				},
			},
			{
				Pattern: "weather",
				Reply: func(input string, p *DemoProvider) string {
					city := extractCity(input)
					if city != "" {
						return "Today in " + city + ": ☀️ Sunny, 25°C — perfect weather!"
					}
					return "Today: ☀️ Sunny, 25°C — perfect weather!"
				},
			},
			{
				Pattern: "joke",
				Reply: func(input string, p *DemoProvider) string {
					return "Why did the trace break? Because it lost its span context! 😄"
				},
			},
			{
				Pattern: "calc",
				Reply: func(input string, p *DemoProvider) string {
					return "I'm a demo agent, not a calculator! Try asking me something else."
				},
			},
		},
	}
}

// Generate returns a response based on rule matching.
// Creates an OTel span to verify provider-level instrumentation.
func (p *DemoProvider) Generate(ctx context.Context, msgs []provider.ProviderMessage) (provider.Response, error) {
	tracer := otel.Tracer("gogent.demo")
	ctx, span := tracer.Start(ctx, "provider.generate",
		trace.WithAttributes(
			attribute.Int("msg_count", len(msgs)),
		),
	)
	defer span.End()

	// Find the last user message.
	content := ""
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			content = msgs[i].Content
			break
		}
	}

	input := strings.ToLower(content)

	// Match against rules.
	for _, rule := range p.rules {
		if strings.Contains(input, rule.Pattern) {
			result := rule.Reply(content, p)
			span.SetAttributes(
				attribute.String("rule", rule.Pattern),
				attribute.String("response.content", result),
			)
			return provider.Response{
				Content:      result,
				FinishReason: "stop",
			}, nil
		}
	}

	// Default fallback.
	fallback := fmt.Sprintf("I heard you say: %q. Try saying hello, asking about the weather, or tell me a joke!", content)
	span.SetAttributes(
		attribute.String("rule", "fallback"),
		attribute.String("response.content", fallback),
	)
	return provider.Response{
		Content:      fallback,
		FinishReason: "stop",
	}, nil
}

func (p *DemoProvider) ModelInfo() provider.ModelInfo {
	return provider.ModelInfo{
		Name:     "demo-provider",
		Provider: "gogent-e2e",
	}
}

func (p *DemoProvider) Stream(_ context.Context, _ []provider.ProviderMessage) (<-chan provider.StreamChunk, error) {
	ch := make(chan provider.StreamChunk)
	close(ch)
	return ch, nil
}

func extractCity(input string) string {
	keywords := []string{"in ", "at ", "for "}
	lower := strings.ToLower(input)
	for _, kw := range keywords {
		if idx := strings.Index(lower, kw); idx >= 0 {
			city := strings.TrimSpace(input[idx+len(kw):])
			city = strings.TrimRight(city, ".!?")
			if len(city) > 0 && len(city) < 30 {
				return city
			}
		}
	}
	return ""
}
