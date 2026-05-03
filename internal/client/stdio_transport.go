package client

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

type StdioTransportConfig struct {
	Command        string
	Args           []string
	Env            []string
	Component      string
	Logger         Logger
	RequestHandler transport.RequestHandler
}

type StdioTransport struct {
	cfg         StdioTransportConfig
	tr          *transport.Stdio
	log         Logger
	requestID   atomic.Int64
	initialized atomic.Bool

	notifyMu   sync.RWMutex
	notifyFuncs map[string][]func(json.RawMessage)
}

func NewStdioTransport(cfg StdioTransportConfig) *StdioTransport {
	return &StdioTransport{
		cfg:         cfg,
		log:         cfg.Logger,
		notifyFuncs: make(map[string][]func(json.RawMessage)),
	}
}

func (t *StdioTransport) Start(ctx context.Context) error {
	t.tr = transport.NewStdio(t.cfg.Command, t.cfg.Env, t.cfg.Args...)

	if t.cfg.RequestHandler != nil {
		t.tr.SetRequestHandler(t.cfg.RequestHandler)
	}

	t.tr.SetNotificationHandler(func(notification mcp.JSONRPCNotification) {
		t.notifyMu.RLock()
		defer t.notifyMu.RUnlock()
		if handlers, ok := t.notifyFuncs[notification.Method]; ok {
			params := buildNotifyParams(notification.Params)
			for _, h := range handlers {
				h(params)
			}
		}
	})

	if err := t.tr.Start(ctx); err != nil {
		return fmt.Errorf("start stdio process: %w", err)
	}
	if err := t.handshake(ctx); err != nil {
		return err
	}
	t.sendServicesAnnounce(ctx)
	return nil
}

func (t *StdioTransport) Close() error {
	if t.tr != nil {
		return t.tr.Close()
	}
	return nil
}

func (t *StdioTransport) Call(ctx context.Context, method string, params any, result any) error {
	if !t.initialized.Load() {
		return fmt.Errorf("%w: transport not started", ErrTransportClosed)
	}

	start := time.Now()
	resp, err := t.sendRequest(ctx, method, params)
	dur := time.Since(start)

	t.logTransport(ctx, method, err, dur)

	if err != nil {
		return fmt.Errorf("call %s: %w", method, err)
	}

	return unmarshalResult(resp.Result, result)
}

func (t *StdioTransport) OnNotify(method string, handler func(params json.RawMessage)) {
	t.notifyMu.Lock()
	defer t.notifyMu.Unlock()
	t.notifyFuncs[method] = append(t.notifyFuncs[method], handler)
}

func (t *StdioTransport) handshake(ctx context.Context) error {
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

func (t *StdioTransport) sendServicesAnnounce(ctx context.Context) {
	_ = t.tr.SendNotification(ctx, mcp.JSONRPCNotification{
		JSONRPC: mcp.JSONRPC_VERSION,
		Notification: mcp.Notification{
			Method: "services/announce",
			Params: mcp.NotificationParams{
				AdditionalFields: map[string]any{
					"logger":   map[string]any{"topic": "system.log"},
					"eventbus": map[string]any{"available": true},
				},
			},
		},
	})
}

func (t *StdioTransport) sendRequest(ctx context.Context, method string, params any) (*transport.JSONRPCResponse, error) {
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

func (t *StdioTransport) logTransport(ctx context.Context, method string, callErr error, dur time.Duration) {
	if t.log == nil {
		return
	}
	level := InfoLevel
	msg := fmt.Sprintf("%s OK", method)
	if callErr != nil {
		level = ErrorLevel
		msg = fmt.Sprintf("%s FAIL", method)
	}
	t.log.Log(ctx, LogEntry{
		Level:    level,
		Module:   "transport",
		Message:  msg,
		Duration: dur,
		Fields: []Field{
			{Key: "method", Value: method},
			{Key: "transport", Value: "stdio"},
		},
	})
}

func buildNotifyParams(params mcp.NotificationParams) json.RawMessage {
	raw := make(map[string]any)
	if params.Meta != nil {
		raw["_meta"] = params.Meta
	}
	for k, v := range params.AdditionalFields {
		raw[k] = v
	}
	data, _ := json.Marshal(raw)
	return data
}
