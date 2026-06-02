package tool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// McpRunner handles both process and http MCP servers via mcp-go.
// Servers are started during daemon initialization (not lazy).
// Clients are keyed by server name, consistent across both transports.
type McpRunner struct {
	mu      sync.Mutex
	servers map[string]*ServerInfo // key: server name
	store   *ServerStore
	reg     *ToolRegistry
}

// NewMcpRunner creates an McpRunner backed by the given store and registry.
func NewMcpRunner(store *ServerStore, reg *ToolRegistry) *McpRunner {
	return &McpRunner{
		servers: make(map[string]*ServerInfo),
		store:   store,
		reg:     reg,
	}
}

// Execute dispatches a tool call to the appropriate MCP server.
// def.Name is expected in "<server>.<tool>" format for child tools.
func (r *McpRunner) Execute(ctx context.Context, def *ToolDefinition, params map[string]any) (Result, error) {
	serverName, toolName := parseServerTool(def)
	if serverName == "" {
		return Result{IsError: true, ErrorMsg: "mcp: expected <server>.<tool> format"}, nil
	}

	r.mu.Lock()
	info, ok := r.servers[serverName]
	r.mu.Unlock()
	if !ok || info.Status != StatusActive {
		return Result{IsError: true, ErrorMsg: "mcp: server " + serverName + " not active"}, nil
	}

	result, err := info.Client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: toolName, Arguments: params},
	})
	if err != nil {
		return Result{IsError: true, ErrorMsg: "mcp: " + err.Error()}, nil
	}
	return mcpResultToResult(result), nil
}

// StartServer starts an MCP server, discovers tools via ListTools,
// and registers child tools in the ToolRegistry as "<server>.<tool>".
// Called during daemon initialization.
func (r *McpRunner) StartServer(ctx context.Context, serverName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.servers[serverName]; ok {
		return nil // already running
	}

	storeInfo, ok := r.store.Get(serverName)
	if !ok {
		return fmt.Errorf("server %q not found in store", serverName)
	}
	if storeInfo.Driver == string(DriverBuiltin) {
		return nil // builtin tools don't need a client
	}

	// Create transport-specific client
	var mcpCl client.MCPClient
	var err error
	switch storeInfo.Driver {
	case string(DriverProcess):
		mcpCl, err = client.NewStdioMCPClient(storeInfo.Command, envMapToSlice(storeInfo.Env))
	case string(DriverHTTP):
		mcpCl, err = client.NewStreamableHttpClient(storeInfo.Endpoint)
	default:
		return fmt.Errorf("unsupported driver: %s", storeInfo.Driver)
	}
	if err != nil {
		return fmt.Errorf("create mcp client: %w", err)
	}

	// Initialize MCP session
	if _, err := mcpCl.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		mcpCl.Close()
		return fmt.Errorf("mcp init: %w", err)
	}

	// Discover tools and register in ToolRegistry
	toolsResult, err := mcpCl.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		mcpCl.Close()
		return fmt.Errorf("list tools: %w", err)
	}
	for _, t := range toolsResult.Tools {
		childName := serverName + "." + t.Name
		childDef := &ToolDefinition{
			Name:        childName,
			ServerName:  serverName,
			Description: t.Description,
			DefaultLvl:  storeInfo.DefaultLvl,
		}
		// Register each child tool (idempotent)
		r.reg.RegisterOrUpdate(childDef)
	}
	storeInfo.ToolCount = len(toolsResult.Tools)

	// Store runtime info
	info := &ServerInfo{
		Name:      serverName,
		Client:    mcpCl,
		Status:    StatusActive,
		StartedAt: time.Now(),
	}
	r.servers[serverName] = info
	storeInfo.Status = StatusActive
	storeInfo.Client = mcpCl
	storeInfo.StartedAt = info.StartedAt
	return nil
}

// StopServer gracefully stops a server and closes its client.
func (r *McpRunner) StopServer(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	info, ok := r.servers[name]
	if !ok {
		return nil
	}
	delete(r.servers, name)
	if info.Client != nil {
		return info.Client.Close()
	}
	return nil
}

// RestartServer closes the old client and starts a new one.
// Re-discovers tools (idempotent registrations).
func (r *McpRunner) RestartServer(ctx context.Context, name string) error {
	r.mu.Lock()
	if old, ok := r.servers[name]; ok {
		if old.Client != nil {
			old.Client.Close()
		}
		delete(r.servers, name)
	}
	r.mu.Unlock()

	// Also reset ServerStore status so StartServer can proceed
	r.store.SetStatus(name, StatusRegistered)
	return r.StartServer(ctx, name)
}

// GetServerStatus returns the runtime status of a server.
func (r *McpRunner) GetServerStatus(name string) ToolStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	if info, ok := r.servers[name]; ok {
		return info.Status
	}
	return StatusRemoved
}

// ListServers returns all running server names.
func (r *McpRunner) ListServers() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.servers))
	for name := range r.servers {
		names = append(names, name)
	}
	return names
}

// parseServerTool extracts server name and tool name from "<server>.<tool>".
func parseServerTool(def *ToolDefinition) (server, tool string) {
	if def.ServerName != "" {
		return def.ServerName, def.Name[len(def.ServerName)+1:]
	}
	for i := len(def.Name) - 1; i >= 0; i-- {
		if def.Name[i] == '.' {
			return def.Name[:i], def.Name[i+1:]
		}
	}
	return "", def.Name
}

// envMapToSlice converts a map to KEY=VALUE slice for mcp-go.
func envMapToSlice(env map[string]string) []string {
	if env == nil {
		return nil
	}
	slice := make([]string, 0, len(env))
	for k, v := range env {
		slice = append(slice, k+"="+v)
	}
	return slice
}
