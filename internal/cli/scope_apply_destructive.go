package cli

import (
	"fmt"
	"io"
	"strings"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
)

// destructiveApplyConfirmPrompt returns the interactive confirmation prompt for an
// apply, upgrading it to an explicit data-loss prompt (and emitting the object list as
// a warning) when the run would destructively rebuild machines/clusters under --override
// without --allow-destroy. A non-destructive run keeps the routine prompt.
func destructiveApplyConfirmPrompt(stdout io.Writer, destructive []string, allowDestroy bool) string {
	if len(destructive) == 0 || allowDestroy {
		return "Continue with apply? [y/N] (default: no): "
	}
	cliout.NewContinuation(stdout).Warning("override", "will DESTROY and rebuild "+strings.Join(destructive, ", ")+" — disks wiped / Ceph OSD data zapped. This is irreversible.")
	return "Confirm this DESTRUCTIVE rebuild (accept data loss)? [y/N] (default: no): "
}

// destructiveOverrideYesGuard is the data-loss seatbelt for a non-interactive
// destructive --override. --yes skips the routine apply confirm but never authorizes
// data loss (mirroring how --yes never implies --override for destroy protection); only
// --allow-destroy authorizes it non-interactively. An empty destructive set,
// --allow-destroy, or an interactive run (!yes, which reaches the interactive
// data-loss confirm) returns nil. It is independent of destroyProtection: a protected
// environment already fails closed earlier, so this gates the unprotected case where a
// mis-scoped --override could otherwise wipe a machine or cluster with only --yes.
func destructiveOverrideYesGuard(destructive []string, yes, allowDestroy bool) error {
	if len(destructive) == 0 || allowDestroy || !yes {
		return nil
	}
	return fmt.Errorf("apply --override would destructively rebuild %s — disks are wiped and any Ceph OSD data is zapped. --yes does not authorize data loss: add --allow-destroy to proceed non-interactively, or drop --yes to confirm interactively", strings.Join(destructive, ", "))
}
