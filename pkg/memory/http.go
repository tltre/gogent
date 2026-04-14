package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type HttpMemoryConfig struct {
	Name     string
	Endpoint string
	Timeout  time.Duration
}

type HttpMemory struct {
	config *HttpMemoryConfig
	client *http.Client
}

func NewHttpMemory(cfg *HttpMemoryConfig) *HttpMemory {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &HttpMemory{
		config: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

func (m *HttpMemory) Add(ctx context.Context, item MemoryItem) error {
	body, err := json.Marshal(item)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.config.Endpoint+"/memory", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http error: %d - %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (m *HttpMemory) AddBatch(ctx context.Context, items []MemoryItem) error {
	body, err := json.Marshal(items)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.config.Endpoint+"/memory/batch", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http error: %d - %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (m *HttpMemory) Query(ctx context.Context, q Query) ([]MemoryItem, error) {
	body, err := json.Marshal(q)
	if err != nil {
		return []MemoryItem{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.config.Endpoint+"/memory/query", bytes.NewReader(body))
	if err != nil {
		return []MemoryItem{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return []MemoryItem{}, err
	}
	defer resp.Body.Close()

	var items []MemoryItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return []MemoryItem{}, err
	}

	return items, nil
}

func (m *HttpMemory) Get(ctx context.Context, id string) (MemoryItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.config.Endpoint+"/memory/"+id, nil)
	if err != nil {
		return MemoryItem{}, err
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return MemoryItem{}, err
	}
	defer resp.Body.Close()

	var item MemoryItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return MemoryItem{}, err
	}

	return item, nil
}

func (m *HttpMemory) Delete(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, m.config.Endpoint+"/memory/"+id, nil)
	if err != nil {
		return err
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (m *HttpMemory) Clear(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, m.config.Endpoint+"/memory", nil)
	if err != nil {
		return err
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (m *HttpMemory) Count(ctx context.Context) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.config.Endpoint+"/memory/count", nil)
	if err != nil {
		return 0, err
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		Count int64 `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	return result.Count, nil
}
