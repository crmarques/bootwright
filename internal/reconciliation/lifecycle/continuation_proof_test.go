package lifecycle

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// refusedContinuation is one incomplete operation whose continuation or
// resolution still has a block to run and whose re-proof refuses on what
// status reads, with the verb that refuses, the refusal's code and message
// and, where it names one, the deletion it offers as its exit.
type refusedContinuation struct {
	name    string
	arrange func(*testing.T) *harness
	steps   []string
	verb    reconciliation.Verb
	code    string
	message string
	exit    string
}

func refusedContinuations() []refusedContinuation {
	unbound := func(h *harness) { h.workspace.controller.State.Bindings = nil }
	moved := func(h *harness) { h.service.automation = testAutomation{digest: strings.Repeat("9", 64)} }
	closure := func(h *harness) {
		approve(&h.workspace.controller, strings.Repeat("c", 64), prerequisites.Definition{Bootstrap: movedBootstrap()})
	}
	failedApplyUnder := func(change func(*harness)) func(*testing.T) *harness {
		return func(t *testing.T) *harness {
			h := newHarness(t, "alpha")
			failedApply(t, h)
			change(h)
			return h
		}
	}
	unknownRemovalUnder := func(change func(*harness)) func(*testing.T) *harness {
		return func(t *testing.T) *harness {
			h := newHarness(t, "alpha")
			unknownRemoval(t, h)
			change(h)
			return h
		}
	}
	const deletion = "bootwright context delete --name lab --purge --allow-orphans"
	const movedClosure = "the approved execution bundle holds another Python and Ansible closure (Python 3.14.7, ansible-core 2.21.5) than the one this operation registered with"
	const earlierBuild = "this operation was registered by an earlier build that froze no Python and Ansible closure, " +
		"and bootwright devel (abcdef1) cannot read this host's controller directory, which now keeps setup runs"
	return []refusedContinuation{
		{
			name: "an unbound failed apply", arrange: failedApplyUnder(unbound),
			steps: []string{"bootwright destroy --context lab"}, verb: reconciliation.Apply,
			code: "controller.identity", message: "this context is not bound to a controller host",
		},
		{
			name: "an unbound unknown removal", arrange: unknownRemovalUnder(unbound),
			steps: []string{deletion}, verb: reconciliation.Destroy,
			code: "controller.identity", message: "this context is not bound to a controller host", exit: deletion,
		},
		{
			name: "a failed apply under a moved automation digest", arrange: failedApplyUnder(moved),
			steps: []string{"bootwright destroy --context lab"}, verb: reconciliation.Apply,
			code: "lifecycle.state", message: "this executable's automation differs from the one this operation froze",
		},
		{
			name: "an unknown removal under a moved automation digest", arrange: unknownRemovalUnder(moved),
			steps: []string{}, verb: reconciliation.Destroy,
			code: "lifecycle.state", message: "this executable's automation differs from the one this operation froze",
		},
		{
			name: "a failed apply under a moved closure", arrange: failedApplyUnder(closure),
			steps: []string{"bootwright destroy --context lab"}, verb: reconciliation.Apply,
			code: "lifecycle.state", message: movedClosure,
		},
		{
			name: "an unknown removal under a moved closure", arrange: unknownRemovalUnder(closure),
			steps: []string{}, verb: reconciliation.Destroy,
			code: "lifecycle.state", message: movedClosure,
		},
		{
			name: "an unknown removal an earlier build registered beside setup runs", arrange: func(t *testing.T) *harness {
				h := newHarness(t, "alpha")
				unknownRemoval(t, h)
				registeredEarlier(t, h)
				h.workspace.controller.SetupRuns = true
				return h
			},
			steps: []string{deletion}, verb: reconciliation.Destroy,
			code: "lifecycle.state", message: earlierBuild, exit: deletion,
		},
	}
}

// Status offers a continuation or resolution that still has a block to run
// only where its re-proof passes over what status reads: the frozen input,
// automation and closure, and the context's binding. Where that re-proof
// refuses, status offers the exit the refusal names and status may run: the
// removal that supersedes an incomplete apply, which status offers anyway,
// and over a removal the context's deletion where the refusal names it, and
// otherwise nothing. Each offered verb decides, and the refused continuation
// refuses unchanged before any effect.
func TestStatusOffersOnlyTheContinuationItsProofsAdmit(t *testing.T) {
	ctx := context.Background()
	for _, row := range refusedContinuations() {
		t.Run(row.name, func(t *testing.T) {
			h := row.arrange(t)
			status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
			if err != nil || !slices.Equal(status.NextSteps, row.steps) {
				t.Fatalf("status = %+v (%v), want steps %q", status, err, row.steps)
			}
			requireContinuationRefuses(t, row.arrange(t), row)
			for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
				if offers(row.steps, verb) {
					requireDecidesIfOffered(t, row.arrange(t), verb, true)
				}
			}
		})
	}
}

// requireContinuationRefuses runs the refused continuation confirmed. It
// refuses with the re-proof's own diagnostic, naming the exit status offers
// where status offers one, and writes and performs nothing.
func requireContinuationRefuses(t *testing.T, h *harness, row refusedContinuation) {
	t.Helper()
	before := untouchedOf(h)
	var err error
	if row.verb == reconciliation.Apply {
		_, err = h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	} else {
		_, err = h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	}
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != row.code || reported[0].Message != row.message ||
		(row.exit != "" && !containsCommand(reported[0].Remediation, row.exit)) {
		t.Fatalf("the %s status withholds = %+v (%v), want %s %q naming %q", row.verb, reported, err, row.code, row.message, row.exit)
	}
	before.require(t, h)
}

// The refused continuations an operator meets most, a failed apply whose
// binding is gone or whose automation moved and an unknown removal whose
// binding is gone, have a golden of what status, a plan preview and both
// verbs report over them.
func TestARefusedContinuationMatchesItsGolden(t *testing.T) {
	reports := map[string]recordStateReport{}
	for _, row := range refusedContinuations()[:3] {
		reports[row.name] = reportOver(t, row.arrange)
	}
	data, err := json.Marshal(reports)
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "lifecycle-refused-continuation", withoutIdentities(data), false)
}
