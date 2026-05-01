package channel

import (
	"context"
	"sync"

	"github.com/tltre/gagent/pkg/component"
)

type ChannelManager struct {
	name     string
	mu       sync.RWMutex
	channels map[string]IChannel
}

func NewComponent(name string, channel ...IChannel) *ChannelManager {
	channels := make(map[string]IChannel)
	for _, ch := range channel {
		channels[ch.Name()] = ch
	}

	return &ChannelManager{
		name:     name,
		channels: channels,
	}
}

func (cm *ChannelManager) GetName() string {
	return cm.name
}

func (cm *ChannelManager) GetType() component.ComponentType {
	return component.ComponentChannel
}

func (cm *ChannelManager) Initialize(ctx context.Context, registry *component.Registry) error {
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

// TODO 这里 startAll 后需要接收各个 channels 的消息并且统一投递到 eventBus 中
func (cm *ChannelManager) startAll(ctx context.Context) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	for _, ch := range cm.channels {
		if err := ch.Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (cm *ChannelManager) stopAll(ctx context.Context) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	for _, ch := range cm.channels {
		if err := ch.Stop(ctx); err != nil {
			return err
		}
	}
	return nil
}
