package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tltre/gogent/internal/api"
	"github.com/tltre/gogent/internal/daemon/tool"
)

// DaemonClient is an HTTP client for communicating with the daemon process's
// management API. It calls daemon-level endpoints (load/list/stop apps,
// component health, etc.).
type DaemonClient struct {
	baseURL string
	http    *http.Client
}

// NewDaemonClient creates a DaemonClient pointing at the daemon's HTTP port
// (e.g. ":9090").
func NewDaemonClient(port string) *DaemonClient {
	return &DaemonClient{
		baseURL: "http://127.0.0.1" + port,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// LoadApp sends a POST request to load a new app into the daemon.
func (c *DaemonClient) LoadApp(configPath string, needForkApplication ...bool) (*api.AppInfo, error) {
	req := api.LoadAppRequest{ConfigPath: configPath}
	if len(needForkApplication) > 0 {
		nf := needForkApplication[0]
		req.NeedForkApplication = &nf
	}
	body, _ := json.Marshal(req)
	var result api.AppInfo
	err := c.post(api.PathDaemonApps, body, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ListApps lists all registered apps from the daemon.
func (c *DaemonClient) ListApps() ([]api.AppInfo, error) {
	var list []api.AppInfo
	err := c.get(api.PathDaemonApps, &list)
	return list, err
}

// GetApp returns a single app by name.
func (c *DaemonClient) GetApp(name string) (*api.AppInfo, error) {
	var info api.AppInfo
	err := c.get(api.PathDaemonAppsPath+name, &info)
	return &info, err
}

// StopApp stops and removes an app by name.
func (c *DaemonClient) StopApp(name string) error {
	return c.delete(api.PathDaemonAppsPath + name)
}

// RestartApp restarts an app by name.
func (c *DaemonClient) RestartApp(name string) (*api.AppInfo, error) {
	var info api.AppInfo
	err := c.post(api.PathDaemonAppsPath+name+"/restart", nil, &info)
	return &info, err
}

// AppStatus returns the current status of an app by name.
func (c *DaemonClient) AppStatus(name string) (*api.AppInfo, error) {
	return c.GetApp(name)
}

// ComponentsHealth returns health check results for daemon-managed components.
func (c *DaemonClient) ComponentsHealth(appName string) ([]api.ComponentHealthResult, error) {
	var results []api.ComponentHealthResult
	path := api.PathDaemonComponentsHealth
	if appName != "" {
		path += "?app=" + appName
	}
	err := c.get(path, &results)
	return results, err
}

// Info returns daemon info.
func (c *DaemonClient) Info() (*api.InfoResponse, error) {
	var info api.InfoResponse
	err := c.get(api.PathDaemonInfo, &info)
	return &info, err
}

// ToolsList returns all registered tool definitions.
func (c *DaemonClient) ToolsList() ([]tool.ToolDefinition, error) {
	var list []tool.ToolDefinition
	err := c.get(api.PathDaemonTools, &list)
	return list, err
}

// ToolsStatus returns status details for a single tool.
func (c *DaemonClient) ToolsStatus(name string) (map[string]any, error) {
	var result map[string]any
	err := c.get(api.PathDaemonToolsPath+name, &result)
	return result, err
}

// ToolRegister registers a new tool definition with the daemon.
func (c *DaemonClient) ToolRegister(req *api.RegisterToolRequest) error {
	body, _ := json.Marshal(req)
	return c.post(api.PathDaemonTools, body, nil)
}

// ToolUnregister removes a tool definition. If force is true, skips the
// safety check for apps currently using the tool.
func (c *DaemonClient) ToolUnregister(name string, force bool) error {
	path := api.PathDaemonToolsPath + name
	if force {
		path += "?force=true"
	}
	return c.delete(path)
}

// ToolRestart restarts a registered MCP server.
func (c *DaemonClient) ToolRestart(name string) error {
	return c.post(api.PathDaemonToolsRestart+name+"/restart", nil, nil)
}

// Ping performs a lightweight health check.
func (c *DaemonClient) Ping() error {
	_, err := c.Info()
	return err
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func (c *DaemonClient) get(path string, result any) error {
	resp, err := c.http.Get(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("daemon mgmt request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		if len(respBody) > 0 {
			return fmt.Errorf("daemon mgmt request failed: %d - %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
		}
		return fmt.Errorf("daemon mgmt request failed: %d", resp.StatusCode)
	}
	if result == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(result)
}

func (c *DaemonClient) post(path string, body []byte, result any) error {
	resp, err := c.http.Post(c.baseURL+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("daemon mgmt request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		if len(respBody) > 0 {
			return fmt.Errorf("daemon mgmt request failed: %d - %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
		}
		return fmt.Errorf("daemon mgmt request failed: %d", resp.StatusCode)
	}

	if result == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(result)
}

func (c *DaemonClient) delete(path string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("daemon mgmt request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("daemon mgmt request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		if len(body) > 0 {
			return fmt.Errorf("daemon mgmt request failed: %d - %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return fmt.Errorf("daemon mgmt request failed: %d", resp.StatusCode)
	}
	return nil
}
