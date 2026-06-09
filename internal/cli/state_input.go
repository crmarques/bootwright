package cli

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/runtime/context"
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

// snapshotMutatingRunInput records the loaded input YAML files under
// snapshotDir at the start of a mutating apply or destroy run. The snapshot
// is a forensic output (what was applied); plan and --dry-run never write it.
func snapshotMutatingRunInput(snapshotDir string, ctx contextstore.Context) error {
	files, err := desiredstate.LoadedInputFiles(ctx.InputPaths)
	if err != nil {
		return fmt.Errorf("snapshot run input: %w", err)
	}
	if err := workflow.SnapshotRunInput(snapshotDir, ctx.InputDir, files); err != nil {
		return fmt.Errorf("snapshot run input: %w", err)
	}
	return nil
}
