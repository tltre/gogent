package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.Reader
	mu     sync.Mutex
	nextID int
}

func NewClient(command ...string) (*Client, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("command required")
	}

	cmd := exec.Command(command[0], command[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	return &Client{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		nextID: 1,
	}, nil
}

func (c *Client) Start(ctx context.Context) error {
	return c.cmd.Start()
}

func (c *Client) Stop(ctx context.Context) error {
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd.Process != nil {
		return c.cmd.Process.Kill()
	}
	return nil
}

func (c *Client) Initialize(ctx context.Context) (*InitializeResult, error) {
	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities: ClientCapabilities{
			Roots: &RootsCapability{ListChanged: true},
		},
		ClientInfo: ImplementationInfo{
			Name:    "gagent",
			Version: "1.0.0",
		},
	}

	resp, err := c.sendRequest("initialize", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("initialize error: %s", resp.Error.Message)
	}

	var result InitializeResult
	if err := json.Unmarshal(toJSON(resp.Result), &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *Client) ListTools(ctx context.Context) (*ToolListResult, error) {
	resp, err := c.sendRequest("tools/list", nil)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("list tools error: %s", resp.Error.Message)
	}

	var result ToolListResult
	if err := json.Unmarshal(toJSON(resp.Result), &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (*CallToolResult, error) {
	params := CallToolParams{
		Name:      name,
		Arguments: arguments,
	}

	resp, err := c.sendRequest("tools/call", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("call tool error: %s", resp.Error.Message)
	}

	var result CallToolResult
	if err := json.Unmarshal(toJSON(resp.Result), &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *Client) sendRequest(method string, params any) (*Response, error) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	c.mu.Unlock()

	var paramsBytes json.RawMessage
	if params != nil {
		p, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		paramsBytes = p
	}

	req := Request{
		JSONRPC: JSONRPC20,
		ID:      id,
		Method:  method,
		Params:  paramsBytes,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	line := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(reqBytes), reqBytes)

	c.mu.Lock()
	_, err = c.stdin.Write([]byte(line))
	c.mu.Unlock()

	if err != nil {
		return nil, err
	}

	return c.readResponse()
}

func (c *Client) readResponse() (*Response, error) {
	reader := bufio.NewReader(c.stdout)

	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}

	if !hasContentLengthPrefix(line) {
		return nil, fmt.Errorf("invalid response format")
	}

	length := parseContentLength(line)
	if length <= 0 {
		return nil, fmt.Errorf("invalid content length")
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if line == "\r\n" {
			break
		}
	}

	body := make([]byte, length)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}

	var resp Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

func hasContentLengthPrefix(line string) bool {
	return len(line) >= 16 && line[:16] == "Content-Length: "
}

func parseContentLength(line string) int {
	if len(line) < 16 {
		return 0
	}
	var length int
	fmt.Sscanf(line[16:], "%d", &length)
	return length
}

func toJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
