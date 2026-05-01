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

type HTTPTransportConfig struct {
	Endpoint  string
	Timeout   time.Duration
	Component string
	Logger    Logger
}

type HTTPTransport struct {
	cfg         HTTPTransportConfig
	tr          transport.Interface
	log         Logger
	requestID   atomic.Int64
	initialized atomic.Bool

	notifyMu   sync.RWMutex
	notifyFuncs map[string][]func(json.RawMessage)
}

func NewHTTPTransport(cfg HTTPTransportConfig) *HTTPTransport {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &HTTPTransport{
		cfg:         cfg,
		log:         cfg.Logger,
		notifyFuncs: make(map[string][]func(json.RawMessage)),
	}
}

func (t *HTTPTransport) Start(ctx context.Context) error {
	tr, err := transport.NewStreamableHTTP(t.cfg.Endpoint,
		transport.WithHTTPTimeout(t.cfg.Timeout),
	)
	if err != nil {
		return fmt.Errorf("create http transport: %w", err)
	}
	t.tr = tr

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
		return fmt.Errorf("start http transport: %w", err)
	}
	if err := t.handshake(ctx); err != nil {
		return err
	}
	t.sendServicesAnnounce(ctx)
	return nil
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

	start := time.Now()
	resp, err := t.sendRequest(ctx, method, params)
	dur := time.Since(start)

	t.logTransport(ctx, method, err, dur)

	if err != nil {
		return fmt.Errorf("call %s: %w", method, err)
	}

	return unmarshalResult(resp.Result, result)
}

func (t *HTTPTransport) OnNotify(method string, handler func(params json.RawMessage)) {
	t.notifyMu.Lock()
	defer t.notifyMu.Unlock()
	t.notifyFuncs[method] = append(t.notifyFuncs[method], handler)
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

func (t *HTTPTransport) sendServicesAnnounce(ctx context.Context) {
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

func (t *HTTPTransport) logTransport(ctx context.Context, method string, callErr error, dur time.Duration) {
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
			{Key: "transport", Value: "http"},
		},
	})
}
