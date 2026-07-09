package converge

import (
	"fmt"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/state/desired"
	"github.com/crmarques/bootwright/internal/workspace"
)

func SnapshotMutatingRunInput(snapshotDir string, ctx workspace.Context) error {
	files, err := desiredstate.LoadedInputFiles(ctx.InputPaths)
	if err != nil {
		return fmt.Errorf("snapshot run input: %w", err)
	}
	if err := workflow.SnapshotRunInput(snapshotDir, ctx.InputDir, files); err != nil {
		return fmt.Errorf("snapshot run input: %w", err)
	}
	return nil
}
