package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/tltre/gagent/pkg/app"
	"github.com/tltre/gagent/pkg/tool"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: gagent <config.yaml>")
		os.Exit(1)
	}

	configPath := os.Args[1]

	builder, err := app.NewBuilder(configPath)
	if err != nil {
		fmt.Printf("Failed to create builder: %v\n", err)
		os.Exit(1)
	}

	application, err := builder.Build()
	if err != nil {
		fmt.Printf("Failed to build application: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		cancel()
	}()

	if err := application.Run(ctx); err != nil {
		fmt.Printf("Application error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Application stopped gracefully")
}

func init() {
	calcTool := tool.NewNativeTool(tool.ToolInfo{
		Name:        "calculator",
		Description: "Perform basic arithmetic calculations",
	}, func(ctx context.Context, params map[string]any) (tool.Result, error) {
		expression, ok := params["expression"].(string)
		if !ok {
			return tool.Result{IsError: true, ErrorMsg: "expression required"}, nil
		}
		result, err := evaluate(expression)
		if err != nil {
			return tool.Result{IsError: true, ErrorMsg: err.Error()}, nil
		}
		return tool.Result{Output: result}, nil
	})
	_ = calcTool
}

type CalculatorTool struct{}

func (t *CalculatorTool) Info() tool.ToolInfo {
	return tool.ToolInfo{
		Name:        "calculator",
		Description: "Perform basic arithmetic calculations",
	}
}

func (t *CalculatorTool) Execute(ctx context.Context, params map[string]any) (tool.Result, error) {
	expression, ok := params["expression"].(string)
	if !ok {
		return tool.Result{IsError: true, ErrorMsg: "expression required"}, nil
	}

	result, err := evaluate(expression)
	if err != nil {
		return tool.Result{IsError: true, ErrorMsg: err.Error()}, nil
	}

	return tool.Result{Output: result}, nil
}

func (t *CalculatorTool) Stream(ctx context.Context, params map[string]any) (<-chan tool.StreamChunk, error) {
	ch := make(chan tool.StreamChunk, 1)
	go func() {
		defer close(ch)
		result, err := t.Execute(ctx, params)
		ch <- tool.StreamChunk{Data: result.Output, Done: true, Error: err}
	}()
	return ch, nil
}

func evaluate(expr string) (string, error) {
	return fmt.Sprintf("Result of %s", expr), nil
}
