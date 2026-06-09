package workflow

import (
	"fmt"
	"sort"
	"strings"
)

// EvaluateApplyModePreflight is the Go-side gate that enforces the apply mode
// contract against the recorded convergence state of the selected objects, before
// any mutation. Per-role Ansible gates enforce the same contract against live
// state; this gate gives a fast, meaningful refusal up front. It returns a non-nil
// error when the chosen mode forbids proceeding.
//
//	create:   greenfield assert (--expect-new) — refuse if any selected object
//	          already exists.
//	continue: safe reconcile (the default) — refuse on drift or foreign ownership;
//	          otherwise proceed (missing objects are created, matching objects no-op).
//	override: break-glass — refuse only on foreign ownership; rebuild drifted and
//	          create missing objects; objects already matching are left untouched.
func EvaluateApplyModePreflight(mode ApplyMode, objects []ObjectClassification) error {
	switch mode {
	case ApplyModeCreate:
		var existing []ObjectClassification
		for _, o := range objects {
			if o.Recorded() {
				existing = append(existing, o)
			}
		}
		if len(existing) > 0 {
			return fmt.Errorf("apply --expect-new requires a greenfield environment and these objects already exist: %s; drop --expect-new to reconcile them, or run `bootwright apply --override` to rebuild drifted objects", summarizeApplyObjects(existing))
		}
	case ApplyModeContinue:
		var differ []ObjectClassification
		for _, o := range objects {
			if o.HasForeign() || o.HasDrift() {
				differ = append(differ, o)
			}
		}
		if len(differ) > 0 {
			return fmt.Errorf("apply refuses to mutate objects that differ from their recorded desired state: %s; align the desired state, or run `bootwright apply --override` to rebuild drifted objects (foreign objects are never rebuilt)", summarizeApplyObjects(differ))
		}
	case ApplyModeOverride:
		var foreign []ObjectClassification
		for _, o := range objects {
			if o.HasForeign() {
				foreign = append(foreign, o)
			}
		}
		if len(foreign) > 0 {
			return fmt.Errorf("apply --override never rebuilds objects recorded by another manager: %s; resolve ownership before retrying", summarizeApplyObjects(foreign))
		}
	}
	return nil
}

func summarizeApplyObjects(objs []ObjectClassification) string {
	parts := make([]string, 0, len(objs))
	for _, o := range objs {
		parts = append(parts, fmt.Sprintf("%s (%s)", o.Label, o.Class))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
