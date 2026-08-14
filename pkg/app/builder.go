package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/tltre/gogent/internal/grpctransport"
	internal_otel "github.com/tltre/gogent/internal/otel"
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
	mgmtPort   string

	// v0.14.1: unified provider manager (HookManager pattern).
	// Created lazily on first provider component; registered into the
	// Registry once at the end of Build().
	providerManager *provider.ProviderManager

	// v0.14.5: app-scoped credential store for provider API keys.
	// Defaults to a FileCredentialStore at ~/.gogent/apps/<name>/credentials.yaml;
	// overridable via WithCredentialStore.
	credStore provider.CredentialStore
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
		credStore: provider.NewFileCredentialStore(
			provider.DefaultAppCredentialPath(cfg.Name),
		),
	}, nil
}

func NewBuilderFromConfig(cfg *Config) *Builder {
	return &Builder{
		config:   cfg,
		registry: component.NewRegistry(),
		pool:     grpctransport.NewPool(),
		credStore: provider.NewFileCredentialStore(
			provider.DefaultAppCredentialPath(cfg.Name),
		),
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

	// v0.14.1: register the unified ProviderManager created by native provider
	// components (idempotent — WithProvider options may create it later too).
	b.registerProviderManager()

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

	// v0.14.5: inject the (possibly overridden) credential store into all
	// registered engine instances. Done after BuildOptions so
	// WithCredentialStore takes effect before any Generate call.
	b.injectCredentialStore()

	// v0.14.1: WithProvider options may have created the manager — ensure it
	// is registered before registerDefaults.
	b.registerProviderManager()

	b.registerDefaults()

	// Create ToolManager and inject tool declarations (v0.12.2).
	tm := tool.NewToolManager(b.config.Name)
	if len(b.config.Tools) > 0 {
		entries := make([]tool.ManifestEntry, len(b.config.Tools))
		for i, t := range b.config.Tools {
		entries[i] = tool.ManifestEntry{
			Name:    t.Name,
			Level:   t.SecurityLevel,
			Sandbox: t.Sandbox, // v0.13.4: per-tool sandbox mapping
		}
		}
		tm.SetManifest(b.config.Name, entries)
	}

	// v0.13.3: Inject sandbox declarations into ToolManager.
	if len(b.config.Sandboxes) > 0 || b.config.Default != nil {
		sandboxes := make([]tool.SandboxDecl, len(b.config.Sandboxes))
		for i, sb := range b.config.Sandboxes {
			sandboxes[i] = tool.SandboxDecl{
				Name:    sb.Name,
				Profile: sb.Profile,
			}
		}
		defaultSb := ""
		if b.config.Default != nil {
			defaultSb = b.config.Default.Sandbox
		}
		tm.SetSandboxConfig(sandboxes, defaultSb)
	}

	// Inject HookManager into ToolManager (v0.12.9)
	if hmComp := b.registry.GetDefault(component.ComponentHook); hmComp != nil {
		if hm, ok := hmComp.(*hook.HookManager); ok {
			tm.SetHookManager(hm)
		}
	}

	app := &App{
		config:      b.config,
		registry:    b.registry,
		mgmtPort:    b.mgmtPort,
		toolManager: tm,
	}

	if b.config.Observability.OTel.Enabled {
		shutdown, err := internal_otel.InitFromConfig(context.Background(), b.config.Observability.OTel)
		if err != nil {
			return nil, fmt.Errorf("init otel: %w", err)
		}
		app.otelShutdown = shutdown
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
	// ComponentTool removed in v0.12.2 — tools managed by daemon ToolRegistry
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
	case string(component.DriverHTTP), string(component.DriverProcess):
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
	case string(component.DriverNative):
		// v0.14.7: native agentcore types registered in the agent type
		// registry (e.g. "react"). The type is selected via config.type;
		// defaults to "react".
		agentType := getString(cc.Config, "type")
		if agentType == "" {
			agentType = "react"
		}
		core, err := agentcore.CreateAgent(agentType)
		if err != nil {
			return nil, fmt.Errorf("create agent type %s: %w", agentType, err)
		}
		return agentcore.NewComponent(cc.Name, core), nil
	case string(component.DriverHTTP), string(component.DriverProcess):
		core := agentcore.NewProcessAgentCore(&agentcore.ProcessAgentCoreConfig{
			Name:   cc.Name,
			Pool:   b.pool,
			Target: targetFromEnvOrConfig("GOGENT_AGENTCORE_TARGET", cc.Config),
		})
		return agentcore.NewComponent(cc.Name, core), nil
	default:
		return nil, nil
	}
}

func (b *Builder) buildProvider(cc ComponentConfig) (component.Component, error) {
	switch cc.Driver {
	case string(component.DriverNative):
		// v0.14.1: native providers are registered into the unified
		// ProviderManager (blacklist mechanism). All registered engines are
		// enabled by default; the optional "exclude" config filters them out.
		if err := b.buildNativeProviders(cc); err != nil {
			return nil, err
		}
		return nil, nil
	case string(component.DriverHTTP), string(component.DriverProcess):
		// TODO(problem C): unify process/http providers under ProviderManager.
		// v0.14.1 keeps the existing standalone behavior.
		p := provider.NewProcessProvider(&provider.ProcessProviderConfig{
			Name:   cc.Name,
			Pool:   b.pool,
			Target: targetFromEnvOrConfig("GOGENT_PROVIDER_TARGET", cc.Config),
		})
		return provider.NewComponent(cc.Name, p), nil
	default:
		return nil, nil
	}
}

// ensureProviderManager lazily creates the unified ProviderManager component.
func (b *Builder) ensureProviderManager() *provider.ProviderManager {
	if b.providerManager == nil {
		b.providerManager = provider.NewManagerComponent("provider-manager")
	}
	return b.providerManager
}

// buildNativeProviders instantiates every registered engine (minus the ones
// listed in the "exclude" config) and registers it into the ProviderManager.
func (b *Builder) buildNativeProviders(cc ComponentConfig) error {
	excluded := make(map[string]bool)
	for _, e := range getStringSlice(cc.Config, "exclude") {
		excluded[e] = true
	}

	mgr := b.ensureProviderManager()
	for _, engine := range provider.RegisteredEngines() {
		if excluded[engine] {
			continue
		}
		impl, err := provider.CreateEngine(engine)
		if err != nil {
			return fmt.Errorf("create engine %s: %w", engine, err)
		}
		if err := mgr.Register(engine, impl); err != nil {
			return fmt.Errorf("register engine %s: %w", engine, err)
		}
	}
	return nil
}

// buildTool removed in v0.12.2 — tools managed by daemon ToolRegistry.

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
					case string(component.DriverHTTP), string(component.DriverProcess):
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
	case string(component.DriverHTTP), string(component.DriverProcess):
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
	case string(component.DriverHTTP), string(component.DriverProcess):
		cm := contextmanager.NewProcessContextManager(&contextmanager.ProcessContextManagerConfig{
			Name:   cc.Name,
			Pool:   b.pool,
			Target: targetFromEnvOrConfig("GOGENT_CONTEXT_TARGET", cc.Config),
		})
		return contextmanager.NewComponent(cc.Name, cm), nil
	default:
		return nil, nil
	}
}

func (b *Builder) buildMemory(cc ComponentConfig) (component.Component, error) {
	switch cc.Driver {
	case string(component.DriverHTTP), string(component.DriverProcess):
		m := memory.NewProcessMemory(&memory.ProcessMemoryConfig{
			Name:   cc.Name,
			Pool:   b.pool,
			Target: targetFromEnvOrConfig("GOGENT_MEMORY_TARGET", cc.Config),
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
	case string(component.DriverHTTP), string(component.DriverProcess):
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

// targetFromEnvOrConfig checks the given environment variable first.
// If the env var is set and non-empty, returns its value directly.
// Otherwise falls back to targetFromConfig.
func targetFromEnvOrConfig(envVar string, cfgMap map[string]any) string {
	if t := os.Getenv(envVar); t != "" {
		return t
	}
	return targetFromConfig(cfgMap)
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

// registerProviderManager registers the unified ProviderManager into the
// Registry if it exists and is not yet registered. Idempotent — safe to call
// multiple times across Build() (e.g. after WithProvider options).
func (b *Builder) registerProviderManager() {
	if b.providerManager == nil {
		return
	}
	if b.registry.Get(b.providerManager.GetName()) != nil {
		return
	}
	b.registry.Register(b.providerManager)
}

// injectCredentialStore hands the app-scoped CredentialStore to every
// registered engine instance that implements provider.CredentialStoreAware.
func (b *Builder) injectCredentialStore() {
	if b.providerManager == nil || b.credStore == nil {
		return
	}
	for _, name := range provider.RegisteredEngines() {
		impl := b.providerManager.Get(name)
		if impl == nil {
			continue
		}
		if aware, ok := impl.(provider.CredentialStoreAware); ok {
			aware.SetCredentialStore(b.credStore)
		}
	}
}

func (b *Builder) buildInterface() iface.Interface {
	switch b.config.Interface.Type {
	case "cli":
		c := cli.New(b.config.Interface.CLI.Banner, b.config.Interface.CLI.Prompt)
		cli.RegisterByPath(c, b.cliEntries)
		// v0.14.x: inject the app-scoped CredentialStore so the interactive
		// /key command can configure provider credentials.
		c.SetCredentialStore(b.credStore)
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

// WithProvider registers an IProvider instance into the unified
// ProviderManager (v0.14.1). The manager is created on demand and registered
// into the Registry at the end of Build().
func WithProvider(name string, p provider.IProvider) BuildOption {
	return func(b *Builder) error {
		mgr := b.ensureProviderManager()
		if err := mgr.Register(name, p); err != nil {
			return err
		}
		return nil
	}
}

// WithCredentialStore overrides the app-scoped CredentialStore used to
// resolve provider API keys (v0.14.5). The default is a FileCredentialStore
// at ~/.gogent/apps/<app-name>/credentials.yaml. App developers can inject
// their own implementation (Vault, keyring, etc.).
func WithCredentialStore(s provider.CredentialStore) BuildOption {
	return func(b *Builder) error {
		b.credStore = s
		return nil
	}
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

// WithTool declares a tool the application needs. The tool name must match
// a definition in the daemon's ~/.gogent/tools.yaml. level is the desired
// security level (0-2).
func WithTool(name string, level int) BuildOption {
	return func(b *Builder) error {
		b.config.Tools = append(b.config.Tools, ToolManifestEntry{
			Name:          name,
			SecurityLevel: level,
		})
		return nil
	}
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

// WithMgmtPort sets the management port for the App. The mgmt HTTP server
// is started automatically by app.Run() on this port.
func WithMgmtPort(port string) BuildOption {
	return func(b *Builder) error {
		b.mgmtPort = port
		return nil
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
