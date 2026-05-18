package mgmt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/tltre/gogent/pkg/agentcore"
	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/contextmanager"
	"github.com/tltre/gogent/pkg/logger"
	"github.com/tltre/gogent/pkg/tool"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Version is the framework version. Set via ldflags at build time.
var Version = "0.6.0-dev"

func statusToString(s component.ComponentStatus) string {
	switch s {
	case component.StatusUninitialized:
		return "uninitialized"
	case component.StatusInitialized:
		return "initialized"
	case component.StatusStarted:
		return "started"
	case component.StatusStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

type Server struct {
	http    *http.Server
	reg     *component.Registry
	store   *AppStore
	daemon  *Daemon
	started time.Time
}

func Listen(addr string, reg *component.Registry) *Server {
	s := &Server{
		reg:     reg,
		started: time.Now(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/registry", s.handleRegistry)
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/logs", s.handleLogs)
	mux.HandleFunc("/api/v1/info", s.handleInfo)
	mux.HandleFunc("/api/v1/tools/exec", s.handleToolsExec)
	mux.HandleFunc("/api/v1/sessions", s.handleSessions)
	mux.HandleFunc("/api/v1/agent/run", s.handleAgentRun)

	otelHandler := otelhttp.NewHandler(mux, "mgmt-agent")

	s.http = &http.Server{
		Addr:    addr,
		Handler: otelHandler,
	}

	go func() {
		fmt.Fprintf(os.Stderr, "[mgmt] listening on %s\n", addr)
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "[mgmt] server error: %v\n", err)
		}
	}()

	return s
}

// NewDaemonServer creates a management server for the daemon process. It registers
// daemon-level /api/v1/apps endpoints in addition to the standard agent-level endpoints.
func NewDaemonServer(addr string, reg *component.Registry, d *Daemon) *Server {
	s := &Server{
		reg:     reg,
		store:   d.Store(),
		daemon:  d,
		started: time.Now(),
	}

	mux := http.NewServeMux()
	// Daemon-level endpoints
	mux.HandleFunc("/api/v1/apps", s.handleApps)
	mux.HandleFunc("/api/v1/apps/", s.handleAppsPath)
	mux.HandleFunc("/api/v1/health/components", s.handleComponentsHealth)

	// Agent-level endpoints
	mux.HandleFunc("/api/v1/registry", s.handleRegistry)
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/logs", s.handleLogs)
	mux.HandleFunc("/api/v1/info", s.handleInfo)
	mux.HandleFunc("/api/v1/tools/exec", s.handleToolsExec)
	mux.HandleFunc("/api/v1/sessions", s.handleSessions)

	otelHandler := otelhttp.NewHandler(mux, "mgmt-daemon")

	s.http = &http.Server{
		Addr:    addr,
		Handler: otelHandler,
	}

	go func() {
		fmt.Fprintf(os.Stderr, "[mgmt] listening on %s\n", addr)
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "[mgmt] server error: %v\n", err)
		}
	}()

	return s
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

func (s *Server) handleRegistry(w http.ResponseWriter, r *http.Request) {
	if s.reg == nil {
		http.Error(w, "not available in daemon mode", http.StatusServiceUnavailable)
		return
	}
	types := []component.ComponentType{
		component.ComponentChannel, component.ComponentAgentCore,
		component.ComponentProvider, component.ComponentTool,
		component.ComponentHook, component.ComponentEventBus,
		component.ComponentContextManager, component.ComponentMemory,
		component.ComponentSandbox, component.ComponentLogger,
	}
	var list []ComponentInfo
	for _, typ := range types {
		for _, comp := range s.reg.GetByType(typ) {
			list = append(list, ComponentInfo{
				Name:   comp.GetName(),
				Type:   string(comp.GetType()),
				Status: statusToString(s.reg.GetComponentStatus(comp.GetName())),
			})
		}
	}

	if err := json.NewEncoder(w).Encode(list); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	var results []HealthResult
	if s.reg == nil {
		json.NewEncoder(w).Encode(results)
		w.Header().Set("Content-Type", "application/json")
		return
	}
	types := []component.ComponentType{
		component.ComponentChannel, component.ComponentAgentCore,
		component.ComponentProvider, component.ComponentTool,
		component.ComponentHook, component.ComponentEventBus,
		component.ComponentContextManager, component.ComponentMemory,
		component.ComponentSandbox, component.ComponentLogger,
	}

	results = make([]HealthResult, 0)
	for _, typ := range types {
		for _, comp := range s.reg.GetByType(typ) {
			start := time.Now()
			result := HealthResult{Component: comp.GetName()}
			if hc, ok := comp.(component.HealthChecker); ok {
				if err := hc.Health(r.Context()); err != nil {
					result.Status = "unhealthy"
					result.Error = err.Error()
				} else {
					result.Status = "ok"
				}
			} else {
				result.Status = "ok"
			}
			result.LatencyMs = time.Since(start).Milliseconds()
			results = append(results, result)
		}
	}

	if err := json.NewEncoder(w).Encode(results); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
}

func (s *Server) handleComponentsHealth(w http.ResponseWriter, r *http.Request) {
	if s.daemon == nil {
		http.Error(w, "not available in agent mode", http.StatusServiceUnavailable)
		return
	}

	appName := r.URL.Query().Get("app")

	var comps []ComponentInfo
	if appName != "" {
		// Filtered: look up AppInfo, iterate its Components list.
		appInfo, exists := s.daemon.GetApp(appName)
		if !exists {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("app %q not found", appName)})
			return
		}
		for _, compName := range appInfo.Components {
			if compInfo, ok := s.daemon.ComponentStore().Get(compName); ok {
				comps = append(comps, *compInfo)
			}
		}
	} else {
		// All components from ComponentStore.
		comps = s.daemon.ComponentStore().List()
	}

	var results []ComponentHealthResult
	for _, comp := range comps {
		result := ComponentHealthResult{
			Name:   comp.Name,
			Type:   comp.Type,
			Driver: string(comp.Driver),
		}
		if comp.Driver == component.DriverProcess || comp.Driver == component.DriverHTTP {
			if comp.Target == "" {
				result.Status = "unknown"
			} else {
				start := time.Now()
				if err := s.daemon.GRPCHealthCheck(comp.Target); err != nil {
					result.Status = "unhealthy"
					result.Error = err.Error()
				} else {
					result.Status = "ok"
				}
				result.LatencyMs = time.Since(start).Milliseconds()
			}
		} else {
			result.Status = "ok" // native: no independent check, assume healthy
		}
		results = append(results, result)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	name := "gogent"
	if s.reg != nil {
		if ac := s.reg.GetDefault(component.ComponentAgentCore); ac != nil {
			name = ac.GetName()
		}
	}
	info := InfoResponse{
		Name:      name,
		Version:   Version,
		GoVersion: runtime.Version(),
		UptimeSec: int64(time.Since(s.started).Seconds()),
	}

	if err := json.NewEncoder(w).Encode(info); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Verify logger component is wired. Real-time log streaming requires a
	// subscription API on the Logger interface, which is not yet available.
	// Once the Logger interface gains a Subscribe() or channel-based API, this
	// handler will stream log entries directly.
	if s.reg != nil {
		if lc := s.reg.GetDefault(component.ComponentLogger); lc != nil {
			if _, ok := lc.(*logger.LoggerComponent); !ok {
				fmt.Fprintf(os.Stderr, "[mgmt] logs: default logger is not a LoggerComponent\n")
			}
		}
	}

	writeSSE(w, ssePayload{
		Message:   "log stream connected",
		Level:     "info",
		Module:    "mgmt",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	flusher.Flush()

	// Periodic keepalive to prevent proxy/load-balancer timeouts.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			writeSSE(w, ssePayload{
				Message:   "heartbeat",
				Level:     "debug",
				Module:    "mgmt",
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// ssePayload is the JSON payload sent as an SSE event for handleLogs.
type ssePayload struct {
	Message   string `json:"message"`
	Level     string `json:"level"`
	Module    string `json:"module,omitempty"`
	Timestamp string `json:"timestamp"`
}

// writeSSE marshals v as JSON and writes it as an SSE data frame.
func writeSSE(w io.Writer, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func (s *Server) handleToolsExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req struct {
		Tool   string         `json:"tool"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}

	if s.reg == nil {
		http.Error(w, "not available in daemon mode", http.StatusServiceUnavailable)
		return
	}
	tmComp := s.reg.GetDefault(component.ComponentTool)
	if tmComp == nil {
		http.Error(w, "no tool manager registered", http.StatusServiceUnavailable)
		return
	}
	tm, ok := tmComp.(*tool.ToolManager)
	if !ok {
		http.Error(w, "default tool component is not a ToolManager", http.StatusInternalServerError)
		return
	}

	start := time.Now()
	result, execErr := tm.Execute(r.Context(), req.Tool, req.Params)
	elapsed := time.Since(start)

	resp := map[string]any{
		"tool_name":   req.Tool,
		"output":      result.Output,
		"is_error":    result.IsError,
		"error_msg":   result.ErrorMsg,
		"duration_ms": elapsed.Milliseconds(),
	}
	if execErr != nil {
		resp["exec_error"] = execErr.Error()
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if s.reg == nil {
		http.Error(w, "not available in daemon mode", http.StatusServiceUnavailable)
		return
	}
	cmComp := s.reg.GetDefault(component.ComponentContextManager)
	if cmComp == nil {
		// FIXME: return empty list gracefully when no context manager configured
		json.NewEncoder(w).Encode([]string{})
		return
	}
	cm, ok := cmComp.(*contextmanager.ContextManagerComponent)
	if !ok {
		http.Error(w, "default context-manager component type mismatch", http.StatusInternalServerError)
		return
	}

	sessions := cm.ListSessions()
	if sessions == nil {
		sessions = []string{}
	}

	if err := json.NewEncoder(w).Encode(sessions); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
}

func (s *Server) handleAgentRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.reg == nil {
		http.Error(w, "not available in daemon mode", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req AgentRunRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}

	agentComp := s.reg.GetDefault(component.ComponentAgentCore)
	if agentComp == nil {
		http.Error(w, "no agent core registered", http.StatusServiceUnavailable)
		return
	}
	agent, ok := agentComp.(*agentcore.AgentRuntime)
	if !ok {
		http.Error(w, "default agent component is not an AgentRuntime", http.StatusInternalServerError)
		return
	}

	// Convert request messages to agentcore.Input
	msgs := make([]agentcore.Message, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = agentcore.Message{Role: m.Role, Content: m.Content}
	}
	input := agentcore.Input{
		Messages: msgs,
		Context:  req.Context,
	}

	output, runErr := agent.Run(r.Context(), input)

	resp := AgentRunResponse{
		Response: AgentMessage{Role: output.Response.Role, Content: output.Response.Content},
	}

	if output.Actions != nil {
		actions := make([]AgentAction, len(output.Actions))
		for i, a := range output.Actions {
			actions[i] = AgentAction{
				ToolName: a.ToolName,
				Params:   a.Params,
				Result:   a.Result,
			}
		}
		resp.Actions = actions
	}
	if output.Metadata != nil {
		resp.Metadata = output.Metadata
	}
	if runErr != nil {
		resp.Error = runErr.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// --- Daemon-level endpoints (/api/v1/apps) ---

func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListApps(w, r)
	case http.MethodPost:
		s.handleLoadApp(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAppsPath(w http.ResponseWriter, r *http.Request) {
	subPath := strings.TrimPrefix(r.URL.Path, "/api/v1/apps/")
	parts := strings.SplitN(subPath, "/", 2)
	name := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		s.handleGetApp(w, r, name)
	case action == "" && r.Method == http.MethodDelete:
		s.handleStopApp(w, r, name)
	case action == "restart" && r.Method == http.MethodPost:
		s.handleRestartApp(w, r, name)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (s *Server) handleLoadApp(w http.ResponseWriter, r *http.Request) {
	if s.daemon == nil {
		http.Error(w, "daemon not configured", http.StatusInternalServerError)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req LoadAppRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}

	info, err := s.daemon.LoadApp(req.ConfigPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(info)
}

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.daemon != nil {
		apps := s.daemon.ListApps()
		if err := json.NewEncoder(w).Encode(apps); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	} else if s.store != nil {
		apps := s.store.List()
		if err := json.NewEncoder(w).Encode(apps); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	} else {
		json.NewEncoder(w).Encode([]AppInfo{})
	}
}

func (s *Server) handleGetApp(w http.ResponseWriter, r *http.Request, name string) {
	if s.daemon == nil {
		http.Error(w, "daemon not configured", http.StatusInternalServerError)
		return
	}

	info, exists := s.daemon.GetApp(name)
	if !exists {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("app %q not found", name)})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func (s *Server) handleStopApp(w http.ResponseWriter, r *http.Request, name string) {
	if s.daemon == nil {
		http.Error(w, "daemon not configured", http.StatusInternalServerError)
		return
	}

	if err := s.daemon.StopApp(name); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRestartApp(w http.ResponseWriter, r *http.Request, name string) {
	if s.daemon == nil {
		http.Error(w, "daemon not configured", http.StatusInternalServerError)
		return
	}

	info, err := s.daemon.RestartApp(name)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(info)
}
