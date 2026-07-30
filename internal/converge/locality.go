package converge

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/locality"
	"github.com/crmarques/bootwright/internal/state/desired"
	"github.com/crmarques/bootwright/internal/workspace"
)

func EnforceContextLocality(ctx workspace.Context, policy locality.Policy) error {
	state, err := desiredstate.LoadNormalizeInputFiles(ctx.InputPaths)
	if err != nil {
		return err
	}
	return EnforceControllerLocality(state, policy)
}

func EnforceContextLocalityTolerant(ctx workspace.Context, policy locality.Policy) ([]error, error) {
	loaded, err := desiredstate.LoadNormalizeTolerant(ctx.InputPaths)
	if err != nil {
		return nil, err
	}
	return loaded.Skipped, EnforceControllerLocality(loaded.State, policy)
}

func EnforceControllerLocality(state v1alpha1.State, policy locality.Policy) error {
	result := locality.CheckController(state, policy)
	if result.OK {
		return nil
	}
	return fmt.Errorf("bootwright must run from the local bastion context: %s", result.Evidence)
}
