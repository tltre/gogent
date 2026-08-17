package main

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/tltre/gogent/internal/otel"
	"github.com/tltre/gogent/pkg/agentcore"
	"github.com/tltre/gogent/pkg/logger"
	"github.com/tltre/gogent/pkg/provider"
)

// DemoAgent is a minimal IAgentCore that preserves multi-turn context
// and creates OTel spans to verify the framework's instrumentation pipeline.
type DemoAgent struct {
	provider  provider.IProvider
	runtime   *agentcore.AgentRuntime
	messages  []agentcore.Message // multi-turn conversation history
}

func NewDemoAgent(prov provider.IProvider) *DemoAgent {
	return &DemoAgent{provider: prov}
}

func (a *DemoAgent) Run(ctx context.Context, input agentcore.Input) (agentcore.Output, error) {
	tracer := otel.Tracer("gogent.demo")
	ctx, span := tracer.Start(ctx, "agent.run",
		trace.WithAttributes(
			attribute.Int("msg_count", len(input.Messages)),
			attribute.String("last_msg", lastContent(input.Messages)),
		),
	)
	defer span.End()

	// 1. Append new messages to conversation history.
	a.messages = append(a.messages, input.Messages...)

	// 2. Build provider messages from history.
	msgs := make([]provider.ProviderMessage, len(a.messages))
	for i, m := range a.messages {
		msgs[i] = provider.ProviderMessage{Role: m.Role, Content: m.Content}
	}

	// 3. Log with trace context (exercises the Logger TraceID bridge).
	logger.NewDefault().Log(ctx, logger.LogEntry{
		Level:   logger.InfoLevel,
		Module:  "e2edemo",
		Message: "agent calling provider.Generate",
		Fields:  []logger.Field{{Key: "msg_count", Value: len(msgs)}},
	})

	resp, err := a.provider.Generate(ctx, msgs)
	if err != nil {
		span.RecordError(err)
		return agentcore.Output{}, err
	}

	span.SetAttributes(
		attribute.String("response.content", resp.Content),
		attribute.String("response.finish_reason", resp.FinishReason),
	)

	logger.NewDefault().Log(ctx, logger.LogEntry{
		Level:   logger.InfoLevel,
		Module:  "e2edemo",
		Message: "provider returned",
		Fields:  []logger.Field{{Key: "content", Value: resp.Content}},
	})

	// 4. Append response to history.
	a.messages = append(a.messages, agentcore.Message{
		Role: "assistant", Content: resp.Content,
	})

	return agentcore.Output{
		Response: agentcore.Message{Role: "assistant", Content: resp.Content},
	}, nil
}

func (a *DemoAgent) Stream(_ context.Context, _ agentcore.Input) (<-chan agentcore.Event, error) {
	return nil, nil
}

func (a *DemoAgent) SetAgentRuntime(runtime *agentcore.AgentRuntime) {
	a.runtime = runtime
}

func lastContent(msgs []agentcore.Message) string {
	if len(msgs) == 0 {
		return ""
	}
	return msgs[len(msgs)-1].Content
}
