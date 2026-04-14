package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type HttpSandboxConfig struct {
	Name     string
	Endpoint string
	Timeout  time.Duration
}

type HttpSandbox struct {
	config *HttpSandboxConfig
	client *http.Client
	limits ResourceLimits
}

func NewHttpSandbox(cfg *HttpSandboxConfig, limits ResourceLimits) *HttpSandbox {
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	return &HttpSandbox{
		config: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
		limits: limits,
	}
}

func (s *HttpSandbox) Create(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.Endpoint+"/sandbox/create", nil)
	if err != nil {
		return "", err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.ID, nil
}

func (s *HttpSandbox) Destroy(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.config.Endpoint+"/sandbox/"+id, nil)
	if err != nil {
		return err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (s *HttpSandbox) Execute(ctx context.Context, sandboxID string, req ExecRequest) (ExecResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return ExecResult{}, err
	}

	url := fmt.Sprintf("%s/sandbox/%s/execute", s.config.Endpoint, sandboxID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ExecResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return ExecResult{}, err
	}
	defer resp.Body.Close()

	var result ExecResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ExecResult{}, err
	}

	return result, nil
}

func (s *HttpSandbox) SetLimits(limits ResourceLimits) {
	s.limits = limits
}

func (s *HttpSandbox) GetLimits() ResourceLimits {
	return s.limits
}
