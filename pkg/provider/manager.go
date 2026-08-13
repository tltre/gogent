package provider

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/logger"
)

// ProviderInfo is the capability declaration of a registered provider,
// presented to end users by the Interface layer.
type ProviderInfo struct {
	Name         string   // engine identifier (registration name), e.g. "openai"
	DisplayName  string   // brand name visible to end users, e.g. "OpenAI"
	DefaultModel string   // engine's default model (ModelInfo.Name); used when switching providers
	Models       []string // models this engine supports (user picks at runtime)
}

// ProviderManager is the unified registry of all provider instances.
//
// It follows the same pattern as hook.HookManager: the manager itself is a
// Component (GetType returns component.ComponentProvider), while individual
// provider instances are plain IProvider objects registered via Register and
// never enter the Registry. This keeps a single query entry point for the
// Interface layer (List) and the AgentCore (Get).
type ProviderManager struct {
	component.BasicComponent
	mu        sync.RWMutex
	providers map[string]IProvider
}

// NewManagerComponent creates a ProviderManager component with the given name.
func NewManagerComponent(name string) *ProviderManager {
	return &ProviderManager{
		BasicComponent: component.NewBasicComponent(name),
		providers:      make(map[string]IProvider),
	}
}

var _ component.Component = (*ProviderManager)(nil)

func (m *ProviderManager) GetType() component.ComponentType {
	return component.ComponentProvider
}

func (m *ProviderManager) Initialize(ctx context.Context, registry *component.Registry) error {
	m.SetRegistry(registry)
	m.log(ctx, logger.InfoLevel, "provider manager initialized")
	return nil
}

func (m *ProviderManager) Start(ctx context.Context) error {
	m.log(ctx, logger.DebugLevel, "provider manager started")
	return nil
}

func (m *ProviderManager) Stop(ctx context.Context) error {
	m.log(ctx, logger.DebugLevel, "provider manager stopped")
	return nil
}

func (m *ProviderManager) Dependencies() map[string]component.DependencySpec {
	return nil
}

// Register adds a provider instance under the given name.
// It is an error to register the same name twice.
func (m *ProviderManager) Register(name string, p IProvider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.providers[name]; exists {
		return fmt.Errorf("%w: %s", ErrProviderAlreadyExists, name)
	}
	m.providers[name] = p
	return nil
}

// Unregister removes a provider instance by name.
func (m *ProviderManager) Unregister(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.providers[name]; !exists {
		return fmt.Errorf("%w: %s", ErrProviderNotFound, name)
	}
	delete(m.providers, name)
	return nil
}

// Get returns the provider instance registered under the given name.
func (m *ProviderManager) Get(name string) IProvider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.providers[name]
}

// List returns capability information for all registered providers,
// sorted by name. Used by the Interface layer to present provider
// choices to the end user.
func (m *ProviderManager) List() []ProviderInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.providers))
	for name := range m.providers {
		names = append(names, name)
	}
	sort.Strings(names)

	infos := make([]ProviderInfo, 0, len(names))
	for _, name := range names {
		info := ProviderInfo{
			Name: name,
		}
		if mi := m.providers[name].ModelInfo(); mi.Provider != "" {
			info.DisplayName = mi.DisplayName
			info.DefaultModel = mi.Name
			info.Models = mi.Models
			if info.DisplayName == "" {
				info.DisplayName = mi.Provider
			}
		}
		infos = append(infos, info)
	}
	return infos
}

func (m *ProviderManager) log(ctx context.Context, level logger.Level, msg string, fields ...logger.Field) {
	r := m.Registry()
	if r == nil {
		return
	}
	lc := r.GetDefault(component.ComponentLogger)
	if lc == nil {
		return
	}
	l, ok := lc.(logger.Logger)
	if !ok {
		return
	}
	l.Log(ctx, logger.LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Module:    m.GetName(),
		Message:   msg,
		Fields:    fields,
	})
}
