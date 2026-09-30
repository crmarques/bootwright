package lifecycle

import (
	"context"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// unresolvedRig is a kill rig whose apply lost b's result, and whose next
// observation of b proves nothing: unproved sets why.
func unresolvedRig(ctx context.Context, t *testing.T, unproved func(*killHost)) *killRig {
	t.Helper()
	rig := newKillRig(t)
	rig.host.lost["b"] = 1
	if err := killInvoke(ctx, rig.harness.service, reconciliation.Apply); err == nil {
		t.Fatal("the apply whose attempt of b was lost completed")
	}
	unproved(rig.host)
	return rig
}

// requireUnresolvedRefusal holds a removal's refusal to naming b, why it
// stayed unknown and its remedy.
func requireUnresolvedRefusal(t *testing.T, err error, why Unresolved) {
	t.Helper()
	want := []diagnostics.Diagnostic{
		diagnostics.Of(unresolvedFailure("b", why))[0],
		{Severity: "error", Code: "lifecycle.unknown", Message: "this removal cannot prove what these effects left behind, so it registered nothing: b",
			Remediation: "do what the diagnostic of each reports, then repeat bootwright destroy"},
	}
	if got := diagnostics.Of(err); !slices.Equal(got, want) {
		t.Fatalf("the removal reported %+v, want %+v", got, want)
	}
}

// unresolvedStatus is what status names about b: the operation holding it and
// why its outcome is unproved.
func unresolvedStatus(t *testing.T, service Service) (string, Unresolved) {
	t.Helper()
	status, err := service.Status(context.Background(), StatusRequest{ContextName: testContextName})
	if err != nil || status.Lifecycle == nil {
		t.Fatalf("status = %+v (%v)", status, err)
	}
	for _, block := range status.Lifecycle.Blocks {
		if block.ID != "b" {
			if block.Unresolved != nil {
				t.Fatalf("the proved block %s reads unresolved %+v", block.ID, block.Unresolved)
			}
			continue
		}
		if block.Unresolved == nil {
			t.Fatalf("the unproved block b reads %s with no reason", block.State)
		}
		return status.Lifecycle.Verb + " " + status.Lifecycle.State + ", b " + block.State, *block.Unresolved
	}
	t.Fatal("status names no block b")
	return "", Unresolved{}
}

// A removal over an apply whose block b no observation can prove refuses
// having registered nothing, whether a foreign object holds b's target or b's
// host does not answer, and names why. Killed at any write of that refusal, as
// a process dying there would, the store reads back, holds nothing past its
// invariants and still holds the apply; the replayed removal refuses again
// with the same reason, which status then names, and no removal effect reached
// the host. Once the operator removes the foreign object or restores the host,
// one removal resolves b and removes everything the apply started.
func TestAnUnresolvableRemovalKilledAtAnyWriteRefusesAgainUntilItsTargetIsRestored(t *testing.T) {
	ctx := context.Background()
	pristine := killPristine(t)
	for name, test := range map[string]struct {
		unproved, restored func(*killHost)
		why                Unresolved
	}{
		"a foreign object at the target": {
			unproved: func(h *killHost) { delete(h.partial, "b"); h.foreign["b"] = true },
			restored: func(h *killHost) { delete(h.foreign, "b") },
			why:      killForeign("b"),
		},
		"a host that does not answer": {
			unproved: func(h *killHost) { h.silent["b"] = true },
			restored: func(h *killHost) { delete(h.silent, "b") },
			why:      killSilent("b"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			rig := unresolvedRig(ctx, t, test.unproved)
			if held, why := unresolvedStatus(t, rig.harness.service); held != "apply unknown, b unknown" || why != unobserved {
				t.Fatalf("before any resolution status reads %s: %+v", held, why)
			}
			names := &killPoints{counts: map[string]int{}}
			rig.arm(names)
			requireUnresolvedRefusal(t, killInvoke(ctx, rig.harness.service, reconciliation.Destroy), test.why)
			if len(names.keys) == 0 {
				t.Fatal("the refused removal made no durable write")
			}
			for index, key := range names.keys {
				t.Run(key, func(t *testing.T) {
					killed := unresolvedRig(ctx, t, test.unproved)
					points := &killPoints{counts: map[string]int{}, at: index + 1}
					killed.arm(points)
					_ = killInvoke(ctx, killed.harness.service, reconciliation.Destroy)
					snapshot := points.snapshot
					if snapshot == nil || len(points.keys) <= index || points.keys[index] != key {
						t.Fatalf("kill point %d is not %s in a repeated run: %v", index+1, key, points.keys)
					}
					service := killed.over(snapshot)
					failures := append(killReadBack(snapshot.workspace), killInvariants(snapshot, pristine)...)
					if len(failures) != 0 {
						t.Fatalf("the killed refusal left %v", failures)
					}
					if held, why := unresolvedStatus(t, service); held != "apply unknown, b unknown" || why != unobserved && why != test.why {
						t.Fatalf("the killed refusal left status reading %s: %+v", held, why)
					}
					requireUnresolvedRefusal(t, killInvoke(ctx, service, reconciliation.Destroy), test.why)
					if held, why := unresolvedStatus(t, service); held != "apply unknown, b unknown" || why != test.why {
						t.Fatalf("the replayed refusal left status reading %s: %+v", held, why)
					}
					for _, block := range []string{"a", "b", "c"} {
						if removed := snapshot.host.effectCount(reconciliation.Destroy, block); removed != 0 {
							t.Fatalf("an unproved removal reached the host's %s %d times", block, removed)
						}
					}
					test.restored(snapshot.host)
					if err := killInvoke(ctx, service, reconciliation.Destroy); err != nil {
						t.Fatalf("the removal after the target was restored failed: %v", err)
					}
					if rest, realized := killRest(snapshot.workspace), snapshot.host.realizedBlocks(); rest != "removed" || len(realized) != 0 || len(snapshot.workspace.reservations) != 0 {
						t.Fatalf("the restored removal left the context %s, the host holding %v and reservations %v", rest, realized, snapshot.workspace.reservations)
					}
				})
			}
		})
	}
}
