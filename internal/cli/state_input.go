package cli

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/state/desired"
)

func loadDesiredState(cf *commonFlags) (v1alpha1.State, error) {
	ctx, err := cf.resolve()
	if err != nil {
		return v1alpha1.State{}, err
	}
	return desiredstate.LoadNormalizeValidateInputFiles(ctx.InputPaths)
}

// loadDesiredStateScopedValidation loads the full effective desired state but
// validates only the resources the scopeTarget/clusterScope selection will act
// on, so a desired-state error in an out-of-scope object (e.g. a broken
// StorageCluster when applying --clusters ocp1,ocp2) does not block the scoped
// run. It returns the full state: downstream scoping derives the work set and
// shared-service conflict checks still need to see the unselected clusters.
//
// Validation runs through desiredstate.ValidateScoped, which compares the scoped
// findings against the full-state findings and drops the ones that are artifacts
// of scoping (a reference orphaned only because its referent was scoped out, such
// as an InfraComponent on a Machine no consumer pulls in, or a render-reference
// data-foundation binding whose ContainerCluster the storage scope drops). A
// genuine in-scope error still appears in both sets and blocks the run.
func loadDesiredStateScopedValidation(cf *commonFlags, scopeTarget, clusterScope string) (v1alpha1.State, error) {
	ctx, err := cf.resolve()
	if err != nil {
		return v1alpha1.State{}, err
	}
	state, err := desiredstate.LoadNormalizeInputFiles(ctx.InputPaths)
	if err != nil {
		return v1alpha1.State{}, err
	}
	scoped, err := clusteraccess.ScopeStateForApply(state, scopeTarget, clusterScope)
	if err != nil {
		return v1alpha1.State{}, err
	}
	if err := desiredstate.ValidateScoped(scoped, state); err != nil {
		return v1alpha1.State{}, err
	}
	return state, nil
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
