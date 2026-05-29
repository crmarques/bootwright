package cli

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/desired"
)

func loadDesiredState(cf *commonFlags) (v1alpha1.State, error) {
	ctx, err := cf.resolve()
	if err != nil {
		return v1alpha1.State{}, err
	}
	return desiredstate.LoadNormalizeValidateInputFiles(ctx.InputPaths)
}

func loadOptionalDesiredState(cf *commonFlags) (v1alpha1.State, error) {
	ctx, err := cf.resolve()
	if err != nil {
		return v1alpha1.State{}, err
	}
	return desiredstate.LoadNormalizeValidateInputFiles(ctx.InputPaths)
}
