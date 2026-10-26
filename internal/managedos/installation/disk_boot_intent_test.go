package installation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// An installation interrupted at each step after its proof is resolved from
// what the guest shows and from the before-state its attempt recorded before
// the network restart powered the machine off (Q66, D183): once that durable
// intent names this installation's marker, a powered-off machine that answers
// nothing and shows nothing published is this operation's own partial
// realization, never no effect, and its retry is told the installation is
// proved, so it boots the installed disk and never the installer. A failed
// read stays unknown. Both a virtual and a physical profile are held to it.
func TestAnInstallationProvedBeforeItsPowerOffIsNeverReadAsNoEffect(t *testing.T) {
	profiles := map[string]func(*testing.T) (lifecycle.Execution, Request){
		"virtual":  func(t *testing.T) (lifecycle.Execution, Request) { return execution(t, "digest") },
		"physical": func(t *testing.T) (lifecycle.Execution, Request) { return privateExecution(t, nil) },
	}
	for profile, build := range profiles {
		t.Run(profile, func(t *testing.T) {
			call, request := build(t)
			marker, err := MarkerFor(request, "digest")
			if err != nil {
				t.Fatal(err)
			}
			quoted, _ := json.Marshal(string(marker))
			intent := json.RawMessage(`{"marker":` + string(quoted) + `}`)
			published := request.Private != nil
			for step, want := range map[string]struct {
				evidence Evidence
				intent   bool
				effect   reconciliation.EffectState
			}{
				"before the installation proof": {Evidence{Power: "Off"}, false, reconciliation.EffectNoEffect},
				"after the installation proof": {Evidence{Power: "On", Marker: string(marker), Reachable: true, Image: !published, Private: published},
					true, reconciliation.EffectPartial},
				"after the material withdrawal": {Evidence{Power: "On", Marker: string(marker), Reachable: true}, true, reconciliation.EffectPartial},
				"after the power-off":           {Evidence{Power: "Off"}, true, reconciliation.EffectPartial},
				"after the disk boot":           {Evidence{Power: "On", Marker: string(marker), Reachable: true}, true, reconciliation.EffectPartial},
			} {
				t.Run(step, func(t *testing.T) {
					observed := want.evidence
					observed.Request, observed.Address, observed.NetworkDifferences = "digest", request.Address, []string{}
					data, _ := json.Marshal(observed)
					runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: data}}
					read := call
					if want.intent {
						read.ApplyPreparations = []json.RawMessage{intent}
					}
					observation, err := New(runner).WithIdentities(&pinReader{}).Observe(context.Background(), read)
					if err != nil || observation.Effect != want.effect {
						t.Fatalf("observed %s (%v), want %s", observation.Effect, err, want.effect)
					}
				})
			}
			for name, preparations := range map[string][]json.RawMessage{"no recorded intent": nil, "a recorded intent": {intent}} {
				runner := &fakeRunner{err: errors.New("stopped after its decision")}
				apply := call
				apply.ApplyPreparations = preparations
				_, _ = New(runner).WithIdentities(&pinReader{}).Apply(context.Background(), apply)
				if len(runner.requests) != 1 {
					t.Fatalf("%s: the apply did not reach the adapter", name)
				}
				if got, wanted := runner.requests[0].MaterialValues["installationProved"], map[bool]string{true: "true", false: "false"}[preparations != nil]; got != wanted {
					t.Fatalf("%s: the retry was told installationProved=%q, want %q", name, got, wanted)
				}
			}
			failing := &fakeRunner{err: errors.New("the identity read was refused")}
			read := call
			read.ApplyPreparations = []json.RawMessage{intent}
			if observation, _ := New(failing).WithIdentities(&pinReader{}).Observe(context.Background(), read); observation.Effect != reconciliation.EffectUnknown {
				t.Fatalf("a failed read with a recorded intent observed %s, want unknown", observation.Effect)
			}
		})
	}
}
