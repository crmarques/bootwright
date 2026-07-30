package cli

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/infra/locality"
	"github.com/crmarques/bootwright/internal/workspace"
)

var controllerLocalityPolicy = locality.DefaultPolicy

func enforceContextLocality(ctx workspace.Context) error {
	return converge.EnforceContextLocality(ctx, controllerLocalityPolicy)
}

func enforceContextLocalityTolerant(ctx workspace.Context) ([]error, error) {
	return converge.EnforceContextLocalityTolerant(ctx, controllerLocalityPolicy)
}

func enforceControllerLocality(state v1alpha1.State) error {
	return converge.EnforceControllerLocality(state, controllerLocalityPolicy)
}
