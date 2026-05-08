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

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/contextmanager"
	"github.com/tltre/gogent/pkg/tool"
)

// Version is the framework version. Set via ldflags at build time.
var Version = "0.5.0-dev"

type Server struct {
	http    *http.Server
	reg     *component.Registry
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

	s.http = &http.Server{
		Addr:    addr,
		Handler: mux,
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
			// FIXME: derive real lifecycle status from component (uninitialized/initialized/started/stopped)
			list = append(list, ComponentInfo{
				Name:   comp.GetName(),
				Type:   string(comp.GetType()),
				Status: "registered",
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
	types := []component.ComponentType{
		component.ComponentAgentCore, component.ComponentProvider,
		component.ComponentTool, component.ComponentEventBus,
		component.ComponentContextManager, component.ComponentMemory,
		component.ComponentSandbox,
		// FIXME: add Channel, Hook, Logger once health-check interfaces are defined
	}

	var results []HealthResult
	for _, typ := range types {
		for _, comp := range s.reg.GetByType(typ) {
			// FIXME: call component-specific health/ping interface instead of hardcoding "ok"
			results = append(results, HealthResult{
				Component: comp.GetName(),
				Status:    "ok",
			})
		}
	}

	if err := json.NewEncoder(w).Encode(results); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content/Type", "application/json")
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	// FIXME: agent name should come from app config, not hardcoded
	info := InfoResponse{
		Name:      "gogent",
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
	// FIXME: wire up to actual logger component for real-time log streaming
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "data: {\"message\":\"log stream started\"}\n\n")
	flusher.Flush()

	<-r.Context().Done()
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

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
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
