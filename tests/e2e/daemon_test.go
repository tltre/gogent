package e2e_test

import (
	"os"
	"testing"
)

// TestMain is the package-level test entry point.
//
// In Phase 0 this is a minimal skeleton retained from the original file.
// The old stdio-based mock daemon (mcp-go transport) was removed in v0.8.x.
//
// Phase 2 (Scheme B — full daemon flow) will restore the mock daemon
// pattern using gRPC-based stub servers instead of stdio JSON-RPC:
//
//	if os.Getenv("GOGENT_DAEMON_MOCK") == "1" {
//		os.Exit(runComponentMock())
//	}
func TestMain(m *testing.M) {
	// TODO(Phase 2): check GOGENT_DAEMON_MOCK env to fork gRPC mock process.
	os.Exit(m.Run())
}
