package app

import (
	"context"
	"fmt"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/iface"
)

type App struct {
	config   *Config
	registry *component.Registry
	iface    iface.Interface
}

func (a *App) Registry() *component.Registry {
	return a.registry
}

func (a *App) Interface() iface.Interface {
	return a.iface
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
	return a.registry.StopAll(ctx)
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

func (a *App) Run(ctx context.Context) error {
	if err := a.Initialize(ctx); err != nil {
		return err
	}
	if err := a.Start(ctx); err != nil {
		return err
	}

	if a.iface != nil {
		err := a.iface.Run(ctx, a.registry)
		if err != nil {
			return err
		}
	} else {
		<-ctx.Done()
	}

	return a.Stop(ctx)
}
