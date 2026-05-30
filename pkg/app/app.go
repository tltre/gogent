package app

import (
	"context"
	"fmt"
	"os"

	"github.com/tltre/gogent/internal/daemon"
	"github.com/tltre/gogent/internal/mgmt"
	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/iface"
	"github.com/tltre/gogent/pkg/tool"
)

type otelShutdownFn func(context.Context) error

type App struct {
	config       *Config
	registry     *component.Registry
	iface        iface.Interface
	mgmtPort     string
	otelShutdown otelShutdownFn
	toolManager  *tool.ToolManager // v0.12.2
}

// ToolManager returns the app's ToolManager instance.
func (a *App) ToolManager() *tool.ToolManager {
	return a.toolManager
}

// SetToolManager sets the ToolManager instance (used by Builder).
func (a *App) SetToolManager(tm *tool.ToolManager) {
	a.toolManager = tm
}

func (a *App) Name() string {
	if a.config == nil {
		return ""
	}
	return a.config.Name
}

func (a *App) Registry() *component.Registry {
	return a.registry
}

func (a *App) Interface() iface.Interface {
	return a.iface
}

func (a *App) SetInterface(i iface.Interface) {
	a.iface = i
}

func (a *App) Initialize(ctx context.Context) error {
	return a.registry.InitializeAll(ctx)
}

func (a *App) Start(ctx context.Context) error {
	if err := a.registry.StartAll(ctx); err != nil {
		return fmt.Errorf("start components: %w", err)
	}
	return nil
}

func (a *App) Stop(ctx context.Context) error {
	var errs []error
	if a.otelShutdown != nil {
		if err := a.otelShutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("otel shutdown: %w", err))
		}
	}
	if err := a.registry.StopAll(ctx); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("stop errors: %v", errs)
	}
	return nil
}

func (a *App) Get(name string) component.Component {
	return a.registry.Get(name)
}

func (a *App) GetDefault(typ component.ComponentType) component.Component {
	return a.registry.GetDefault(typ)
}

func (a *App) GetByType(typ component.ComponentType) []component.Component {
	return a.registry.GetByType(typ)
}

// hasProcessDriverComponents returns true if any component in the app's
// config uses driver "process", indicating that component processes should
// be managed by a daemon.
func (a *App) hasProcessDriverComponents() bool {
	if a.config == nil {
		return false
	}
	for _, cc := range a.config.Components {
		if cc.Driver == string(component.DriverProcess) {
			return true
		}
	}
	return false
}

func (a *App) Run(ctx context.Context) error {
	if err := a.Initialize(ctx); err != nil {
		return err
	}
	if err := a.Start(ctx); err != nil {
		return err
	}

	// Start ToolManager (v0.12.2) — non-blocking on failure.
	if a.toolManager != nil {
		if err := a.toolManager.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "[app] warn: tool manager start: %v\n", err)
		}
	}
	defer func() {
		if a.toolManager != nil {
			a.toolManager.Stop(ctx)
		}
	}()

	// Start management HTTP server (non-blocking goroutine).
	var srv *mgmt.Server
	if a.mgmtPort != "" {
		srv = mgmt.Listen(a.mgmtPort, a.registry, a.toolManager)
		defer srv.Shutdown(context.Background())

		if err := daemon.WriteAppPortFile(a.Name(), a.mgmtPort); err != nil {
			fmt.Fprintf(os.Stderr, "warn: write port file: %v\n", err)
		}

		// Auto-detect/start daemon when process-driver components are present.
		if a.hasProcessDriverComponents() {
			if _, err := daemon.EnsureDaemon(a.mgmtPort); err != nil {
				fmt.Fprintf(os.Stderr, "warn: daemon: %v\n", err)
			}
		}
	}

	var runErr error
	if a.iface != nil {
		runErr = a.iface.Run(ctx, a.registry)
	} else {
		<-ctx.Done()
	}

	// Cleanup (reverse order of setup).
	if a.mgmtPort != "" {
		daemon.RemoveAppPortFile(a.Name())
	}

	if stopErr := a.Stop(ctx); stopErr != nil {
		if runErr == nil {
			runErr = stopErr
		}
	}
	return runErr
}
