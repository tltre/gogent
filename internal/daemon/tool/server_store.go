package tool

import (
	"fmt"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client"
)

// ServerInfo holds runtime state for an MCP server (process or http) or a builtin tool.
type ServerInfo struct {
	Name       string
	Driver     string // "builtin" | "process" | "http"
	Command    string // process driver startup command
	Endpoint   string // http driver endpoint
	Env        map[string]string
	DefaultLvl int
	ToolCount  int // number of tools under this server; builtin = 1

	// Runtime state
	Status    ToolStatus
	Client    client.MCPClient // mcp-go client (process/http); builtin = nil
	Pid       int              // process driver PID
	StartedAt time.Time
}

// ServerStore manages all servers (builtin + MCP) in one place.
// Thread-safe.
type ServerStore struct {
	mu      sync.RWMutex
	servers map[string]*ServerInfo
}

// NewServerStore creates an empty server store.
func NewServerStore() *ServerStore {
	return &ServerStore{servers: make(map[string]*ServerInfo)}
}

// Add registers a new server entry. Returns error if name already exists.
func (s *ServerStore) Add(name string, info *ServerInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.servers[name]; exists {
		return fmt.Errorf("server %q already exists", name)
	}
	s.servers[name] = info
	return nil
}

// Get returns a server entry by name.
func (s *ServerStore) Get(name string) (*ServerInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	info, ok := s.servers[name]
	return info, ok
}

// Remove deletes a server entry by name.
func (s *ServerStore) Remove(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.servers, name)
}

// List returns a copy of all server entries.
func (s *ServerStore) List() []*ServerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*ServerInfo, 0, len(s.servers))
	for _, info := range s.servers {
		result = append(result, info)
	}
	return result
}

// SetStatus updates the status of a server entry.
func (s *ServerStore) SetStatus(name string, status ToolStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, exists := s.servers[name]
	if !exists {
		return fmt.Errorf("server %q not found", name)
	}
	info.Status = status
	return nil
}

// Exists checks if a server name is registered.
func (s *ServerStore) Exists(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.servers[name]
	return exists
}
