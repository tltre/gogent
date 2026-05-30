package mgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tltre/gogent/internal/api"
)

// AppClient is an HTTP client for querying a running agent's management API.
// Each running agent exposes an mgmt server on its own port (via Listen).
// AppClient connects to one specific agent's port and calls app-level endpoints
// (registry, health, logs, etc.).
type AppClient struct {
	baseURL string
	http    *http.Client
}

// NewAppClient creates an AppClient pointing at the given agent port (e.g. ":9091").
func NewAppClient(port string) *AppClient {
	return &AppClient{
		baseURL: "http://127.0.0.1" + port,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Registry returns the agent's component list.
func (c *AppClient) Registry() ([]api.ComponentInfo, error) {
	var list []api.ComponentInfo
	err := c.get(api.PathAppRegistry, &list)
	return list, err
}

// Health returns health check results for all agent components.
func (c *AppClient) Health() ([]api.HealthResult, error) {
	var results []api.HealthResult
	err := c.get(api.PathAppHealth, &results)
	return results, err
}

// Info returns agent info.
func (c *AppClient) Info() (*api.InfoResponse, error) {
	var info api.InfoResponse
	err := c.get(api.PathAppInfo, &info)
	return &info, err
}

// Logs opens an SSE stream for the agent's log output.
func (c *AppClient) Logs(ctx context.Context) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+api.PathAppLogs, nil)
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

// ToolsExec executes a tool through the agent's tool manager.
func (c *AppClient) ToolsExec(toolName string, params map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(map[string]any{"tool": toolName, "params": params})
	var result map[string]any
	err := c.post(api.PathAppToolsExec, body, &result)
	return result, err
}

// Sessions returns the agent's active session IDs.
func (c *AppClient) Sessions() ([]string, error) {
	var list []string
	err := c.get(api.PathAppSessions, &list)
	return list, err
}

// AgentRun sends a run request to the agent core.
func (c *AppClient) AgentRun(messages []api.AgentMessage, ctx map[string]any) (*api.AgentRunResponse, error) {
	body, _ := json.Marshal(api.AgentRunRequest{
		Messages: messages,
		Context:  ctx,
	})
	var result api.AgentRunResponse
	err := c.post(api.PathAppAgentRun, body, &result)
	return &result, err
}

// Ping performs a lightweight health check against the agent's info endpoint.
func (c *AppClient) Ping() error {
	_, err := c.Info()
	return err
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func (c *AppClient) get(path string, result any) error {
	resp, err := c.http.Get(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("app mgmt request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if len(body) > 0 {
			return fmt.Errorf("app mgmt request failed: %d - %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return fmt.Errorf("app mgmt request failed: %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(result)
}

func (c *AppClient) post(path string, body []byte, result any) error {
	resp, err := c.http.Post(c.baseURL+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("app mgmt request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		if len(respBody) > 0 {
			return fmt.Errorf("app mgmt request failed: %d - %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
		}
		return fmt.Errorf("app mgmt request failed: %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(result)
}
