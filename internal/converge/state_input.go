package converge

import (
	"fmt"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/state/desired"
	"github.com/crmarques/bootwright/internal/workspace"
)

// SnapshotMutatingRunInput records the loaded input YAML files under
// snapshotDir at the start of a mutating apply or destroy run. The snapshot
// is a forensic output (what was applied); plan and --dry-run never write it.
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
