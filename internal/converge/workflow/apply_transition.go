package workflow

// ApplyTransitionAction is what apply would do to one object under a given mode — the
// display twin of the EvaluateApplyModePreflight decision, used by the read-only
// plan/--dry-run change ledger.
type ApplyTransitionAction string

const (
	// ApplyTransitionCreate: no completed apply is recorded (or the object is only
	// partially applied) — apply provisions/resumes it.
	ApplyTransitionCreate ApplyTransitionAction = "create"
	// ApplyTransitionReconcile: recorded and drifted, but the drift is reconcilable
	// in place (a Ceph OSD-device add, a storage set-* edit, a day-2 reconfigure) —
	// converged without destroying data, OS, or VM.
	ApplyTransitionReconcile ApplyTransitionAction = "reconcile"
	// ApplyTransitionRebuild: a destructive --override rebuild (machine reinstall
	// with disks wiped, container/Ceph cluster wipe-and-rebuild).
	ApplyTransitionRebuild ApplyTransitionAction = "rebuild"
	// ApplyTransitionRefuse: this mode fails closed on the object (structural drift
	// under continue, foreign ownership, or any recorded object under --expect-new).
	ApplyTransitionRefuse ApplyTransitionAction = "refuse"
	// ApplyTransitionUnchanged: recorded and matching — a no-op.
	ApplyTransitionUnchanged ApplyTransitionAction = "unchanged"
)

// ApplyTransition is one object's planned transition for the change ledger.
type ApplyTransition struct {
	Label  string
	Kind   string
	Action ApplyTransitionAction
}

// ClassifyApplyTransitions maps each classified object to the transition apply would
// perform under mode, for a read-only change preview. It never mutates and mirrors
// EvaluateApplyModePreflight's decision so the ledger and the gate cannot disagree.
func ClassifyApplyTransitions(objects []ObjectClassification, mode ApplyMode) []ApplyTransition {
	out := make([]ApplyTransition, 0, len(objects))
	for _, o := range objects {
		out = append(out, ApplyTransition{Label: o.Label, Kind: o.Kind, Action: applyTransitionAction(o, mode)})
	}
	return out
}

func applyTransitionAction(o ObjectClassification, mode ApplyMode) ApplyTransitionAction {
	switch {
	case !o.Recorded():
		return ApplyTransitionCreate
	case mode == ApplyModeCreate:
		// --expect-new asserts greenfield: any already-recorded object is refused.
		return ApplyTransitionRefuse
	case o.HasForeign():
		return ApplyTransitionRefuse
	case o.HasStructuralDrift():
		switch {
		case mode == ApplyModeOverride && isOverrideDestructive(o):
			return ApplyTransitionRebuild
		case mode == ApplyModeOverride:
			// Structural but reconfigure-only: --override re-applies in place.
			return ApplyTransitionReconcile
		default:
			return ApplyTransitionRefuse
		}
	case o.HasReconcilableDrift():
		return ApplyTransitionReconcile
	case o.Class == ConvergeSafetyMissing:
		// Partially applied (some backing task never completed): resume/create it.
		return ApplyTransitionCreate
	default:
		return ApplyTransitionUnchanged
	}
}
