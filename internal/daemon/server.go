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
