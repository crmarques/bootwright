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

func loadDesiredStateTolerant(cf *commonFlags) (v1alpha1.State, []error, error) {
	ctx, err := cf.resolve()
	if err != nil {
		return v1alpha1.State{}, nil, err
	}
	loaded, err := desiredstate.LoadNormalizeValidateTolerant(ctx.InputPaths)
	if err != nil {
		return v1alpha1.State{}, nil, err
	}
	return loaded.State, loaded.Skipped, nil
}

func loadDesiredStateWithExclusions(cf *commonFlags) (v1alpha1.State, desiredstate.ClusterSelectionExclusions, error) {
	ctx, err := cf.resolve()
	if err != nil {
		return v1alpha1.State{}, desiredstate.ClusterSelectionExclusions{}, err
	}
	return desiredstate.LoadNormalizeValidateWithExclusions(ctx.InputPaths)
}

func loadOptionalDesiredState(cf *commonFlags) (v1alpha1.State, error) {
	ctx, err := cf.resolve()
	if err != nil {
		return v1alpha1.State{}, err
	}
	return desiredstate.LoadNormalizeValidateInputFiles(ctx.InputPaths)
}

func loadDesiredStateLocalOnly(cf *commonFlags) (v1alpha1.State, error) {
	ctx, err := cf.resolveLocalOnly()
	if err != nil {
		return v1alpha1.State{}, err
	}
	return desiredstate.LoadNormalizeValidateInputFiles(ctx.InputPaths)
}
