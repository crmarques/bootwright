package bundlelocal

import (
	"context"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type bundleLaunch struct {
	Loader      string
	Arguments   []string
	Directory   string
	Environment []string
}

type bundleGuard interface {
	WithPython(context.Context, prerequisites.BundleArea, prerequisites.ExecutionRequirement, func(bundleLaunch, func() error) error) error
}
