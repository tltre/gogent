package client

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

const ProtocolVersion = "0.1.0"

type LogLevel int8

const (
	InfoLevel  LogLevel = 0
	ErrorLevel LogLevel = 3
)

type Field struct {
	Key   string
	Value any
}

type LogEntry struct {
	Level    LogLevel
	Module   string
	Message  string
	Duration time.Duration
	Fields   []Field
}

type Logger interface {
	Log(ctx context.Context, entry LogEntry)
}

type Transport interface {
	Start(ctx context.Context) error
	Close() error
	Call(ctx context.Context, method string, params any, result any) error
	OnNotify(method string, handler func(params json.RawMessage))
}

type InitRequest struct {
	ComponentType   string `json:"componentType"`
	ProtocolVersion string `json:"protocolVersion"`
}

type InitResponse struct {
	ProtocolVersion string   `json:"protocolVersion"`
	ComponentType   string   `json:"componentType"`
	Methods         []string `json:"methods"`
}

func (r *InitResponse) Supports(method string) bool {
	for _, m := range r.Methods {
		if m == method {
			return true
		}
	}
	return false
}

var (
	ErrTransportClosed = fmt.Errorf("transport closed")
)

func unmarshalResult(raw json.RawMessage, result any) error {
	if result == nil {
		return nil
	}
	return json.Unmarshal(raw, result)
}

type LazyTransport struct {
	tr      Transport
	mu      sync.Mutex
	started bool
}

func WrapLazy(tr Transport) *LazyTransport {
	return &LazyTransport{tr: tr}
}

func (t *LazyTransport) Call(ctx context.Context, method string, params any, result any) error {
	t.mu.Lock()
	if !t.started {
		t.started = true
		t.mu.Unlock()
		if err := t.tr.Start(ctx); err != nil {
			return err
		}
	} else {
		t.mu.Unlock()
	}
	return t.tr.Call(ctx, method, params, result)
}

func (t *LazyTransport) Close() error {
	return t.tr.Close()
}

func (t *LazyTransport) OnNotify(method string, handler func(params json.RawMessage)) {
	t.tr.OnNotify(method, handler)
}
