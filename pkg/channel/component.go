package channel

import (
	"context"
	"sync"
	"time"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/logger"
)

type ChannelManager struct {
	component.BasicComponent
	mu       sync.RWMutex
	channels map[string]IChannel
}

func NewComponent(name string, channel ...IChannel) *ChannelManager {
	channels := make(map[string]IChannel)
	for _, ch := range channel {
		channels[ch.Name()] = ch
	}

	return &ChannelManager{
		BasicComponent: component.NewBasicComponent(name),
		channels:       channels,
	}
}

func (cm *ChannelManager) GetType() component.ComponentType {
	return component.ComponentChannel
}

func (cm *ChannelManager) Initialize(ctx context.Context, registry *component.Registry) error {
	cm.SetRegistry(registry)
	cm.log(ctx, logger.InfoLevel, "channel manager initialized")
	return nil
}

func (cm *ChannelManager) Start(ctx context.Context) error {
	return cm.startAll(ctx)
}

func (cm *ChannelManager) Stop(ctx context.Context) error {
	return cm.stopAll(ctx)
}

func (cm *ChannelManager) Dependencies() map[string]component.DependencySpec {
	return nil
}

func (cm *ChannelManager) Register(name string, ch IChannel) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.channels[name] = ch
}

func (cm *ChannelManager) Get(name string) IChannel {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.channels[name]
}

func (cm *ChannelManager) List() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	names := make([]string, 0, len(cm.channels))
	for name := range cm.channels {
		names = append(names, name)
	}
	return names
}

func (cm *ChannelManager) startAll(ctx context.Context) error {
	cm.mu.RLock()
	count := len(cm.channels)
	cm.mu.RUnlock()
	cm.log(ctx, logger.DebugLevel, "channels starting",
		logger.Field{Key: "count", Value: count},
	)
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	for _, ch := range cm.channels {
		if err := ch.Start(ctx); err != nil {
			return err
		}
	}
	cm.log(ctx, logger.DebugLevel, "channels started",
		logger.Field{Key: "count", Value: count},
	)
	return nil
}

func (cm *ChannelManager) stopAll(ctx context.Context) error {
	cm.mu.RLock()
	count := len(cm.channels)
	cm.mu.RUnlock()
	cm.log(ctx, logger.DebugLevel, "channels stopping",
		logger.Field{Key: "count", Value: count},
	)
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	for _, ch := range cm.channels {
		if err := ch.Stop(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (cm *ChannelManager) log(ctx context.Context, level logger.Level, msg string, fields ...logger.Field) {
	r := cm.Registry()
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
		Module:    cm.GetName(),
		Message:   msg,
		Fields:    fields,
	})
}
