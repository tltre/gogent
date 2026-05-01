package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/tltre/gagent/internal/client"
	"github.com/tltre/gagent/pkg/agentcore"
	"github.com/tltre/gagent/pkg/channel"
	"github.com/tltre/gagent/pkg/component"
	"github.com/tltre/gagent/pkg/contextmanager"
	"github.com/tltre/gagent/pkg/eventbus"
	"github.com/tltre/gagent/pkg/hook"
	"github.com/tltre/gagent/pkg/logger"
	"github.com/tltre/gagent/pkg/memory"
	"github.com/tltre/gagent/pkg/provider"
	"github.com/tltre/gagent/pkg/sandbox"
	"github.com/tltre/gagent/pkg/tool"
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

func (b *Builder) Build(opts ...BuildOption) (*App, error) {
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

	for _, opt := range opts {
		if err := opt(b); err != nil {
			return nil, fmt.Errorf("apply build option: %w", err)
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
	case "logger":
		return b.buildLogger(cc)
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
		t := b.newHTTPTransport(cc.Config, cc.Type)
		ch := channel.NewProcessChannel(&channel.ProcessChannelConfig{
			Name:      cc.Name,
			Transport: t,
		})
		return channel.NewComponent(cc.Name, ch), nil
	case "process":
		t := b.newStdioTransport(cc.Config, cc.Type)
		ch := channel.NewProcessChannel(&channel.ProcessChannelConfig{
			Name:      cc.Name,
			Transport: t,
		})
		return channel.NewComponent(cc.Name, ch), nil
	default:
		return nil, fmt.Errorf("unknown channel driver: %s", cc.Driver)
	}
}

func (b *Builder) buildAgentCore(cc ComponentConfig) (component.Component, error) {
	switch cc.Driver {
	case "native":
		return agentcore.NewComponent(cc.Name, nil), nil
	case "http":
		t := b.newHTTPTransport(cc.Config, cc.Type)
		core := agentcore.NewProcessAgentCore(&agentcore.ProcessAgentCoreConfig{
			Name:      cc.Name,
			Transport: t,
		})
		return agentcore.NewComponent(cc.Name, core), nil
	case "process":
		t := b.newStdioTransport(cc.Config, cc.Type)
		core := agentcore.NewProcessAgentCore(&agentcore.ProcessAgentCoreConfig{
			Name:      cc.Name,
			Transport: t,
		})
		return agentcore.NewComponent(cc.Name, core), nil
	default:
		return nil, fmt.Errorf("unknown agentcore driver: %s", cc.Driver)
	}
}

func (b *Builder) buildProvider(cc ComponentConfig) (component.Component, error) {
	switch cc.Driver {
	case "native":
		p := provider.NewNativeProvider(cc.Name)
		return provider.NewComponent(cc.Name, p), nil
	case "http":
		t := b.newHTTPTransport(cc.Config, cc.Type)
		p := provider.NewProcessProvider(&provider.ProcessProviderConfig{
			Name:      cc.Name,
			Transport: t,
		})
		return provider.NewComponent(cc.Name, p), nil
	case "process":
		t := b.newStdioTransport(cc.Config, cc.Type)
		p := provider.NewProcessProvider(&provider.ProcessProviderConfig{
			Name:      cc.Name,
			Transport: t,
		})
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
						tr := b.newHTTPTransport(toolMap, "tool")
						toolImpl := tool.NewProcessTool(&tool.ProcessToolConfig{
							Name:        name,
							Description: desc,
							ToolName:    getString(toolMap, "toolName"),
							Transport:   tr,
						})
						comp.Register(toolImpl)
				case "process":
						tr := b.newStdioTransport(toolMap, "tool")
						toolImpl := tool.NewProcessTool(&tool.ProcessToolConfig{
							Name:        name,
							Description: desc,
							ToolName:    getString(toolMap, "toolName"),
							Transport:   tr,
						})
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
						tr := b.newHTTPTransport(hookMap, "hook")
						hookImpl := hook.NewProcessHook(&hook.ProcessHookConfig{
							Name:      name,
							Events:    events,
							Transport: tr,
						})
						comp.Register(hookImpl)
				case "process":
						tr := b.newStdioTransport(hookMap, "hook")
						hookImpl := hook.NewProcessHook(&hook.ProcessHookConfig{
							Name:      name,
							Events:    events,
							Transport: tr,
						})
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
		t := b.newHTTPTransport(cc.Config, cc.Type)
		eb := eventbus.NewProcessEventBus(&eventbus.ProcessEventBusConfig{
			Name:      cc.Name,
			Transport: t,
		})
		return eventbus.NewComponent(cc.Name, eb), nil
	case "process":
		t := b.newStdioTransport(cc.Config, cc.Type)
		eb := eventbus.NewProcessEventBus(&eventbus.ProcessEventBusConfig{
			Name:      cc.Name,
			Transport: t,
		})
		return eventbus.NewComponent(cc.Name, eb), nil
	default:
		return nil, fmt.Errorf("unknown eventbus driver: %s", cc.Driver)
	}
}

func (b *Builder) buildContextManager(cc ComponentConfig) (component.Component, error) {
	switch cc.Driver {
	case "native":
		return contextmanager.NewComponent(cc.Name, nil), nil
	case "http":
		t := b.newHTTPTransport(cc.Config, cc.Type)
		cm := contextmanager.NewProcessContextManager(&contextmanager.ProcessContextManagerConfig{
			Name:      cc.Name,
			Transport: t,
		})
		return contextmanager.NewComponent(cc.Name, cm), nil
	case "process":
		t := b.newStdioTransport(cc.Config, cc.Type)
		cm := contextmanager.NewProcessContextManager(&contextmanager.ProcessContextManagerConfig{
			Name:      cc.Name,
			Transport: t,
		})
		return contextmanager.NewComponent(cc.Name, cm), nil
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
		t := b.newHTTPTransport(cc.Config, cc.Type)
		m := memory.NewProcessMemory(&memory.ProcessMemoryConfig{
			Name:      cc.Name,
			Transport: t,
		})
		return memory.NewComponent(cc.Name, m), nil
	case "process":
		t := b.newStdioTransport(cc.Config, cc.Type)
		m := memory.NewProcessMemory(&memory.ProcessMemoryConfig{
			Name:      cc.Name,
			Transport: t,
		})
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
		t := b.newHTTPTransport(cc.Config, cc.Type)
		s := sandbox.NewProcessSandbox(&sandbox.ProcessSandboxConfig{
			Name:      cc.Name,
			Transport: t,
		})
		return sandbox.NewComponent(cc.Name, s, limits), nil
	case "process":
		t := b.newStdioTransport(cc.Config, cc.Type)
		s := sandbox.NewProcessSandbox(&sandbox.ProcessSandboxConfig{
			Name:      cc.Name,
			Transport: t,
		})
		return sandbox.NewComponent(cc.Name, s, limits), nil
	default:
		return nil, fmt.Errorf("unknown sandbox driver: %s", cc.Driver)
	}
}

func (b *Builder) buildLogger(cc ComponentConfig) (component.Component, error) {
	cfg := logger.Config{
		Level:  getString(cc.Config, "level"),
		Format: getString(cc.Config, "format"),
		Output: getString(cc.Config, "output"),
	}
	if cfg.Level == "" {
		cfg.Level = "info"
	}
	if cfg.Format == "" {
		cfg.Format = "console"
	}
	l := logger.NewZapLogger(cfg)
	return logger.NewComponent(cc.Name, l), nil
}

func (b *Builder) newStdioTransport(cfgMap map[string]any, component string) *client.StdioTransport {
	return client.NewStdioTransport(client.StdioTransportConfig{
		Command:   getString(cfgMap, "command"),
		Args:      getStringSlice(cfgMap, "args"),
		Env:       getStringSlice(cfgMap, "env"),
		Component: component,
		Logger:    &transportLogAdapter{l: logger.Default()},
	})
}

func (b *Builder) newHTTPTransport(cfgMap map[string]any, component string) *client.HTTPTransport {
	return client.NewHTTPTransport(client.HTTPTransportConfig{
		Endpoint:  getString(cfgMap, "endpoint"),
		Timeout:   getDuration(cfgMap, "timeout"),
		Component: component,
		Logger:    &transportLogAdapter{l: logger.Default()},
	})
}

type transportLogAdapter struct {
	l logger.Logger
}

func (a *transportLogAdapter) Log(ctx context.Context, entry client.LogEntry) {
	fields := make([]logger.Field, len(entry.Fields))
	for i, f := range entry.Fields {
		fields[i] = logger.Field{Key: f.Key, Value: f.Value}
	}
	lvl := logger.InfoLevel
	if entry.Level == client.ErrorLevel {
		lvl = logger.ErrorLevel
	}
	a.l.Log(ctx, logger.LogEntry{
		Level:    lvl,
		Module:   entry.Module,
		Message:  entry.Message,
		Duration: entry.Duration,
		Fields:   fields,
	})
}

type BuildOption func(*Builder) error

func WithAgentCore(name string, core agentcore.IAgentCore) BuildOption {
	comp := agentcore.NewComponent(name, core)
	return WithComponent(comp)
}

func WithProvider(name string, p provider.IProvider) BuildOption {
	comp := provider.NewComponent(name, p)
	return WithComponent(comp)
}

func WithEventBus(name string, eb eventbus.IEventBus) BuildOption {
	comp := eventbus.NewComponent(name, eb)
	return WithComponent(comp)
}

func WithContextManager(name string, cm contextmanager.IContextManager) BuildOption {
	comp := contextmanager.NewComponent(name, cm)
	return WithComponent(comp)
}

func WithMemory(name string, m memory.IMemory) BuildOption {
	comp := memory.NewComponent(name, m)
	return WithComponent(comp)
}

func WithHooks(hooks ...hook.IHook) BuildOption {
	// TODO
	return nil
}

func WithTools(tools ...tool.ITool) BuildOption {
	// TODO
	return nil
}

func WithChannels(channels ...channel.IChannel) BuildOption {
	// TODO
	return nil
}

func WithComponent(comp component.Component) BuildOption {
	return func(b *Builder) error {
		if err := b.registry.Unregister(comp.GetName()); err != nil && !errors.Is(err, component.ErrComponentNotFound) {
			return err
		}

		if err := b.registry.Register(comp); err != nil && !errors.Is(err, component.ErrComponentAlreadyExists) {
			return err
		}

		return b.registry.SetDefault(comp.GetType(), comp.GetName())
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

func getStringSlice(m map[string]any, key string) []string {
	if v, ok := m[key]; ok {
		if list, ok := v.([]any); ok {
			result := make([]string, 0, len(list))
			for _, item := range list {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
	}
	return nil
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
