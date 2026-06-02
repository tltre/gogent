package tool

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
)

const (
	maxRetries          = 3
	healthCheckInterval = 30 * time.Second
)

// LifecycleManager manages MCP server health checks and auto-restart.
// Uses MCP Ping for both process and http drivers — consistent behavior.
type LifecycleManager struct {
	store      *ServerStore
	runner     *McpRunner
	retryCount map[string]int
	mu         sync.Mutex
}

// NewLifecycleManager creates a lifecycle manager for the given store and runner.
func NewLifecycleManager(store *ServerStore, runner *McpRunner) *LifecycleManager {
	return &LifecycleManager{
		store:      store,
		runner:     runner,
		retryCount: make(map[string]int),
	}
}

// Start begins the health check loop in a background goroutine.
func (lm *LifecycleManager) Start(ctx context.Context) {
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			lm.checkAll(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (lm *LifecycleManager) checkAll(ctx context.Context) {
	for _, info := range lm.store.List() {
		if info.Driver == string(DriverBuiltin) {
			continue // builtin tools are always healthy
		}
		if info.Status != StatusActive && info.Status != StatusUnhealthy {
			continue
		}
		if info.Client == nil {
			continue
		}

		// Use MCP Ping — works for both stdio and http transport
		err := info.Client.Ping(ctx)
		if err == nil {
			lm.resetRetry(info.Name)
			if info.Status != StatusActive {
				lm.store.SetStatus(info.Name, StatusActive)
			}
			continue
		}

		// Ping failed — handle as unhealthy
		lm.handleFailure(ctx, info.Name)
	}
}

func (lm *LifecycleManager) handleFailure(ctx context.Context, name string) {
	lm.mu.Lock()
	lm.retryCount[name]++
	count := lm.retryCount[name]
	lm.mu.Unlock()

	lm.store.SetStatus(name, StatusUnhealthy)

	if count > maxRetries {
		lm.store.SetStatus(name, StatusStopped)
		fmt.Fprintf(os.Stderr, "[lifecycle] server %q stopped after %d failures\n", name, count)
		return
	}

	// Attempt restart
	if err := lm.runner.RestartServer(ctx, name); err != nil {
		fmt.Fprintf(os.Stderr, "[lifecycle] restart %q failed: %v\n", name, err)
		return
	}

	lm.store.SetStatus(name, StatusActive)
	lm.resetRetry(name)
}

func (lm *LifecycleManager) resetRetry(name string) {
	lm.mu.Lock()
	delete(lm.retryCount, name)
	lm.mu.Unlock()
}
