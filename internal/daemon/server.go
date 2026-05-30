package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/tltre/gogent/internal/api"
	"github.com/tltre/gogent/internal/daemon/tool"
	"github.com/tltre/gogent/pkg/component"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Server is the HTTP management server for the daemon process.
// It serves daemon-level endpoints (apps, components/health, info).
type Server struct {
	http    *http.Server
	store   *AppStore
	daemon  *Daemon
	started time.Time
}

// NewDaemonServer creates a management server for the daemon process.
// It registers daemon-level endpoints and starts the server in a background goroutine.
func NewDaemonServer(addr string, d *Daemon) *Server {
	s := &Server{
		store:   d.Store(),
		daemon:  d,
		started: time.Now(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc(api.PathDaemonApps, s.handleApps)
	mux.HandleFunc(api.PathDaemonAppsPath, s.handleAppsPath)
	mux.HandleFunc(api.PathDaemonComponentsHealth, s.handleComponentsHealth)
	mux.HandleFunc(api.PathDaemonInfo, s.handleInfo)
	mux.HandleFunc(api.PathDaemonTools, s.handleTools)
	mux.HandleFunc(api.PathDaemonToolsPath, s.handleToolsPath)

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

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	resp := api.InfoResponse{
		Name:      "gogent-daemon",
		Version:   api.Version,
		GoVersion: "unknown", // filled below if available
		UptimeSec: int64(time.Since(s.started).Seconds()),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleTools returns all registered tool definitions (GET) or registers a new one (POST).
func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tools := s.daemon.ToolRegistry().List()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tools)

	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var req api.RegisterToolRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		def := &tool.ToolDefinition{
			Name:        req.Name,
			Driver:      req.Driver,
			Command:     req.Command,
			Endpoint:    req.Endpoint,
			DefaultLvl:  req.DefaultLevel,
			Description: req.Description,
			Env:         req.Env,
		}
		if err := s.daemon.ToolRegistry().Register(def); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"status": "registered"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleToolsPath returns status for a single tool (GET) or deletes it (DELETE).
func (s *Server) handleToolsPath(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, api.PathDaemonToolsPath)
	if name == "" || strings.Contains(name, "/") {
		http.Error(w, "tool name required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		def, ok := s.daemon.ToolRegistry().Get(name)
		if !ok {
			http.Error(w, "tool not found", http.StatusNotFound)
			return
		}
		status := s.daemon.ToolRegistry().GetStatus(name)
		stats := s.daemon.ToolRegistry().GetStats(name)

		resp := map[string]any{
			"name":         def.Name,
			"driver":       def.Driver,
			"description":  def.Description,
			"defaultLevel": def.DefaultLvl,
			"source":       s.daemon.ToolRegistry().GetSource(name),
			"status":       string(status),
			"invocations":  stats.Invocations,
			"failures":     stats.Failures,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

	case http.MethodDelete:
		// Safety check: refuse if apps are using this tool (unless force=true)
		if r.URL.Query().Get("force") != "true" {
			apps := s.daemon.ManifestStore().AppsUsingTool(name)
			if len(apps) > 0 {
				errMsg := fmt.Sprintf("tool %q is in use by apps: %v (use --force to override)", name, apps)
				http.Error(w, errMsg, http.StatusConflict)
				return
			}
		}
		if err := s.daemon.ToolRegistry().Unregister(name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "unregistered"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list := s.store.List()
		if list == nil {
			list = []api.AppInfo{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)

	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var req api.LoadAppRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}

		needFork := true
		if req.NeedForkApplication != nil {
			needFork = *req.NeedForkApplication
		}

		info, err := s.daemon.LoadApp(req.ConfigPath, needFork)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(info)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAppsPath(w http.ResponseWriter, r *http.Request) {
	// Extract app name from path: /api/v1/daemon/apps/{name}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, api.PathDaemonAppsPath), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "app name required", http.StatusBadRequest)
		return
	}
	name := parts[0]

	switch r.Method {
	case http.MethodGet:
		info, exists := s.daemon.GetApp(name)
		if !exists {
			http.Error(w, fmt.Sprintf("app %s not found", name), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)

	case http.MethodDelete:
		if err := s.daemon.StopApp(name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleComponentsHealth(w http.ResponseWriter, r *http.Request) {
	if s.daemon == nil {
		http.Error(w, "daemon not available", http.StatusServiceUnavailable)
		return
	}

	appName := r.URL.Query().Get("app")

	var comps []api.ComponentInfo
	if appName != "" {
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
		comps = s.daemon.ComponentStore().List()
	}

	var results []api.ComponentHealthResult
	for _, comp := range comps {
		result := api.ComponentHealthResult{
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
			result.Status = "ok"
		}
		results = append(results, result)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}
