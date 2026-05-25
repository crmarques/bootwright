package cli

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/contextstore"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/locality"
)

var bastionLocalityPolicy = locality.DefaultPolicy

func enforceContextLocality(ctx contextstore.Context) error {
	state, err := desiredstate.LoadNormalizeValidate(ctx.InputPaths)
	if err != nil {
		return err
	}
	return enforceBastionLocality(state)
}

func enforceBastionLocality(state v1alpha1.State) error {
	result := locality.CheckBastion(state, bastionLocalityPolicy)
	if result.OK {
		return nil
	}
	return fmt.Errorf("bootwright must run on the declared bastion host: %s", result.Evidence)
}
