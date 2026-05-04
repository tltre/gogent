package iface

import (
	"context"

	"github.com/tltre/gagent/pkg/component"
)

type Interface interface {
	Run(ctx context.Context, reg *component.Registry) error
}
