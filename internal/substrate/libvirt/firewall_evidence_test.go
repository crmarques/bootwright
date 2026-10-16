package libvirt

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestHostPresenceRequiresItsEnabledOwnedFirewallBarrier(t *testing.T) {
	request := hostRequest(t)
	if request.FirewallReadyUnit == "" {
		t.Fatal("the managed-network request has no startup barrier")
	}
	for name, damage := range map[string]func(*FirewallReadyEvidence){
		"unread":       func(e *FirewallReadyEvidence) { e.Answered = false },
		"missing":      func(e *FirewallReadyEvidence) { e.Present = false },
		"foreign":      func(e *FirewallReadyEvidence) { e.Owned = false },
		"disabled":     func(e *FirewallReadyEvidence) { e.Enabled = false },
		"failed":       func(e *FirewallReadyEvidence) { e.State = "failed" },
		"another unit": func(e *FirewallReadyEvidence) { e.Name = "other.service" },
		"unreported":   func(e *FirewallReadyEvidence) { *e = FirewallReadyEvidence{} },
	} {
		t.Run(name, func(t *testing.T) {
			var evidence HostEvidence
			if err := json.Unmarshal(hostEvidence(request, "digest"), &evidence); err != nil {
				t.Fatal(err)
			}
			damage(&evidence.FirewallReady)
			if err := ValidateHostPresence(encode(t, evidence), request, "digest"); err == nil {
				t.Fatal("presence accepted without the frozen boot barrier")
			}
			if err := ValidateHostUnremoved(encode(t, evidence), request, "digest"); err == nil {
				t.Fatal("a missing or changed boot barrier proved no removal effect")
			}
		})
	}
}

func TestHostFirewallBarrierAbsenceAndInterruptedCleanup(t *testing.T) {
	request := hostRequest(t)
	gone := HostEvidence{Absent: true, Directory: observed(false), PoolAnswered: true, Postcondition: true, Request: "digest", URI: true,
		FirewallReady: FirewallReadyEvidence{Name: request.FirewallReadyUnit, Answered: true}}
	if err := ValidateHostAbsence(encode(t, gone), "digest"); err != nil {
		t.Fatalf("positive absence refused: %v", err)
	}
	for name, damage := range map[string]func(*FirewallReadyEvidence){
		"unread":        func(e *FirewallReadyEvidence) { e.Answered = false },
		"unit or link":  func(e *FirewallReadyEvidence) { e.Present, e.Owned = true, true },
		"enabled link":  func(e *FirewallReadyEvidence) { e.Enabled = true },
		"active unit":   func(e *FirewallReadyEvidence) { e.State = "active" },
		"ready unit":    func(e *FirewallReadyEvidence) { e.Ready = true },
		"foreign proof": func(e *FirewallReadyEvidence) { e.Owned = true },
	} {
		t.Run(name, func(t *testing.T) {
			evidence := gone
			damage(&evidence.FirewallReady)
			if err := ValidateHostAbsence(encode(t, evidence), "digest"); err == nil {
				t.Fatal("unproved barrier removal was accepted")
			}
		})
	}
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	residual := gone
	residual.Absent, residual.Postcondition = false, false
	residual.FirewallReady.Present, residual.FirewallReady.Owned = true, true
	execution := lifecycle.Execution{Block: reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{ID: request.Identity.Block, Request: canonical}, RequestDigest: "digest",
	}}
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(t, residual)}}
		capability := NewHost(runner)
		observe := capability.Observe
		if verb == reconciliation.Destroy {
			observe = capability.ObserveRemoval
		}
		observation, err := observe(context.Background(), execution)
		if err != nil || observation.Effect != reconciliation.EffectPartial {
			t.Fatalf("%s over the residual barrier = %+v, %v", verb, observation, err)
		}
	}
	residual.FirewallReady.Owned = false
	if err := ValidateHostPartial(encode(t, residual), "digest"); err == nil {
		t.Fatal("a foreign barrier was adopted as a partial realization")
	}
}

func TestAnActiveBarrierDoesNotProveTheFirewallStillReady(t *testing.T) {
	request := hostRequest(t)
	var evidence HostEvidence
	if err := json.Unmarshal(hostEvidence(request, "digest"), &evidence); err != nil {
		t.Fatal(err)
	}
	evidence.FirewallReady.Ready = false
	if err := ValidateHostPresence(encode(t, evidence), request, "digest"); err == nil {
		t.Fatal("a stopped or disabled firewall was accepted from an earlier oneshot")
	}
	// The firewall itself is not owned. A stopped firewall alone says nothing
	// was removed from the provider's intact unit, link, networks and pool.
	if err := ValidateHostUnremoved(encode(t, evidence), request, "digest"); err != nil {
		t.Fatalf("a shared prerequisite was mistaken for a removal effect: %v", err)
	}
}

func TestAnExternalOnlyHostRequiresNoFirewallBarrier(t *testing.T) {
	request := hostRequest(t)
	request.FirewallReadyUnit = ""
	for index := range request.Networks {
		request.Networks[index].Managed = false
	}
	if err := ValidateHostPresence(hostEvidence(request, "digest"), request, "digest"); err != nil {
		t.Fatalf("external-only host required a barrier: %v", err)
	}
}
