package status

import (
	"strings"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func LedgerNextSteps(ledger workflow.RunLedger, activity workflow.RunActivity, existing []string) []string {
	switch ledger.Status {
	case workflow.RunStatusRunning:
		if activity.State == workflow.RunActivityStale {
			return append([]string{ledgerRetryCommand(ledger)}, existing...)
		}
		return append([]string{"bootwright status --watch"}, existing...)
	case workflow.RunStatusFailed:
		hints := []string{ledgerRetryCommand(ledger)}
		for _, task := range ledger.FailedTasks() {
			if task.LogPath != "" {
				hints = append(hints, "inspect "+task.LogPath)
			}
		}
		return append(hints, existing...)
	default:
		return existing
	}
}

// ledgerRetryCommand renders the retry hint by mapping the ledger's recorded
// target back to the verb and scope flags that produced it, so the hint re-runs
// the SAME operation:
//   - a destroy-labelled target (destroy stamps "<stage> destroy", e.g.
//     "clusters destroy", via ExecuteDestroyGraph) retries as `bootwright
//     destroy`, never a re-apply of what a teardown just failed to remove;
//   - a `--through` prefix scope ("through-<phase>") retries with `--through`;
//   - a family or sub-phase stage (infra|clusters|fabric|machines|deps|base|
//     add-ons) retries with `--stage`, so a narrow sub-phase rerun keeps its
//     scope instead of silently widening to a full apply;
//   - the remaining scope names (all, container-cluster, storage-cluster) carry
//     no stage flag.
func ledgerRetryCommand(ledger workflow.RunLedger) string {
	command := "bootwright apply"
	if stage, ok := ledgerDestroyStage(ledger.Target); ok {
		command = "bootwright destroy"
		if stage != "" {
			command += " --stage " + stage
		}
	} else if through, ok := strings.CutPrefix(ledger.Target, "through-"); ok {
		command += " --through " + through
	} else if ledgerTargetIsApplyStage(ledger.Target) {
		command += " --stage " + ledger.Target
	}
	if ledger.Scope != "" {
		command += " --clusters " + ledger.Scope
	}
	return command + " --yes"
}

// ledgerDestroyStage reports whether a ledger target was written by destroy —
// destroy stamps "<stage> destroy" (see internal/converge ExecuteDestroyGraph) —
// and returns the family stage to thread back into the destroy retry ("" for a
// full destroy, whose target is "all destroy"). Apply targets never carry the
// "destroy" token, which cleanly separates the two verbs.
func ledgerDestroyStage(target string) (string, bool) {
	fields := strings.Fields(target)
	destroy := false
	for _, f := range fields {
		if f == "destroy" {
			destroy = true
			break
		}
	}
	if !destroy {
		return "", false
	}
	if len(fields) > 0 {
		switch fields[0] {
		case "infra", "clusters":
			return fields[0], true
		}
	}
	return "", true
}

// ledgerTargetIsApplyStage reports whether an apply ledger target maps directly
// to a single `--stage` value (the two families plus the five sub-phases). The
// remaining apply scope names (all, container-cluster, storage-cluster) and the
// through-<phase> prefixes are handled separately by ledgerRetryCommand.
func ledgerTargetIsApplyStage(target string) bool {
	switch target {
	case "infra", "clusters", "fabric", "machines", "deps", "base", "add-ons":
		return true
	}
	return false
}
