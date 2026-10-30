package cli

import "github.com/crmarques/bootwright/internal/reconciliation/lifecycle"

const (
	additiveRoot     = "op-6f1c2a9e0b7d4c3f8a5e2d1b0c9f8e7a"
	additiveHead     = "op-0a4b8c2d6e1f3a5b7c9d0e2f4a6b8c1d"
	additiveRevision = "rev-3b8d0f5a9c1e4d7b2a6f8c0e1d3b5a7f"
)

func additivePlanFixture() *lifecycle.PlanResult {
	return &lifecycle.PlanResult{
		Context: lifecycle.ContextIdentity{Name: "lab", Revision: additiveRevision}, Verb: "apply", AddsTo: additiveRoot,
		Steps: []lifecycle.PlanStep{
			{ID: "addon-metallb-ocp-01", Description: "install the add-on metallb on ocp-01", Stage: "add-ons", Impacts: []string{"create-subscription metallb-operator"}, State: "pending", Selection: lifecycle.StepStart, Wave: 1},
		},
		Waves: 1, Widest: 1, Bound: 1, Startable: 1,
		Receipt: lifecycle.Receipt{Operation: "none", Verb: "plan", State: "preview", Next: "apply"},
	}
}

func lineagePlanFixture() *lifecycle.PlanResult {
	return &lifecycle.PlanResult{
		Context: lifecycle.ContextIdentity{Name: "lab", Revision: additiveRevision}, Verb: "destroy", TakesBack: []string{additiveRoot, additiveHead},
		Steps: []lifecycle.PlanStep{
			{ID: "addon-metallb-ocp-01", Description: "retire the add-on metallb on ocp-01", Stage: "add-ons", State: "pending", Selection: lifecycle.StepStart, Wave: 1},
			{ID: "cluster-ocp-01", Description: "remove the cluster ocp-01", Stage: "clusters", State: "pending", Selection: lifecycle.StepStart, After: []int{1}, Wave: 2},
		},
		Waves: 2, Widest: 1, Bound: 1, Startable: 2,
		Receipt: lifecycle.Receipt{Operation: additiveHead, Verb: "plan", State: "preview", Next: "destroy"},
	}
}

func additiveDiff() *lifecycle.DiffResult {
	result := diffNone()
	result.Input, result.Adds = "changed", true
	result.Plan = lifecycle.DiffPlan{State: "differs", Added: 1, Rebuilt: []string{}}
	return result
}

func additiveStatus() *lifecycle.StatusResult {
	blocks := []lifecycle.BlockResult{{ID: "addon-metallb-ocp-01", Description: "install the add-on metallb on ocp-01", Stage: "add-ons", State: "done", Attempts: 1}}
	return &lifecycle.StatusResult{
		Context:     lifecycle.ContextIdentity{Name: "lab", Revision: additiveRevision, Mode: "ready"},
		SetupChecks: []lifecycle.SetupCheck{{ID: "controller-binding", Status: "ready"}},
		Desired:     lifecycle.DesiredSummary{Revision: additiveRevision, Environment: "lab", Files: 6, Objects: 6},
		Clusters:    []lifecycle.ClusterSummary{{Name: "ocp-01", Kind: "OpenShiftCluster", Status: lifecycle.RealizationDone}},
		Secrets:     lifecycle.SecretSummary{Declared: 1, Bindings: 1},
		Handoff:     lifecycle.HandoffSummary{Ready: true},
		NextSteps:   []string{"bootwright destroy --context lab"},
		Lifecycle: &lifecycle.LifecycleSummary{
			Operation: additiveHead, Verb: "apply", State: "done", Next: "none", Blocks: blocks,
			Logs: []string{"contexts/lab/state/operations/" + additiveHead + "/logs/operation.jsonl"}, Executable: "1.4.0 (9f2c1ab)",
			Lineage: []lifecycle.LineageEntry{
				{Operation: additiveRoot, Verb: "apply", State: "done"},
				{Operation: additiveHead, Verb: "apply", State: "done"},
			},
		},
		Contradictions: []string{},
		LogLocation:    "/var/lib/bootwright/contexts/lab/state/operations/" + additiveHead + "/logs",
	}
}

func additiveGoldens() []cliGolden {
	return []cliGolden{
		{golden: "cli-plan-adds", args: "plan", record: func(r *dispatchRecord) { r.result.lifecyclePlan = additivePlanFixture() }},
		{golden: "cli-plan-adds-json", args: "plan --output json", record: func(r *dispatchRecord) { r.result.lifecyclePlan = additivePlanFixture() }},
		{golden: "cli-plan-lineage-destroy", args: "plan", record: func(r *dispatchRecord) { r.result.lifecyclePlan = lineagePlanFixture() }},
		{golden: "cli-plan-lineage-destroy-json", args: "plan --output json", record: func(r *dispatchRecord) { r.result.lifecyclePlan = lineagePlanFixture() }},
		{golden: "cli-diff-adds", args: "diff", record: func(r *dispatchRecord) { r.result.lifecycleDiff = additiveDiff() }},
		{golden: "cli-diff-adds-json", args: "diff --output json", record: func(r *dispatchRecord) { r.result.lifecycleDiff = additiveDiff() }},
		{golden: "cli-status-lineage", args: "status", record: func(r *dispatchRecord) { r.result.lifecycleStatus = additiveStatus() }},
		{golden: "cli-status-lineage-json", args: "status --output json", record: func(r *dispatchRecord) { r.result.lifecycleStatus = additiveStatus() }},
	}
}
