package app

import (
	"fmt"
	"os"
	"time"

	"github.com/yourorg/gagent/pkg/agentcore"
	"github.com/yourorg/gagent/pkg/channel"
	"github.com/yourorg/gagent/pkg/component"
	"github.com/yourorg/gagent/pkg/contextmanager"
	"github.com/yourorg/gagent/pkg/eventbus"
	"github.com/yourorg/gagent/pkg/hook"
	"github.com/yourorg/gagent/pkg/memory"
	"github.com/yourorg/gagent/pkg/provider"
	"github.com/yourorg/gagent/pkg/sandbox"
	"github.com/yourorg/gagent/pkg/tool"
)

type Builder struct {
	config   *Config
	registry *component.Registry
}

func NewBuilder(configPath string) (*Builder, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg, err := ParseConfig(data)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &Builder{
		config:   cfg,
		registry: component.NewRegistry(),
	}, nil
}

func NewBuilderFromConfig(cfg *Config) *Builder {
	return &Builder{
		config:   cfg,
		registry: component.NewRegistry(),
	}
}

func (b *Builder) Registry() *component.Registry {
	return b.registry
}

func (b *Builder) Build() (*App, error) {
	for _, cc := range b.config.Components {
		comp, err := b.buildComponent(cc)
		if err != nil {
			return nil, fmt.Errorf("build component %s: %w", cc.Name, err)
		}
		if err := b.registry.Register(comp); err != nil {
			return nil, fmt.Errorf("register component %s: %w", cc.Name, err)
		}
	}

	for typ, name := range b.config.Defaults {
		compType := component.ComponentType(typ)
		if err := b.registry.SetDefault(compType, name); err != nil {
			return nil, fmt.Errorf("set default for %s: %w", typ, err)
		}
	}

	return &App{
		config:   b.config,
		registry: b.registry,
	}, nil
}

func (b *Builder) buildComponent(cc ComponentConfig) (component.Component, error) {
	switch cc.Type {
	case "channel":
		return b.buildChannel(cc)
	case "agentcore":
		return b.buildAgentCore(cc)
	case "provider":
		return b.buildProvider(cc)
	case "tool":
		return b.buildTool(cc)
	case "hook":
		return b.buildHook(cc)
	case "eventbus":
		return b.buildEventBus(cc)
	case "contextmanager":
		return b.buildContextManager(cc)
	case "memory":
		return b.buildMemory(cc)
	case "sandbox":
		return b.buildSandbox(cc)
	default:
		return nil, fmt.Errorf("unknown component type: %s", cc.Type)
	}
}

func (b *Builder) buildChannel(cc ComponentConfig) (component.Component, error) {
	switch cc.Driver {
	case "native":
		bufferSize := getInt(cc.Config, "bufferSize", 100)
		ch := channel.NewNativeChannel(cc.Name, bufferSize)
		return channel.NewComponent(cc.Name, ch), nil
	case "http":
		cfg := &channel.HttpChannelConfig{
			Name:     cc.Name,
			Endpoint: getString(cc.Config, "endpoint"),
			Timeout:  getDuration(cc.Config, "timeout"),
		}
		ch := channel.NewHttpChannel(cfg)
		return channel.NewComponent(cc.Name, ch), nil
	default:
		return nil, fmt.Errorf("unknown channel driver: %s", cc.Driver)
	}
}

func (b *Builder) buildAgentCore(cc ComponentConfig) (component.Component, error) {
	return agentcore.NewComponent(cc.Name), nil
}

func (b *Builder) buildProvider(cc ComponentConfig) (component.Component, error) {
	switch cc.Driver {
	case "native":
		p := provider.NewNativeProvider(cc.Name)
		return provider.NewComponent(cc.Name, p), nil
	case "http":
		cfg := &provider.HttpProviderConfig{
			Name:     cc.Name,
			Endpoint: getString(cc.Config, "endpoint"),
			ApiKey:   getString(cc.Config, "apiKey"),
			Model:    getString(cc.Config, "model"),
			Timeout:  getDuration(cc.Config, "timeout"),
		}
		p := provider.NewHttpProvider(cfg)
		return provider.NewComponent(cc.Name, p), nil
	default:
		return nil, fmt.Errorf("unknown provider driver: %s", cc.Driver)
	}
}

func (b *Builder) buildTool(cc ComponentConfig) (component.Component, error) {
	comp := tool.NewComponent(cc.Name)

	if tools, ok := cc.Config["tools"]; ok {
		if toolList, ok := tools.([]any); ok {
			for _, t := range toolList {
				if toolMap, ok := t.(map[string]any); ok {
					name := getString(toolMap, "name")
					desc := getString(toolMap, "description")
					driver := getString(toolMap, "driver")
					switch driver {
					case "native":
						toolImpl := tool.NewNativeTool(tool.ToolInfo{
							Name:        name,
							Description: desc,
						}, nil)
						comp.Register(toolImpl)
					case "http":
						cfg := &tool.HttpToolConfig{
							Name:        name,
							Description: desc,
							Endpoint:    getString(toolMap, "endpoint"),
						}
						toolImpl := tool.NewHttpTool(cfg)
						comp.Register(toolImpl)
					}
				}
			}
		}
	}

	return comp, nil
}

func (b *Builder) buildHook(cc ComponentConfig) (component.Component, error) {
	comp := hook.NewComponent(cc.Name)

	if hooks, ok := cc.Config["hooks"]; ok {
		if hookList, ok := hooks.([]any); ok {
			for _, h := range hookList {
				if hookMap, ok := h.(map[string]any); ok {
					name := getString(hookMap, "name")
					driver := getString(hookMap, "driver")
					events := getEvents(hookMap, "events")
					switch driver {
					case "native":
						hookImpl := hook.NewNativeHook(name, events, nil)
						comp.Register(hookImpl)
					case "http":
						cfg := &hook.HttpHookConfig{
							Name:     name,
							Endpoint: getString(hookMap, "endpoint"),
							Events:   events,
						}
						hookImpl := hook.NewHttpHook(cfg)
						comp.Register(hookImpl)
					}
				}
			}
		}
	}

	return comp, nil
}

func (b *Builder) buildEventBus(cc ComponentConfig) (component.Component, error) {
	switch cc.Driver {
	case "native":
		eb := eventbus.NewNativeEventBus()
		return eventbus.NewComponent(cc.Name, eb), nil
	case "http":
		cfg := &eventbus.HttpEventBusConfig{
			Name:     cc.Name,
			Endpoint: getString(cc.Config, "endpoint"),
		}
		eb := eventbus.NewHttpEventBus(cfg)
		return eventbus.NewComponent(cc.Name, eb), nil
	default:
		return nil, fmt.Errorf("unknown eventbus driver: %s", cc.Driver)
	}
}

func (b *Builder) buildContextManager(cc ComponentConfig) (component.Component, error) {
	switch cc.Driver {
	case "native":
		return contextmanager.NewComponent(cc.Name), nil
	case "http":
		return contextmanager.NewComponent(cc.Name), nil
	default:
		return nil, fmt.Errorf("unknown contextmanager driver: %s", cc.Driver)
	}
}

func (b *Builder) buildMemory(cc ComponentConfig) (component.Component, error) {
	switch cc.Driver {
	case "native":
		m := memory.NewNativeMemory()
		return memory.NewComponent(cc.Name, m), nil
	case "http":
		cfg := &memory.HttpMemoryConfig{
			Name:     cc.Name,
			Endpoint: getString(cc.Config, "endpoint"),
		}
		m := memory.NewHttpMemory(cfg)
		return memory.NewComponent(cc.Name, m), nil
	default:
		return nil, fmt.Errorf("unknown memory driver: %s", cc.Driver)
	}
}

func (b *Builder) buildSandbox(cc ComponentConfig) (component.Component, error) {
	limits := sandbox.ResourceLimits{
		MaxMemoryMB:   getInt(cc.Config, "maxMemoryMB", 512),
		NetworkAccess: getBool(cc.Config, "networkAccess", false),
	}

	switch cc.Driver {
	case "native":
		s := sandbox.NewNativeSandbox(limits)
		return sandbox.NewComponent(cc.Name, s, limits), nil
	case "http":
		cfg := &sandbox.HttpSandboxConfig{
			Name:     cc.Name,
			Endpoint: getString(cc.Config, "endpoint"),
		}
		s := sandbox.NewHttpSandbox(cfg, limits)
		return sandbox.NewComponent(cc.Name, s, limits), nil
	default:
		return nil, fmt.Errorf("unknown sandbox driver: %s", cc.Driver)
	}
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(m map[string]any, key string, defaultVal int) int {
	if v, ok := m[key]; ok {
		if i, ok := v.(int); ok {
			return i
		}
	}
	return defaultVal
}

func getBool(m map[string]any, key string, defaultVal bool) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}

func getDuration(m map[string]any, key string) time.Duration {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			d, err := time.ParseDuration(s)
			if err == nil {
				return d
			}
		}
	}
	return 0
}

func getEvents(m map[string]any, key string) []hook.EventType {
	if v, ok := m[key]; ok {
		if list, ok := v.([]any); ok {
			events := make([]hook.EventType, 0, len(list))
			for _, item := range list {
				if s, ok := item.(string); ok {
					switch s {
					case "beforeRun":
						events = append(events, hook.EventBeforeRun)
					case "afterRun":
						events = append(events, hook.EventAfterRun)
					case "beforeTool":
						events = append(events, hook.EventBeforeTool)
					case "afterTool":
						events = append(events, hook.EventAfterTool)
					case "beforeLLM":
						events = append(events, hook.EventBeforeLLM)
					case "afterLLM":
						events = append(events, hook.EventAfterLLM)
					case "error":
						events = append(events, hook.EventError)
					}
				}
			}
			return events
		}
	}
	return nil
}
