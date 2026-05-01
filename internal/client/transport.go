package client

import (
	"context"
	"encoding/json"
	"fmt"
)

const ProtocolVersion = "0.1.0"

type Transport interface {
	Start(ctx context.Context) error
	Close() error
	Call(ctx context.Context, method string, params any, result any) error
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
