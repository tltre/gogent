package tool

import (
	"context"
	"fmt"
	"os"

	"github.com/tltre/gogent/internal/grpctransport"
	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
)

// dialDaemon reads GOGENT_DAEMON_GRPC_ADDR and establishes a gRPC connection.
// Sets tm.grpcConn and tm.client on success. Thread-safe via tm.mu.
func (tm *ToolManager) dialDaemon() error {
	addr := os.Getenv("GOGENT_DAEMON_GRPC_ADDR")
	if addr == "" {
		return fmt.Errorf("GOGENT_DAEMON_GRPC_ADDR not set")
	}

	var conn *grpctransport.Conn
	var err error
	if tm.dialFn != nil {
		conn, err = tm.dialFn()
	} else {
		conn, err = grpctransport.Dial(addr)
	}
	if err != nil {
		return fmt.Errorf("dial daemon: %w", err)
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.grpcConn = conn
	tm.client = gogentv1.NewToolServiceClient(conn.ClientConn())
	return nil
}

// registerManifest sends RegisterManifest RPC and caches accepted tools.
// Must be called after dialDaemon.
func (tm *ToolManager) registerManifest(ctx context.Context) error {
	tm.mu.RLock()
	client := tm.client
	entries := tm.manifest
	appName := tm.appName
	tm.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("daemon not connected")
	}

	// Build request
	pbEntries := make([]*gogentv1.ManifestEntry, len(entries))
	for i, m := range entries {
		pbEntries[i] = &gogentv1.ManifestEntry{
			Name:          m.Name,
			SecurityLevel: int32(m.Level),
		}
	}

	req := &gogentv1.ManifestRequest{
		AppName: appName,
		Tools:   pbEntries,
	}

	resp, err := client.RegisterManifest(ctx, req)
	if err != nil {
		return fmt.Errorf("register manifest: %w", err)
	}

	// Cache accepted tools
	var cache []ToolInfo
	for _, s := range resp.Tools {
		if s.Accepted {
			cache = append(cache, ToolInfo{
				Name: s.Name,
			})
		}
	}

	tm.mu.Lock()
	tm.cache = cache
	tm.connected = true
	tm.mu.Unlock()

	return nil
}
