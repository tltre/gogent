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
		s.http.ListenAndServe()
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
			list = append(list, ComponentInfo{
				Name:   comp.GetName(),
				Type:   string(comp.GetType()),
				Status: "registered",
			})
		}
	}

	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	types := []component.ComponentType{
		component.ComponentAgentCore, component.ComponentProvider,
		component.ComponentTool, component.ComponentEventBus,
		component.ComponentContextManager, component.ComponentMemory,
		component.ComponentSandbox,
	}

	var results []HealthResult
	for _, typ := range types {
		for _, comp := range s.reg.GetByType(typ) {
			results = append(results, HealthResult{
				Component: comp.GetName(),
				Status:    "ok",
			})
		}
	}

	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	info := InfoResponse{
		Name:      "gogent",
		Version:   "0.5.0-dev",
		GoVersion: runtime.Version(),
		UptimeSec: int64(time.Since(s.started).Seconds()),
	}

	writeJSON(w, http.StatusOK, info)
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

	fmt.Fprintf(w, "data: {\"message\":\"log stream started\"}\n\n")
	flusher.Flush()

	<-r.Context().Done()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

