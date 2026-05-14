package app

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/tltre/gogent/internal/grpctransport"
	"github.com/tltre/gogent/pkg/agentcore"
	"github.com/tltre/gogent/pkg/channel"
	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/contextmanager"
	"github.com/tltre/gogent/pkg/eventbus"
	"github.com/tltre/gogent/pkg/hook"
	"github.com/tltre/gogent/pkg/iface"
	"github.com/tltre/gogent/pkg/iface/cli"
	"github.com/tltre/gogent/pkg/logger"
	"github.com/tltre/gogent/pkg/memory"
	"github.com/tltre/gogent/pkg/provider"
	"github.com/tltre/gogent/pkg/sandbox"
	"github.com/tltre/gogent/pkg/tool"
)

type Builder struct {
	config     *Config
	registry   *component.Registry
	iface      iface.Interface
	cliEntries []cli.CommandEntry
	pool       *grpctransport.Pool
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
		pool:     grpctransport.NewPool(),
	}, nil
}

func NewBuilderFromConfig(cfg *Config) *Builder {
	return &Builder{
		config:   cfg,
		registry: component.NewRegistry(),
		pool:     grpctransport.NewPool(),
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
		if comp == nil {
			continue
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

	b.registerDefaults()

	app := &App{
		config:   b.config,
		registry: b.registry,
	}

	if b.iface != nil {
		app.iface = b.iface
	} else {
		app.iface = b.buildInterface()
	}

	return app, nil
}

func (b *Builder) buildComponent(cc ComponentConfig) (component.Component, error) {
	switch component.ComponentType(cc.Type) {
	case component.ComponentChannel:
		return b.buildChannel(cc)
	case component.ComponentAgentCore:
		return b.buildAgentCore(cc)
	case component.ComponentProvider:
		return b.buildProvider(cc)
	case component.ComponentTool:
		return b.buildTool(cc)
	case component.ComponentHook:
		return b.buildHook(cc)
	case component.ComponentEventBus:
		return b.buildEventBus(cc)
	case component.ComponentContextManager:
		return b.buildContextManager(cc)
	case component.ComponentMemory:
		return b.buildMemory(cc)
	case component.ComponentSandbox:
		return b.buildSandbox(cc)
	case component.ComponentLogger:
		return b.buildLogger(cc)
	default:
		return nil, fmt.Errorf("unknown component type: %s", cc.Type)
	}
}

func (b *Builder) buildChannel(cc ComponentConfig) (component.Component, error) {
	switch cc.Driver {
	case "http", "process":
		ch := channel.NewProcessChannel(&channel.ProcessChannelConfig{
			Name:   cc.Name,
			Pool:   b.pool,
			Target: targetFromConfig(cc.Config),
		})
		return channel.NewComponent(cc.Name, ch), nil
	default:
		return nil, nil
	}
}

func (b *Builder) buildAgentCore(cc ComponentConfig) (component.Component, error) {
	switch cc.Driver {
	case "http", "process":
		core := agentcore.NewProcessAgentCore(&agentcore.ProcessAgentCoreConfig{
			Name:   cc.Name,
			Pool:   b.pool,
			Target: targetFromConfig(cc.Config),
		})
		return agentcore.NewComponent(cc.Name, core), nil
	default:
		return nil, nil
	}
}

func (b *Builder) buildProvider(cc ComponentConfig) (component.Component, error) {
	switch cc.Driver {
	case "http", "process":
		p := provider.NewProcessProvider(&provider.ProcessProviderConfig{
			Name:   cc.Name,
			Pool:   b.pool,
			Target: targetFromConfig(cc.Config),
		})
		return provider.NewComponent(cc.Name, p), nil
	default:
		return nil, nil
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
					case "http", "process":
						toolImpl := tool.NewProcessTool(&tool.ProcessToolConfig{
							Name:        name,
							Description: desc,
							ToolName:    getString(toolMap, "toolName"),
							Pool:        b.pool,
							Target:      targetFromConfig(toolMap),
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
					case "http", "process":
						hookImpl := hook.NewProcessHook(&hook.ProcessHookConfig{
							Name:   name,
							Events: events,
							Pool:   b.pool,
							Target: targetFromConfig(hookMap),
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
	case "http", "process":
		eb := eventbus.NewProcessEventBus(&eventbus.ProcessEventBusConfig{
			Name:   cc.Name,
			Pool:   b.pool,
			Target: targetFromConfig(cc.Config),
		})
		return eventbus.NewComponent(cc.Name, eb), nil
	default:
		return nil, nil
	}
}

func (b *Builder) buildContextManager(cc ComponentConfig) (component.Component, error) {
	switch cc.Driver {
	case "http", "process":
		cm := contextmanager.NewProcessContextManager(&contextmanager.ProcessContextManagerConfig{
			Name:   cc.Name,
			Pool:   b.pool,
			Target: targetFromConfig(cc.Config),
		})
		return contextmanager.NewComponent(cc.Name, cm), nil
	default:
		return nil, nil
	}
}

func (b *Builder) buildMemory(cc ComponentConfig) (component.Component, error) {
	switch cc.Driver {
	case "http", "process":
		m := memory.NewProcessMemory(&memory.ProcessMemoryConfig{
			Name:   cc.Name,
			Pool:   b.pool,
			Target: targetFromConfig(cc.Config),
		})
		return memory.NewComponent(cc.Name, m), nil
	default:
		return nil, nil
	}
}

func (b *Builder) buildSandbox(cc ComponentConfig) (component.Component, error) {
	limits := sandbox.ResourceLimits{
		MaxMemoryMB:     getInt(cc.Config, "maxMemoryMB", 512),
		NetworkAccess:   getBool(cc.Config, "networkAccess", false),
		AllowedCommands: getStringSlice(cc.Config, "allowedCommands"),
		ReadOnlyRoot:    getBool(cc.Config, "readOnlyRoot", false),
	}

	switch cc.Driver {
	case "http", "process":
		s := sandbox.NewProcessSandbox(&sandbox.ProcessSandboxConfig{
			Name:   cc.Name,
			Pool:   b.pool,
			Target: targetFromConfig(cc.Config),
		})
		return sandbox.NewComponent(cc.Name, s, limits), nil
	default:
		return nil, nil
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
	l := logger.NewDefaultLogger(cfg)
	return logger.NewComponent(cc.Name, l), nil
}

// targetFromConfig extracts the gRPC target address from a component config map.
// It reads the "endpoint" key and strips any http:// or https:// prefix,
// since gRPC targets use bare host:port format (e.g. "localhost:9091").
func targetFromConfig(cfgMap map[string]any) string {
	ep := getString(cfgMap, "endpoint")
	ep = strings.TrimPrefix(ep, "http://")
	ep = strings.TrimPrefix(ep, "https://")
	return ep
}

var componentDefaults = map[component.ComponentType]func() component.Component{
	component.ComponentLogger: func() component.Component {
		l := logger.NewDefaultLogger(logger.Config{Level: "info", Format: "console"})
		return logger.NewComponent("logger-default", l)
	},
	component.ComponentSandbox: func() component.Component {
		limits := sandbox.ResourceLimits{MaxMemoryMB: 512, NetworkAccess: false}
		s := sandbox.NewDefaultSandbox(limits)
		return sandbox.NewComponent("sandbox-default", s, limits)
	},
	component.ComponentEventBus: func() component.Component {
		eb := eventbus.NewDefaultEventBus()
		return eventbus.NewComponent("eventbus-default", eb)
	},
	component.ComponentMemory: func() component.Component {
		m := memory.NewDefaultMemory()
		return memory.NewComponent("memory-default", m)
	},
	// TODO: default provider
	// TODO: default tool
	// TODO: default hook
	// TODO: default contextmanager
	// TODO: default channel
}

func (b *Builder) registerDefaults() {
	for typ, factory := range componentDefaults {
		if b.registry.GetDefault(typ) == nil {
			comp := factory()
			if comp == nil {
				continue
			}
			b.registry.Register(comp)
			b.registry.SetDefault(typ, comp.GetName())
		}
	}
}

func (b *Builder) buildInterface() iface.Interface {
	switch b.config.Interface.Type {
	case "cli":
		c := cli.New(b.config.Interface.CLI.Banner, b.config.Interface.CLI.Prompt)
		cli.RegisterByPath(c, b.cliEntries)
		return c
	case "tui", "http":
		// v0.4.3+, currently nil — falls back to blocking bare event loop
		return nil
	default:
		return nil
	}
}



type BuildOption func(*Builder) error

func WithInterface(i iface.Interface) BuildOption {
	return func(b *Builder) error {
		b.iface = i
		return nil
	}
}

func WithCLICommand(entries ...cli.CommandEntry) BuildOption {
	return func(b *Builder) error {
		b.cliEntries = append(b.cliEntries, entries...)
		return nil
	}
}

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

func WithLogger(name string, l logger.Logger) BuildOption {
	comp := logger.NewComponent(name, l)
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
