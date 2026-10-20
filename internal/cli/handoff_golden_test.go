package cli

import (
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func handoffGoldens() []cliGolden {
	cases := []struct {
		name, verb, state, alpha, bravo string
		reasons                         []string
	}{
		{"ready", "apply", "done", "done", "done", []string{}},
		{"no-apply", "", "", "", "", []string{"no apply is recorded"}},
		{"destroy", "destroy", "done", "done", "done", []string{"the latest operation is a destroy"}},
		{"apply-not-done", "apply", "running", "done", "done", []string{"the apply is running, not done"}},
		{"block-not-done", "apply", "done", "failed", "done", []string{"block alpha is failed, not done"}},
		{"unknown", "apply", "done", "unknown", "done", []string{"block alpha is unknown, not done", "block alpha has an unresolved unknown outcome"}},
		{"log-fault", "apply", "done", "done", "done", []string{"the operation has a latched required-log fault"}},
		{"ordered-reasons", "apply", "unknown", "unknown", "pending", []string{"the apply is unknown, not done", "block alpha is unknown, not done", "block bravo is pending, not done", "block alpha has an unresolved unknown outcome", "the operation has a latched required-log fault"}},
	}
	out := make([]cliGolden, 0, len(cases)*2)
	for _, row := range cases {
		for _, suffix := range []string{"", " --output json"} {
			name := "cli-status-handoff-" + row.name
			if suffix != "" {
				name += "-json"
			}
			out = append(out, cliGolden{
				golden: name, args: "status" + suffix,
				record: func(record *dispatchRecord) {
					result := &lifecycle.StatusResult{
						Context: lifecycle.ContextIdentity{Name: "example", Mode: "ready"},
						Handoff: lifecycle.HandoffSummary{Ready: len(row.reasons) == 0, Reasons: row.reasons},
					}
					if row.verb != "" {
						result.Lifecycle = &lifecycle.LifecycleSummary{
							Operation: "op-6f1c2a9e0b7d4c3f8a5e2d1b0c9f8e7a", Verb: row.verb, State: row.state, Next: "none",
							Blocks: []lifecycle.BlockResult{
								{ID: "alpha", Description: "install alpha", Stage: "clusters", State: row.alpha, Attempts: 1},
								{ID: "bravo", Description: "install bravo", Stage: "clusters", State: row.bravo, Attempts: 1},
							},
						}
					}
					if row.name == "log-fault" {
						result.NextSteps = []string{"bootwright apply --context example"}
					}
					record.result.lifecycleStatus = result
				},
			})
		}
	}
	return out
}
