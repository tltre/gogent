package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type HttpProviderConfig struct {
	Name     string
	Endpoint string
	ApiKey   string
	Model    string
	Timeout  time.Duration
}

type HttpProvider struct {
	config    *HttpProviderConfig
	client    *http.Client
	modelInfo ModelInfo
}

func NewHttpProvider(cfg *HttpProviderConfig) *HttpProvider {
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	return &HttpProvider{
		config: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
		modelInfo: ModelInfo{
			Name:     cfg.Model,
			Provider: "http",
		},
	}
}

func (p *HttpProvider) Generate(ctx context.Context, messages []ProviderMessage) (Response, error) {
	reqBody := map[string]any{
		"messages": messages,
		"model":    p.config.Model,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return Response{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.Endpoint+"/generate", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.ApiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return Response{}, fmt.Errorf("http error: %d - %s", resp.StatusCode, string(respBody))
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Response{}, err
	}

	return result, nil
}

func (p *HttpProvider) Stream(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error) {
	reqBody := map[string]any{
		"messages": messages,
		"model":    p.config.Model,
		"stream":   true,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		ch := make(chan StreamChunk)
		close(ch)
		return ch, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.Endpoint+"/stream", bytes.NewReader(body))
	if err != nil {
		ch := make(chan StreamChunk)
		close(ch)
		return ch, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.ApiKey)

	resp, err := p.client.Do(req)
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
				ch <- StreamChunk{Done: true}
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

func (p *HttpProvider) ModelInfo() ModelInfo {
	return p.modelInfo
}
