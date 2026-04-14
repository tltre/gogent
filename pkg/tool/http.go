package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type HttpToolConfig struct {
	Name        string
	Description string
	Endpoint    string
	Timeout     time.Duration
}

type HttpTool struct {
	config *HttpToolConfig
	client *http.Client
}

func NewHttpTool(cfg *HttpToolConfig) *HttpTool {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &HttpTool{
		config: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

func (t *HttpTool) Info() ToolInfo {
	return ToolInfo{
		Name:        t.config.Name,
		Description: t.config.Description,
	}
}

func (t *HttpTool) Execute(ctx context.Context, params map[string]any) (Result, error) {
	body, err := json.Marshal(params)
	if err != nil {
		return Result{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.config.Endpoint+"/execute", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return Result{IsError: true, ErrorMsg: fmt.Sprintf("http error: %d - %s", resp.StatusCode, string(respBody))}, nil
	}

	var result Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Result{}, err
	}

	return result, nil
}

func (t *HttpTool) Stream(ctx context.Context, params map[string]any) (<-chan StreamChunk, error) {
	body, err := json.Marshal(params)
	if err != nil {
		ch := make(chan StreamChunk)
		close(ch)
		return ch, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.config.Endpoint+"/stream", bytes.NewReader(body))
	if err != nil {
		ch := make(chan StreamChunk)
		close(ch)
		return ch, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		ch := make(chan StreamChunk)
		close(ch)
		return ch, err
	}

	ch := make(chan StreamChunk, 100)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		decoder := json.NewDecoder(resp.Body)
		for {
			var chunk StreamChunk
			if err := decoder.Decode(&chunk); err != nil {
				if err == io.EOF {
					return
				}
				ch <- StreamChunk{Done: true, Error: err}
				return
			}
			ch <- chunk
			if chunk.Done {
				return
			}
		}
	}()

	return ch, nil
}
