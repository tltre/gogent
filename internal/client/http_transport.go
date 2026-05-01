package client

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

type HTTPTransportConfig struct {
	Endpoint  string
	Timeout   time.Duration
	Component string
}

type HTTPTransport struct {
	cfg         HTTPTransportConfig
	tr          transport.Interface
	requestID   atomic.Int64
	initialized atomic.Bool
}

func NewHTTPTransport(cfg HTTPTransportConfig) *HTTPTransport {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &HTTPTransport{cfg: cfg}
}

func (t *HTTPTransport) Start(ctx context.Context) error {
	tr, err := transport.NewStreamableHTTP(t.cfg.Endpoint,
		transport.WithHTTPTimeout(t.cfg.Timeout),
	)
	if err != nil {
		return fmt.Errorf("create http transport: %w", err)
	}
	t.tr = tr

	if err := t.tr.Start(ctx); err != nil {
		return fmt.Errorf("start http transport: %w", err)
	}
	return t.handshake(ctx)
}

func (t *HTTPTransport) Close() error {
	if t.tr != nil {
		return t.tr.Close()
	}
	return nil
}

func (t *HTTPTransport) Call(ctx context.Context, method string, params any, result any) error {
	if !t.initialized.Load() {
		return fmt.Errorf("%w: transport not started", ErrTransportClosed)
	}

	resp, err := t.sendRequest(ctx, method, params)
	if err != nil {
		return fmt.Errorf("call %s: %w", method, err)
	}

	return unmarshalResult(resp.Result, result)
}

func (t *HTTPTransport) handshake(ctx context.Context) error {
	req := InitRequest{
		ComponentType:   t.cfg.Component,
		ProtocolVersion: ProtocolVersion,
	}

	resp, err := t.sendRequest(ctx, "initialize", req)
	if err != nil {
		return fmt.Errorf("handshake: %w", err)
	}

	var initResp InitResponse
	if err := json.Unmarshal(resp.Result, &initResp); err != nil {
		return fmt.Errorf("parse init response: %w", err)
	}

	t.initialized.Store(true)
	return nil
}

func (t *HTTPTransport) sendRequest(ctx context.Context, method string, params any) (*transport.JSONRPCResponse, error) {
	id := t.requestID.Add(1)
	req := transport.JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(id),
		Method:  method,
		Params:  params,
	}

	resp, err := t.tr.SendRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}
	if resp.Error != nil {
		return nil, resp.Error.AsError()
	}

	return resp, nil
}
