package mgmt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/tltre/gogent/pkg/component"
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
	w.Header().Set("Content-Type", "application/json")
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
