package main

// FIXME: this entire file is a mock daemon for smoke-testing only.
// All 40+ response strings are hardcoded placeholders. Not for production use.
// Each dispatch function returns stub data; real daemons must implement
// actual business logic for each component type.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	componentType := os.Getenv("GAGENT_DAEMON_TYPE")
	if componentType == "" {
		componentType = "agentcore"
	}

	tr := transport.NewIO(os.Stdin, os.Stdout, nil)
	tr.SetRequestHandler(handler(componentType, tr))
	if err := tr.Start(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "daemon: start:", err)
		os.Exit(1)
	}
	select {}
}

func handler(componentType string, tr *transport.Stdio) transport.RequestHandler {
	return func(ctx context.Context, req transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
		if req.Method == "services/announce" {
			sendAck(ctx, tr, componentType, "announce received")
			return result(req.ID, map[string]any{"ok": true})
		}
		switch req.Method {
		case "initialize":
			return initResp(req.ID, componentType)
		default:
			resp, err := dispatch(componentType, req)
			if err == nil && resp != nil {
				sendAck(ctx, tr, componentType, req.Method)
			}
			return resp, err
		}
	}
}

func initResp(id mcp.RequestId, componentType string) (*transport.JSONRPCResponse, error) {
	raw, _ := json.Marshal(map[string]any{
		"protocolVersion": "0.1.0",
		"componentType":   componentType,
		"methods":         methodList(componentType),
	})
	return transport.NewJSONRPCResultResponse(id, raw), nil
}

func dispatch(componentType string, req transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	switch componentType {
	case "agentcore":
		return agentDispatch(req)
	case "provider":
		return providerDispatch(req)
	case "tool":
		return toolDispatch(req)
	case "memory":
		return memoryDispatch(req)
	case "channel":
		return channelDispatch(req)
	case "hook":
		return hookDispatch(req)
	case "eventbus":
		return eventbusDispatch(req)
	case "contextmanager":
		return contextDispatch(req)
	case "sandbox":
		return sandboxDispatch(req)
	default:
		return rpcErr(req.ID)
	}
}

func agentDispatch(req transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	switch req.Method {
	case "agent/run":
		return result(req.ID, map[string]any{
			"response": map[string]any{
				"role":    "assistant",
				"content": "hello from daemon",
			},
		})
	case "agent/stream":
		return result(req.ID, nil)
	default:
		return rpcErr(req.ID)
	}
}

func providerDispatch(req transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	switch req.Method {
	case "provider/generate":
		return result(req.ID, map[string]any{"content": "hello from daemon", "finishReason": "stop"})
	case "provider/stream":
		return result(req.ID, nil)
	case "provider/modelInfo":
		return result(req.ID, map[string]any{"name": "daemon-model", "provider": "test", "supportsTool": true})
	default:
		return rpcErr(req.ID)
	}
}

func toolDispatch(req transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	switch req.Method {
	case "tools/list":
		return result(req.ID, []map[string]any{{"name": "echo", "description": "echo input"}})
	case "tools/call":
		return result(req.ID, map[string]any{"output": "daemon-echoed", "isError": false})
	default:
		return rpcErr(req.ID)
	}
}

func memoryDispatch(req transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	switch req.Method {
	case "memory/add":
		return result(req.ID, nil)
	case "memory/query":
		return result(req.ID, []map[string]any{{"id": "1", "content": "remembered", "score": 0.9}})
	case "memory/get":
		return result(req.ID, map[string]any{"id": "1", "content": "found"})
	case "memory/delete":
		return result(req.ID, nil)
	case "memory/clear":
		return result(req.ID, nil)
	case "memory/count":
		return result(req.ID, int64(5))
	default:
		return rpcErr(req.ID)
	}
}

func channelDispatch(req transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	switch req.Method {
	case "channel/send":
		return result(req.ID, nil)
	default:
		return rpcErr(req.ID)
	}
}

func hookDispatch(req transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	switch req.Method {
	case "hook/onEvent":
		return result(req.ID, nil)
	default:
		return rpcErr(req.ID)
	}
}

func eventbusDispatch(req transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	switch req.Method {
	case "eventbus/publish":
		return result(req.ID, nil)
	case "eventbus/subscribe":
		return result(req.ID, map[string]any{"subscriptionId": "sub-1"})
	case "eventbus/unsubscribe":
		return result(req.ID, nil)
	default:
		return rpcErr(req.ID)
	}
}

func contextDispatch(req transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	switch req.Method {
	case "context/newSession":
		return result(req.ID, "sess-daemon")
	case "context/addMessage":
		return result(req.ID, nil)
	case "context/getMessages":
		return result(req.ID, []map[string]any{{"role": "user", "content": "hi from daemon"}})
	case "context/getSummary":
		return result(req.ID, map[string]any{"content": "summary from daemon"})
	case "context/buildSystemPrompt":
		return result(req.ID, "You are a daemon assistant.")
	case "context/clear":
		return result(req.ID, nil)
	case "context/deleteSession":
		return result(req.ID, nil)
	default:
		return rpcErr(req.ID)
	}
}

func sandboxDispatch(req transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	switch req.Method {
	case "sandbox/create":
		return result(req.ID, "sb-daemon")
	case "sandbox/destroy":
		return result(req.ID, nil)
	case "sandbox/execute":
		return result(req.ID, map[string]any{"stdout": "daemon-output", "stderr": "", "exitCode": 0})
	case "sandbox/setLimits":
		return result(req.ID, nil)
	case "sandbox/getLimits":
		return result(req.ID, map[string]any{"maxMemoryMB": 512, "maxCPUTime": "10s", "networkAccess": false})
	default:
		return rpcErr(req.ID)
	}
}

func methodList(componentType string) []string {
	switch componentType {
	case "agentcore":
		return []string{"agent/run", "agent/stream"}
	case "provider":
		return []string{"provider/generate", "provider/stream", "provider/modelInfo"}
	case "tool":
		return []string{"tools/list", "tools/call"}
	case "memory":
		return []string{"memory/add", "memory/addBatch", "memory/query", "memory/get", "memory/delete", "memory/clear", "memory/count"}
	case "channel":
		return []string{"channel/send"}
	case "hook":
		return []string{"hook/onEvent"}
	case "eventbus":
		return []string{"eventbus/publish", "eventbus/subscribe", "eventbus/unsubscribe"}
	case "contextmanager":
		return []string{"context/newSession", "context/addMessage", "context/getMessages", "context/getSummary", "context/buildSystemPrompt", "context/clear", "context/deleteSession"}
	case "sandbox":
		return []string{"sandbox/create", "sandbox/destroy", "sandbox/execute", "sandbox/setLimits", "sandbox/getLimits"}
	default:
		return nil
	}
}

func sendAck(ctx context.Context, tr *transport.Stdio, componentType, method string) {
	_ = tr.SendNotification(ctx, mcp.JSONRPCNotification{
		JSONRPC: mcp.JSONRPC_VERSION,
		Notification: mcp.Notification{
			Method: "logger/log",
			Params: mcp.NotificationParams{
				AdditionalFields: map[string]any{
					"entry": map[string]any{
						"level":   "INFO",
						"module":  componentType,
						"message": fmt.Sprintf("%s OK from daemon", method),
					},
				},
			},
		},
	})
}

func result(id mcp.RequestId, v any) (*transport.JSONRPCResponse, error) {
	raw, _ := json.Marshal(v)
	return transport.NewJSONRPCResultResponse(id, raw), nil
}

func rpcErr(id mcp.RequestId) (*transport.JSONRPCResponse, error) {
	return transport.NewJSONRPCErrorResponse(id, -32601, "method not found", nil), nil
}
