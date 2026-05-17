package mgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(port string) *Client {
	return &Client{
		baseURL: "http://127.0.0.1" + port,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) Registry() ([]ComponentInfo, error) {
	var list []ComponentInfo
	err := c.get("/api/v1/registry", &list)
	return list, err
}

func (c *Client) Health() ([]HealthResult, error) {
	var results []HealthResult
	err := c.get("/api/v1/health", &results)
	return results, err
}

func (c *Client) ComponentsHealth(appName string) ([]ComponentHealthResult, error) {
	var results []ComponentHealthResult
	path := "/api/v1/health/components"
	if appName != "" {
		path += "?app=" + appName
	}
	err := c.get(path, &results)
	return results, err
}

func (c *Client) Info() (*InfoResponse, error) {
	var info InfoResponse
	err := c.get("/api/v1/info", &info)
	return &info, err
}

func (c *Client) Logs(ctx context.Context) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v1/logs", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func (c *Client) ToolsExec(toolName string, params map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(map[string]any{"tool": toolName, "params": params})
	var result map[string]any
	err := c.post("/api/v1/tools/exec", body, &result)
	return result, err
}

func (c *Client) Sessions() ([]string, error) {
	var list []string
	err := c.get("/api/v1/sessions", &list)
	return list, err
}

func (c *Client) AgentRun(messages []AgentMessage, ctx map[string]any) (*AgentRunResponse, error) {
	body, _ := json.Marshal(AgentRunRequest{
		Messages: messages,
		Context:  ctx,
	})
	var result AgentRunResponse
	err := c.post("/api/v1/agent/run", body, &result)
	return &result, err
}

func (c *Client) Ping() error {
	_, err := c.Info()
	return err
}

// --- Daemon-level app management methods ---

// LoadApp sends a POST request to load a new app into the daemon.
func (c *Client) LoadApp(configPath string) (*AppInfo, error) {
	body, _ := json.Marshal(LoadAppRequest{ConfigPath: configPath})
	var info AppInfo
	err := c.post("/api/v1/apps", body, &info)
	return &info, err
}

// ListApps returns all running apps from the daemon.
func (c *Client) ListApps() ([]AppInfo, error) {
	var list []AppInfo
	err := c.get("/api/v1/apps", &list)
	return list, err
}

// StopApp sends a DELETE request to stop a running app by name.
func (c *Client) StopApp(name string) error {
	return c.delete("/api/v1/apps/" + name)
}

// RestartApp sends a POST request to restart a running app by name.
func (c *Client) RestartApp(name string) (*AppInfo, error) {
	var info AppInfo
	err := c.post("/api/v1/apps/"+name+"/restart", nil, &info)
	return &info, err
}

// AppStatus returns the current status of an app by name.
func (c *Client) AppStatus(name string) (*AppInfo, error) {
	var info AppInfo
	err := c.get("/api/v1/apps/"+name, &info)
	return &info, err
}

func (c *Client) get(path string, result any) error {
	resp, err := c.http.Get(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("mgmt request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mgmt request failed: %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

func (c *Client) post(path string, body []byte, result any) error {
	resp, err := c.http.Post(c.baseURL+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mgmt request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mgmt request failed: %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

func (c *Client) delete(path string) error {
	req, err := http.NewRequestWithContext(context.Background(), "DELETE", c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("mgmt request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("mgmt request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("mgmt request failed: %d", resp.StatusCode)
	}

	return nil
}
