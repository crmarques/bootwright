package cli

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/contextstore"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/locality"
)

var controllerLocalityPolicy = locality.DefaultPolicy

func enforceContextLocality(ctx contextstore.Context) error {
	state, err := desiredstate.LoadNormalizeValidate(ctx.InputPaths)
	if err != nil {
		return err
	}
	return enforceControllerLocality(state)
}

func enforceControllerLocality(state v1alpha1.State) error {
	result := locality.CheckController(state, controllerLocalityPolicy)
	if result.OK {
		return nil
	}
	return fmt.Errorf("bootwright must run from the local controller context: %s", result.Evidence)
}
