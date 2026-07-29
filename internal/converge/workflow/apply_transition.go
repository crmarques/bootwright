package workflow

type ApplyTransitionAction string

const (
	ApplyTransitionCreate    ApplyTransitionAction = "create"
	ApplyTransitionReconcile ApplyTransitionAction = "reconcile"
	ApplyTransitionRebuild   ApplyTransitionAction = "rebuild"
	ApplyTransitionRefuse    ApplyTransitionAction = "refuse"
	ApplyTransitionUnchanged ApplyTransitionAction = "unchanged"
)

type ApplyTransition struct {
	Label  string
	Kind   string
	Action ApplyTransitionAction
}

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
		return ApplyTransitionRefuse
	case o.HasForeign():
		return ApplyTransitionRefuse
	case o.HasStructuralDrift():
		switch {
		case mode == ApplyModeRebuild && isOverrideDestructive(o):
			return ApplyTransitionRebuild
		case mode == ApplyModeRebuild:
			return ApplyTransitionReconcile
		default:
			return ApplyTransitionRefuse
		}
	case o.HasReconcilableDrift():
		return ApplyTransitionReconcile
	case o.Class == ConvergeSafetyMissing:
		return ApplyTransitionCreate
	default:
		return ApplyTransitionUnchanged
	}
}
