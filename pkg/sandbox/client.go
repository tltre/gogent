// Package sandbox provides the App-side thin SandboxManager client.
//
// It communicates with the Daemon's management API to manage sandbox
// lifecycle (create, list, get, destroy) and execute commands within
// sandboxes. This is the App-side counterpart of
// internal/daemon/sandbox/ (the daemon-side implementation).
package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// SandboxInfo
// ---------------------------------------------------------------------------

// SandboxInfo describes a sandbox instance from the App's perspective.
type SandboxInfo struct {
	Name    string `json:"name"`
	Profile string `json:"profile"`
	Status  string `json:"status,omitempty"`
}

// ---------------------------------------------------------------------------
// Create request
// ---------------------------------------------------------------------------

// CreateRequest is the payload for creating a new sandbox.
type CreateRequest struct {
	Name      string        `json:"name"`
	Profile   string        `json:"profile"`
	Lifecycle string        `json:"lifecycle,omitempty"` // "persistent" | "ephemeral"
	Timeout   time.Duration `json:"timeout,omitempty"`
}

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

// Client is the App-side thin sandbox manager client.
// It communicates with the Daemon's management API over HTTP.
type Client struct {
	baseURL    string
	appName    string
	httpClient *http.Client
}

// NewClient creates a new SandboxManager client.
// The daemonAddr is the Daemon's management HTTP address (e.g. "127.0.0.1:9090").
func NewClient(daemonAddr, appName string) *Client {
	return &Client{
		baseURL: fmt.Sprintf("http://%s/api/v1/agents/%s/sandboxes", daemonAddr, appName),
		appName: appName,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// List returns all sandbox instances for this app.
func (c *Client) List(ctx context.Context) ([]SandboxInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("list sandboxes: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list sandboxes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list sandboxes: status %d: %s", resp.StatusCode, string(body))
	}

	var result []SandboxInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("list sandboxes: decode: %w", err)
	}
	return result, nil
}

// Create creates a new sandbox instance.
func (c *Client) Create(ctx context.Context, req CreateRequest) (*SandboxInfo, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("create sandbox: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create sandbox: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("create sandbox: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		rbody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create sandbox: status %d: %s", resp.StatusCode, string(rbody))
	}

	var result SandboxInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("create sandbox: decode: %w", err)
	}
	return &result, nil
}

// Get returns details for a specific sandbox.
func (c *Client) Get(ctx context.Context, name string) (*SandboxInfo, error) {
	url := c.baseURL + "/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("get sandbox: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get sandbox: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get sandbox: status %d: %s", resp.StatusCode, string(body))
	}

	var result SandboxInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("get sandbox: decode: %w", err)
	}
	return &result, nil
}

// Destroy removes a sandbox instance.
func (c *Client) Destroy(ctx context.Context, name string) error {
	url := c.baseURL + "/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("destroy sandbox: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("destroy sandbox: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("destroy sandbox: status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// execWire is the JSON wire format for ExecResult.
type execWire struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitcode"`
	Error    string `json:"error,omitempty"`
}

// Execute runs a command inside a sandbox and returns the result.
func (c *Client) Execute(ctx context.Context, sandboxName string, req ExecRequest) (ExecResult, error) {
	url := c.baseURL + "/" + sandboxName + "/exec"
	body, err := json.Marshal(req)
	if err != nil {
		return ExecResult{}, fmt.Errorf("execute sandbox: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ExecResult{}, fmt.Errorf("execute sandbox: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ExecResult{}, fmt.Errorf("execute sandbox: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		rbody, _ := io.ReadAll(resp.Body)
		return ExecResult{}, fmt.Errorf("execute sandbox: status %d: %s", resp.StatusCode, string(rbody))
	}

	var wire execWire
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return ExecResult{}, fmt.Errorf("execute sandbox: decode: %w", err)
	}

	var errVal error
	if wire.Error != "" {
		errVal = fmt.Errorf("%s", wire.Error)
	}
	return ExecResult{
		Stdout:   wire.Stdout,
		Stderr:   wire.Stderr,
		ExitCode: wire.ExitCode,
		Error:    errVal,
	}, nil
}

// ---------------------------------------------------------------------------
// Proto definition (reference only — implemented via HTTP in v0.13.3)
// ---------------------------------------------------------------------------

// SandboxManager service would be defined in proto as:
//
//	service SandboxManager {
//	    rpc ListSandboxes(ListRequest) returns (ListResponse);
//	    rpc CreateSandbox(CreateSandboxRequest) returns (SandboxInfo);
//	    rpc GetSandbox(GetSandboxRequest) returns (SandboxInfo);
//	    rpc DestroySandbox(DestroySandboxRequest) returns (DestroyResponse);
//	    rpc ExecInSandbox(ExecInSandboxRequest) returns (ExecResult);
//	}
