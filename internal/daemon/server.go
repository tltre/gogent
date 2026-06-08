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
	"github.com/tltre/gogent/internal/daemon/sandbox"
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
	mux.HandleFunc(api.PathDaemonToolsRestart, s.handleToolsRestart)

	// v0.13.x: Sandbox management endpoints
	mux.HandleFunc(api.PathDaemonSandboxProviders, s.handleSandboxProviders)
	mux.HandleFunc(api.PathDaemonSandboxProvidersPath, s.handleSandboxProvidersPath)
	mux.HandleFunc(api.PathDaemonSandboxProfiles, s.handleSandboxProfiles)
	mux.HandleFunc(api.PathDaemonSandboxProfilesPath, s.handleSandboxProfilesPath)
	mux.HandleFunc(api.PathDaemonSandboxDefaults, s.handleSandboxDefaults)
	mux.HandleFunc(api.PathDaemonSandboxReload, s.handleSandboxReload)
	mux.HandleFunc(api.PathDaemonSandboxStatus, s.handleSandboxStatus)

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
		// Return sanitized list — Env omitted for credential isolation
		tools := s.daemon.ToolRegistry().List()
		sanitized := make([]tool.ToolDefinition, len(tools))
		for i, t := range tools {
			sanitized[i] = t.Sanitized()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sanitized)

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
			Env:         req.Env, // Store raw ${VAR} references
		}
		if err := s.daemon.ToolRegistry().Register(def); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}

		// Persist to tools.yaml so the tool survives daemon restart
		if err := tool.WriteToolToFile("", def); err != nil {
			fmt.Fprintf(os.Stderr, "[tool] persist error: %v\n", err)
		}

		// Write credentials to credentials.yaml (v0.12.6)
		if len(req.Credentials) > 0 {
			credPath := "" // use default ~/.gogent/credentials.yaml
			if err := tool.WriteCredentials(credPath, req.Credentials); err != nil {
				// Log but don't fail the registration — the tool is already registered
				fmt.Fprintf(os.Stderr, "[cred] write error: %v\n", err)
			}
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

		// Clean up tools.yaml and credentials.yaml
		if err := tool.RemoveToolFromFile("", name); err != nil {
			fmt.Fprintf(os.Stderr, "[tool] remove from file error: %v\n", err)
		}
		if err := tool.RemoveCredentialsGroup("", name); err != nil {
			fmt.Fprintf(os.Stderr, "[cred] cleanup error for %q: %v\n", name, err)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "unregistered"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleToolsRestart restarts a registered MCP server.
// POST /api/v1/daemon/tools/{name}/restart
func (s *Server) handleToolsRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, api.PathDaemonToolsRestart)
	name = strings.TrimSuffix(name, "/restart")
	if name == "" || strings.Contains(name, "/") {
		http.Error(w, "server name required", http.StatusBadRequest)
		return
	}

	// Verify the server exists in ServerStore
	if !s.daemon.ServerStore().Exists(name) {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	}

	// Restart via the handler's McpRunner
	if handler := s.daemon.Handler(); handler != nil {
		if err := handler.RestartServer(context.Background(), name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "restarted"})
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

// ---------------------------------------------------------------------------
// Sandbox handlers
// ---------------------------------------------------------------------------

// handleSandboxProviders lists providers (GET) or adds one (POST).
func (s *Server) handleSandboxProviders(w http.ResponseWriter, r *http.Request) {
	mgr := s.daemon.SandboxManager()
	if mgr == nil {
		http.Error(w, `{"error":"sandbox manager not available"}`, http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		providers := mgr.ProviderRegistry().List()
		type providerView struct {
			Name string          `json:"name"`
			Type sandbox.SandboxType `json:"type"`
		}
		view := make([]providerView, 0, len(providers))
		for _, p := range providers {
			view = append(view, providerView{Name: p.Name(), Type: p.Type()})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(view)

	case http.MethodPost:
		var req struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Endpoint string `json:"endpoint,omitempty"`
			APIKey   string `json:"apiKey,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json: `+err.Error()+`"}`, http.StatusBadRequest)
			return
		}
		if req.Name == "" || req.Type == "" {
			http.Error(w, `{"error":"name and type are required"}`, http.StatusBadRequest)
			return
		}

		var p sandbox.SandboxProvider
		switch sandbox.SandboxType(req.Type) {
		case sandbox.SandboxE2B:
			ep, err := sandbox.NewE2BProvider(req.Name, req.APIKey, req.Endpoint)
			if err != nil {
				http.Error(w, `{"error":"create e2b provider: `+err.Error()+`"}`, http.StatusBadRequest)
				return
			}
			p = ep
		default:
			p = &sandbox.GenericProvider{PName: req.Name, PType: sandbox.SandboxType(req.Type)}
		}

		if err := mgr.RegisterProvider(p); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusConflict)
			return
		}
		// Persist to file
		sandbox.WriteProviderToFile("", req.Name, sandbox.ProviderEntry{
			Type: req.Type, Endpoint: req.Endpoint, APIKey: req.APIKey,
		})
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"status": "registered"})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleSandboxProvidersPath handles DELETE /providers/{name}.
func (s *Server) handleSandboxProvidersPath(w http.ResponseWriter, r *http.Request) {
	mgr := s.daemon.SandboxManager()
	if mgr == nil {
		http.Error(w, `{"error":"sandbox manager not available"}`, http.StatusServiceUnavailable)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, api.PathDaemonSandboxProvidersPath)
	if name == "" || strings.Contains(name, "/") {
		http.Error(w, `{"error":"provider name required"}`, http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodDelete:
		mgr.ProviderRegistry().Remove(name)
		sandbox.RemoveProviderFromFile("", name)
		json.NewEncoder(w).Encode(map[string]string{"status": "removed"})
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleSandboxProfiles lists profiles (GET) or adds one (POST).
func (s *Server) handleSandboxProfiles(w http.ResponseWriter, r *http.Request) {
	mgr := s.daemon.SandboxManager()
	if mgr == nil {
		http.Error(w, `{"error":"sandbox manager not available"}`, http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		names := mgr.ListProfiles()
		type profileView struct {
			Name     string `json:"name"`
			Provider string `json:"provider"`
			Template string `json:"template"`
		}
		view := make([]profileView, 0, len(names))
		for _, name := range names {
			if p, ok := mgr.GetProfile(name); ok {
				view = append(view, profileView{Name: name, Provider: p.Provider, Template: p.Template})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(view)

	case http.MethodPost:
		var req struct {
			Name     string `json:"name"`
			Provider string `json:"provider"`
			Template string `json:"template"`
			Network  *bool  `json:"network,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
		if req.Name == "" || req.Provider == "" || req.Template == "" {
			http.Error(w, `{"error":"name, provider, and template are required"}`, http.StatusBadRequest)
			return
		}

		profile := &sandbox.SandboxProfile{
			Name: req.Name, Provider: req.Provider,
			Template: req.Template, Network: req.Network,
		}
		if err := mgr.RegisterProfile(profile); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusConflict)
			return
		}
		pe := sandbox.ProfileEntry{Provider: req.Provider, Template: req.Template, Network: req.Network}
		sandbox.WriteProfileToFile("", req.Name, pe)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"status": "created"})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleSandboxProfilesPath handles PUT (edit) and DELETE /profiles/{name}.
func (s *Server) handleSandboxProfilesPath(w http.ResponseWriter, r *http.Request) {
	mgr := s.daemon.SandboxManager()
	if mgr == nil {
		http.Error(w, `{"error":"sandbox manager not available"}`, http.StatusServiceUnavailable)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, api.PathDaemonSandboxProfilesPath)
	if name == "" || strings.Contains(name, "/") {
		http.Error(w, `{"error":"profile name required"}`, http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Provider *string `json:"provider,omitempty"`
			Template *string `json:"template,omitempty"`
			Network  *bool   `json:"network,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}

		existing, ok := mgr.GetProfile(name)
		if !ok {
			http.Error(w, `{"error":"profile not found"}`, http.StatusNotFound)
			return
		}
		if req.Provider != nil {
			existing.Provider = *req.Provider
		}
		if req.Template != nil {
			existing.Template = *req.Template
		}
		if req.Network != nil {
			existing.Network = req.Network
		}

		// Re-register to persist update
		mgr.RegisterProfile(existing)
		pe := sandbox.ProfileEntry{Provider: existing.Provider, Template: existing.Template, Network: existing.Network}
		sandbox.WriteProfileToFile("", name, pe)
		json.NewEncoder(w).Encode(map[string]string{"status": "updated"})

	case http.MethodDelete:
		mgr.RemoveProfile(name)
		sandbox.RemoveProfileFromFile("", name)
		json.NewEncoder(w).Encode(map[string]string{"status": "removed"})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleSandboxDefaults handles GET/PUT /defaults.
func (s *Server) handleSandboxDefaults(w http.ResponseWriter, r *http.Request) {
	mgr := s.daemon.SandboxManager()
	if mgr == nil {
		http.Error(w, `{"error":"sandbox manager not available"}`, http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Return current defaults from Handler
		json.NewEncoder(w).Encode(map[string]string{
			"builtin": "restricted-shell", // TODO: read from actual handler
			"process": "restricted-shell",
		})

	case http.MethodPut:
		var req struct {
			Builtin *string `json:"builtin,omitempty"`
			Process *string `json:"process,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
		def := &sandbox.DefaultMappings{}
		if req.Builtin != nil {
			def.Builtin = sandbox.ProfileRef{Profile: *req.Builtin}
		}
		if req.Process != nil {
			def.Process = sandbox.ProfileRef{Profile: *req.Process}
		}
		// Persist
		de := &sandbox.DefaultMappingsEntry{}
		if req.Builtin != nil {
			de.Builtin = sandbox.ProfileRefEntry{Profile: *req.Builtin}
		}
		if req.Process != nil {
			de.Process = sandbox.ProfileRefEntry{Profile: *req.Process}
		}
		sandbox.WriteDefaultsToFile("", de)
		json.NewEncoder(w).Encode(def)

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleSandboxReload handles POST /reload.
func (s *Server) handleSandboxReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	mgr := s.daemon.SandboxManager()
	if mgr == nil {
		http.Error(w, `{"error":"sandbox manager not available"}`, http.StatusServiceUnavailable)
		return
	}

	sf, err := sandbox.LoadSandboxFile()
	if err != nil {
		http.Error(w, `{"error":"load sandbox.yaml: `+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	if sf == nil {
		http.Error(w, `{"error":"sandbox.yaml not found"}`, http.StatusNotFound)
		return
	}

	_, err = mgr.ReloadFromConfig(sf)
	if err != nil {
		http.Error(w, `{"error":"reload failed: `+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "reloaded"})
}

// handleSandboxStatus handles GET /status.
func (s *Server) handleSandboxStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	mgr := s.daemon.SandboxManager()
	if mgr == nil {
		http.Error(w, `{"error":"sandbox manager not available"}`, http.StatusServiceUnavailable)
		return
	}

	type providerInfo struct {
		Name string          `json:"name"`
		Type sandbox.SandboxType `json:"type"`
	}
	providers := make([]providerInfo, 0)
	for _, p := range mgr.ProviderRegistry().List() {
		providers = append(providers, providerInfo{Name: p.Name(), Type: p.Type()})
	}

	type profileInfo struct {
		Name     string `json:"name"`
		Provider string `json:"provider"`
		Template string `json:"template"`
	}
	profiles := make([]profileInfo, 0)
	for _, n := range mgr.ListProfiles() {
		if p, ok := mgr.GetProfile(n); ok {
			profiles = append(profiles, profileInfo{Name: n, Provider: p.Provider, Template: p.Template})
		}
	}

	json.NewEncoder(w).Encode(map[string]any{
		"providers": providers,
		"profiles":  profiles,
	})
}
