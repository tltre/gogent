package iface

import (
	"context"

	"github.com/tltre/gogent/pkg/component"
)

type Interface interface {
	Run(ctx context.Context, reg *component.Registry) error
}
