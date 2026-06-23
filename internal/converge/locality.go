package converge

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/locality"
	"github.com/crmarques/bootwright/internal/state/desired"
	"github.com/crmarques/bootwright/internal/workspace"
)

// EnforceContextLocality loads the context's desired state and applies the
// controller locality gate. The policy is owned by the CLI (which also feeds
// it to preflight and SSH-trust flows) and passed in as a parameter.
//
// It loads and normalizes without validating: the gate only needs the
// controller/bastion topology, and validation belongs to the explicit load
// step the command runs next. Keeping it unvalidated also lets a scoped run
// (apply/destroy --scoped-validation) reach its narrowed validation instead of
// failing here on a desired-state error in an out-of-scope object.
func EnforceContextLocality(ctx workspace.Context, policy locality.Policy) error {
	state, err := desiredstate.LoadNormalizeInputFiles(ctx.InputPaths)
	if err != nil {
		return err
	}
	return EnforceControllerLocality(state, policy)
}

func EnforceControllerLocality(state v1alpha1.State, policy locality.Policy) error {
	result := locality.CheckController(state, policy)
	if result.OK {
		return nil
	}
	return fmt.Errorf("bootwright must run from the local bastion context: %s", result.Evidence)
}
