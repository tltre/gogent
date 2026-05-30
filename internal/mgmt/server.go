package mgmt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/tltre/gogent/internal/api"
	"github.com/tltre/gogent/pkg/agentcore"
	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/contextmanager"
	"github.com/tltre/gogent/pkg/logger"
	"github.com/tltre/gogent/pkg/tool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Server is the HTTP management server for an in-process agent.
// It serves app-level endpoints (registry, health, logs, etc.) on
// the agent's own mgmt port.
type Server struct {
	http    *http.Server
	reg     *component.Registry
	started time.Time
}

// Listen creates an agent-level management HTTP server. It registers handlers
// for app-level endpoints (registry, health, logs, etc.) and starts the server
// in a background goroutine.
func Listen(addr string, reg *component.Registry) *Server {
	s := &Server{
		reg:     reg,
		started: time.Now(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc(api.PathAppRegistry, s.handleRegistry)
	mux.HandleFunc(api.PathAppHealth, s.handleHealth)
	mux.HandleFunc(api.PathAppLogs, s.handleLogs)
	mux.HandleFunc(api.PathAppInfo, s.handleInfo)
	mux.HandleFunc(api.PathAppToolsExec, s.handleToolsExec)
	mux.HandleFunc(api.PathAppSessions, s.handleSessions)
	mux.HandleFunc(api.PathAppAgentRun, s.handleAgentRun)

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

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

func (s *Server) handleRegistry(w http.ResponseWriter, r *http.Request) {
	if s.reg == nil {
		http.Error(w, "registry not available", http.StatusServiceUnavailable)
		return
	}
	types := []component.ComponentType{
		component.ComponentChannel, component.ComponentAgentCore,
		component.ComponentProvider, component.ComponentTool,
		component.ComponentHook, component.ComponentEventBus,
		component.ComponentContextManager, component.ComponentMemory,
		component.ComponentSandbox, component.ComponentLogger,
	}
	var list []api.ComponentInfo
	for _, typ := range types {
		for _, comp := range s.reg.GetByType(typ) {
			list = append(list, api.ComponentInfo{
				Name:   comp.GetName(),
				Type:   string(comp.GetType()),
				Status: api.StatusToString(s.reg.GetComponentStatus(comp.GetName())),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	var results []api.HealthResult
	if s.reg == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
		return
	}
	types := []component.ComponentType{
		component.ComponentChannel, component.ComponentAgentCore,
		component.ComponentProvider, component.ComponentTool,
		component.ComponentHook, component.ComponentEventBus,
		component.ComponentContextManager, component.ComponentMemory,
		component.ComponentSandbox, component.ComponentLogger,
	}

	results = make([]api.HealthResult, 0)
	for _, typ := range types {
		for _, comp := range s.reg.GetByType(typ) {
			start := time.Now()
			result := api.HealthResult{Component: comp.GetName()}
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if s.reg == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Verify logger component is wired. Real-time log streaming requires a
	// subscription API on the Logger interface, which is not yet available.
	if lc := s.reg.GetDefault(component.ComponentLogger); lc != nil {
		if _, ok := lc.(*logger.LoggerComponent); !ok {
			fmt.Fprintf(os.Stderr, "[mgmt] logs: default logger is not a LoggerComponent\n")
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

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	name := "gogent"
	if s.reg != nil {
		if ac := s.reg.GetDefault(component.ComponentAgentCore); ac != nil {
			name = ac.GetName()
		}
	}

	resp := api.InfoResponse{
		Name:      name,
		Version:   api.Version,
		GoVersion: runtime.Version(),
		UptimeSec: int64(time.Since(s.started).Seconds()),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	cmComp := s.reg.GetDefault(component.ComponentContextManager)
	if cmComp == nil {
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

func (s *Server) handleAgentRun(w http.ResponseWriter, r *http.Request) {
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

	var req api.AgentRunRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}

	agentComp := s.reg.GetDefault(component.ComponentAgentCore)
	if agentComp == nil {
		http.Error(w, "no agent core registered", http.StatusServiceUnavailable)
		return
	}
	rt, ok := agentComp.(*agentcore.AgentRuntime)
	if !ok {
		http.Error(w, "default agent-core component type mismatch", http.StatusInternalServerError)
		return
	}

	input := agentcore.Input{
		Context: req.Context,
	}
	for _, m := range req.Messages {
		input.Messages = append(input.Messages, agentcore.Message{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	output, err := rt.Run(r.Context(), input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := api.AgentRunResponse{
		Response: output.Response.Content,
		Metadata: output.Metadata,
	}
	for _, action := range output.Actions {
		resp.Actions = append(resp.Actions, api.AgentAction{
			ToolName: action.ToolName,
			Params:   action.Params,
			Result:   action.Result,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ---------------------------------------------------------------------------
// SSE helpers (for handleLogs)
// ---------------------------------------------------------------------------

type ssePayload struct {
	Message   string `json:"message"`
	Level     string `json:"level"`
	Module    string `json:"module,omitempty"`
	Timestamp string `json:"timestamp"`
}

func writeSSE(w io.Writer, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}
