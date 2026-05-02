package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/tltre/gagent/pkg/app"
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
