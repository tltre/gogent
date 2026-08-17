package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tltre/gogent/internal/otel"
	"github.com/tltre/gogent/pkg/agentcore"
	"github.com/tltre/gogent/pkg/logger"
)

func main() {
	ctx := context.Background()

	// 1. Initialise OpenTelemetry with Jaeger OTLP gRPC exporter.
	shutdown, err := otel.InitFromConfig(ctx, otel.Config{
		Enabled:        true,
		Endpoint:       "localhost:4317",
		ServiceName:    "gogent-demo",
		ServiceVersion: "0.10.0",
		Environment:    "e2e",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[warn] OTel init failed (traces disabled): %v\n", err)
	} else {
		defer func() { _ = shutdown(context.Background()) }()
	}

	// 2. Create in-process demo components.
	//    DemoProvider implements IProvider directly (no ProviderManager
	//    wiring needed for the standalone demo binary).
	prov := NewDemoProvider()
	runtime := agentcore.NewComponent("demo-agent", NewDemoAgent(prov))

	// 3. Interactive REPL.
	banner := "  Gogent Demo v0.10.0  |  type 'exit' to quit  |  traces at http://localhost:16686"
	fmt.Println(strings.Repeat("=", len(banner)))
	fmt.Println(banner)
	fmt.Println(strings.Repeat("=", len(banner)))
	fmt.Fprintln(os.Stdout)

	scanner := bufio.NewScanner(os.Stdin)
	reqCount := 0
	fmt.Fprint(os.Stdout, "You > ")

	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			fmt.Fprint(os.Stdout, "You > ")
			continue
		}
		switch input {
		case "exit", "quit":
			goto done
		case "help":
			fmt.Fprintln(os.Stdout, "Commands: hello, name is <name>, weather [in <city>], joke, exit")
			fmt.Fprint(os.Stdout, "You > ")
			continue
		}

		logger.NewDefault().Log(ctx, logger.LogEntry{
			Level:   logger.InfoLevel,
			Module:  "e2edemo",
			Message: "user input",
			Fields:  []logger.Field{{Key: "input", Value: input}},
		})

		output, err := runtime.Run(ctx, agentcore.Input{
			Messages: []agentcore.Message{
				{Role: "user", Content: input},
			},
		})
		if err != nil {
			fmt.Fprintf(os.Stdout, "Error: %v\nYou > ", err)
			continue
		}
		reqCount++
		fmt.Fprintf(os.Stdout, "Agent> %s\nYou > ", output.Response.Content)
	}

done:
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Scanner error: %v\n", err)
	}

	fmt.Fprintf(os.Stderr, "\nBye! %d requests traced. Open http://localhost:16686 to view.\n", reqCount)
	time.Sleep(200 * time.Millisecond)
}

