package hook

import (
	"context"
	"sync"
	"time"

	"github.com/tltre/gagent/pkg/component"
	"github.com/tltre/gagent/pkg/logger"
)

type HookManager struct {
	component.BasicComponent
	mu     sync.RWMutex
	hooks  []IHook
	byType map[EventType][]IHook
}

func NewComponent(name string) *HookManager {
	return &HookManager{
		BasicComponent: component.NewBasicComponent(name),
		hooks:          make([]IHook, 0),
		byType:         make(map[EventType][]IHook),
	}
}

func (hm *HookManager) GetType() component.ComponentType {
	return component.ComponentHook
}

func (hm *HookManager) Initialize(ctx context.Context, registry *component.Registry) error {
	hm.SetRegistry(registry)
	hm.log(ctx, logger.InfoLevel, "hook manager initialized")
	return nil
}

func (hm *HookManager) Start(ctx context.Context) error {
	hm.log(ctx, logger.DebugLevel, "hook manager started")
	return nil
}

func (hm *HookManager) Stop(ctx context.Context) error {
	hm.log(ctx, logger.DebugLevel, "hook manager stopped")
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

	hm.log(ctx, logger.DebugLevel, "trigger",
		logger.Field{Key: "event_type", Value: int(event.Type)},
		logger.Field{Key: "hook_count", Value: len(hooks)},
	)

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

func (hm *HookManager) log(ctx context.Context, level logger.Level, msg string, fields ...logger.Field) {
	r := hm.Registry()
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
		Module:    hm.GetName(),
		Message:   msg,
		Fields:    fields,
	})
}
