package lifecycle

import (
	"context"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type candidateVerdict struct {
	decided bool
	exits   []string
}

// decideCandidate runs verb on h confirmed, as the operator would after
// copying the step: the controller setup is proved after the confirmation, so
// only a confirmed run reaches every refusal. A verb that passes the records
// decides; one that refuses names the exits its remedies give, which are the
// only exits status offers in its place.
func decideCandidate(h *harness, verb reconciliation.Verb) candidateVerdict {
	var err error
	if verb == reconciliation.Apply {
		_, err = h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	} else {
		_, err = h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	}
	if passesTheRecords(err) {
		return candidateVerdict{decided: true}
	}
	var verdict candidateVerdict
	deletion := "bootwright context delete --name " + testContextName + " --purge"
	for _, reported := range diagnostics.Of(err) {
		switch {
		case strings.Contains(reported.Remediation, deletion+" --allow-orphans"):
			verdict.exits = append(verdict.exits, deletion+" --allow-orphans")
		case strings.Contains(reported.Remediation, deletion):
			verdict.exits = append(verdict.exits, deletion)
		case strings.Contains(reported.Remediation, "bootwright controller install"):
			verdict.exits = append(verdict.exits, "bootwright controller install")
		}
	}
	return verdict
}

// passesTheRecords is whether err leaves the records' decision passed: none,
// or only a refusal that a token the step carries or a read of the target
// settles, neither of which status can make.
func passesTheRecords(err error) bool {
	if err == nil {
		return true
	}
	reported := diagnostics.Of(err)
	return len(reported) != 0 && !slices.ContainsFunc(reported, func(item diagnostics.Diagnostic) bool {
		return item.Code != "lifecycle.authorization" && item.Code != "lifecycle.unknown"
	})
}

func stepWithoutTokens(step string) string {
	step, _, _ = strings.Cut(step, " --authorize ")
	return step
}

// The candidates status draws its steps from are the operation's own verb
// (the first plan and apply beside no operation) and the removal that
// supersedes an incomplete apply. Each is derived here from what running that
// verb does, with no reading of status: a verb that decides is offered, one
// that refuses is replaced by the setup or deletion its remedy names, and no
// other refusal leaves a step. Every step status offers must be derived, and
// every derived step must be offered.
func TestStatusStepsAreExactlyWhatTheVerbsDecisionsDerive(t *testing.T) {
	ctx := context.Background()
	failed := map[string]Result{"charlie": {Outcome: reconciliation.OutcomeFailed}}
	chained := func(arrange func(*testing.T, *harness)) func(*testing.T) *harness {
		return func(t *testing.T) *harness {
			h := newPlannedHarness(t, chainedDefinitions())
			arrange(t, h)
			return h
		}
	}
	unprepared := func(arrange func(*testing.T) *harness) func(*testing.T) *harness {
		return func(t *testing.T) *harness {
			h := arrange(t)
			h.workspace.controller.Prepared = false
			return h
		}
	}
	unknownApply := func(t *testing.T) *harness {
		h := newHarness(t, "alpha")
		applyChained(t, h, map[string]Result{"alpha": {Outcome: reconciliation.OutcomeUnknown}})
		return h
	}
	lagging := func(t *testing.T) *harness {
		h, _ := laggingApply(t)
		return h
	}
	rows := map[string]func(*testing.T) *harness{
		"no operation": func(t *testing.T) *harness { return newHarness(t, "alpha") },
		"no operation and no controller record": func(t *testing.T) *harness {
			h := newHarness(t, "alpha")
			h.workspace.controller.Exists, h.workspace.controller.Initialized = false, false
			return h
		},
		"no operation and a failed receipt": unprepared(func(t *testing.T) *harness { return newHarness(t, "alpha") }),
		"a lost index": chained(func(t *testing.T, h *harness) {
			completeApply(t, h)
			lose(h, "index.json")
		}),
		"a lost index beside pristine evidence": chained(func(t *testing.T, h *harness) {
			completeRemoval(t, h)
			lose(h, "index.json")
		}),
		"a lost index beside unreadable evidence": chained(func(t *testing.T, h *harness) {
			completeApply(t, h)
			lose(h, "index.json")
			h.workspace.evidence = []byte("{")
		}),
		"a completed destroy holding a block not done": chained(func(t *testing.T, h *harness) {
			completeRemoval(t, h)
			lose(h, path.Join(currentOperation(t, h), "blocks", "bravo")+"/")
		}),
		"a lost block record": chained(func(t *testing.T, h *harness) {
			applyChained(t, h, failed)
			lose(h, path.Join(currentOperation(t, h), "blocks", "charlie", "state.json"))
		}),
		"a dependency not done beside a dependent that started": chained(func(t *testing.T, h *harness) {
			applyChained(t, h, failed)
			rewriteState(t, h, path.Join(currentOperation(t, h), "blocks", "bravo", "state.json"), string(reconciliation.BlockFailed))
		}),
		"a failed apply that started nothing": chained(func(t *testing.T, h *harness) {
			applyChained(t, h, map[string]Result{"alpha": {Outcome: reconciliation.OutcomeFailed}})
			lose(h, path.Join(currentOperation(t, h), "blocks", "alpha")+"/")
		}),
		"a running destroy with a lost block record": chained(func(t *testing.T, h *harness) {
			failedChainedRemoval(t, h)
			removal := currentOperation(t, h)
			rewriteState(t, h, path.Join(removal, "operation.json"), string(reconciliation.OperationRunning))
			lose(h, path.Join(removal, "blocks", "charlie", "state.json"))
		}),
		"a failed destroy": chained(func(t *testing.T, h *harness) { failedChainedRemoval(t, h) }),
		"a failed destroy with a lost block record": chained(func(t *testing.T, h *harness) {
			failedChainedRemoval(t, h)
			lose(h, path.Join(currentOperation(t, h), "blocks", "charlie", "state.json"))
		}),
		"an unknown apply":                         unknownApply,
		"an unknown apply and a failed receipt":    unprepared(unknownApply),
		"a lagging apply":                          lagging,
		"a lagging apply and a failed receipt":     unprepared(lagging),
		"a failed destroy and a failed receipt":    unprepared(chained(func(t *testing.T, h *harness) { failedChainedRemoval(t, h) })),
		"an unknown apply whose blocks are done":   unknownApplyWithEveryBlockDone,
		"an unknown destroy whose blocks are done": unknownDestroyWithEveryBlockDone,
	}
	for name, arrange := range rows {
		t.Run(name, func(t *testing.T) {
			status, err := arrange(t).service.Status(ctx, StatusRequest{ContextName: testContextName})
			if err != nil {
				t.Fatal(err)
			}
			candidates := []reconciliation.Verb{reconciliation.Apply}
			idle := status.Lifecycle == nil
			if !idle {
				candidates = []reconciliation.Verb{reconciliation.Verb(status.Lifecycle.Verb)}
				if candidates[0] == reconciliation.Apply && status.Lifecycle.State != "done" {
					candidates = append(candidates, reconciliation.Destroy)
				}
			}
			var derived []string
			for _, verb := range candidates {
				verdict := decideCandidate(arrange(t), verb)
				if verdict.decided {
					derived = append(derived, "bootwright "+string(verb)+" --context "+testContextName)
					if idle {
						derived = append(derived, "bootwright plan --context "+testContextName)
					}
				}
				derived = append(derived, verdict.exits...)
			}
			offered := []string{}
			for _, step := range status.NextSteps {
				offered = append(offered, stepWithoutTokens(step))
			}
			slices.Sort(derived)
			slices.Sort(offered)
			if !slices.Equal(slices.Compact(derived), offered) {
				t.Fatalf("status offers %q, the verbs' own decisions derive %q", offered, slices.Compact(derived))
			}
		})
	}
	if len(rows) < 20 {
		t.Fatalf("%d arrangements; the parity sweep has shrunk", len(rows))
	}
}
