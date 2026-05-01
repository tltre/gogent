package hook

import (
	"context"
	"sync"

	"github.com/tltre/gagent/pkg/component"
)

type HookManager struct {
	name   string
	mu     sync.RWMutex
	hooks  []IHook
	byType map[EventType][]IHook
}

func NewComponent(name string) *HookManager {
	return &HookManager{
		name:   name,
		hooks:  make([]IHook, 0),
		byType: make(map[EventType][]IHook),
	}
}

func (hm *HookManager) GetName() string {
	return hm.name
}

func (hm *HookManager) GetType() component.ComponentType {
	return component.ComponentHook
}

func (hm *HookManager) Initialize(ctx context.Context, deps *component.Registry) error {
	return nil
}

func (hm *HookManager) Start(ctx context.Context) error {
	return nil
}

func (hm *HookManager) Stop(ctx context.Context) error {
	return nil
}

func (hm *HookManager) Dependencies() map[string]component.DependencySpec {
	return nil
}

func (hm *HookManager) Register(hook IHook) error {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.hooks = append(hm.hooks, hook)
	for _, eventType := range hook.Events() {
		hm.byType[eventType] = append(hm.byType[eventType], hook)
	}
	return nil
}

func (hm *HookManager) Trigger(ctx context.Context, event Event) (context.Context, error) {
	hm.mu.RLock()
	hooks := hm.byType[event.Type]
	hm.mu.RUnlock()

	for _, hook := range hooks {
		newCtx, err := hook.OnEvent(ctx, event)
		if err != nil {
			return ctx, err
		}
		ctx = newCtx
	}
	return ctx, nil
}

func (hm *HookManager) GetHooks(eventType EventType) []IHook {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return hm.byType[eventType]
}
