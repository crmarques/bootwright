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

// loadDesiredStateLocalOnly loads desired state without the locality (root)
// enforcement a normal run does — it only reads the user-owned input YAML. Shell
// completion callbacks use it: they run as the unprivileged user and must not
// escalate, and any failure just yields no completions.
func loadDesiredStateLocalOnly(cf *commonFlags) (v1alpha1.State, error) {
	ctx, err := cf.resolveLocalOnly()
	if err != nil {
		return v1alpha1.State{}, err
	}
	return desiredstate.LoadNormalizeValidateInputFiles(ctx.InputPaths)
}
