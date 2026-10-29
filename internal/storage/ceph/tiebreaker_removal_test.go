package ceph

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

type tiebreakerRunner struct {
	runs     []lifecycle.RunRequest
	answer   func(lifecycle.RunRequest) (lifecycle.RunResult, error)
	evidence TiebreakerEvidence
}

func (r *tiebreakerRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.runs = append(r.runs, request)
	if r.answer != nil {
		return r.answer(request)
	}
	data, _ := json.Marshal(r.evidence)
	return lifecycle.RunResult{Outcome: "changed", Evidence: data}, nil
}

func tiebreakerExecution(t *testing.T, r Request) lifecycle.Execution {
	t.Helper()
	frozen := r
	frozen.Owner, frozen.FSID = "", ""
	e := executionFor(t, frozen)
	e.Operation, e.RetiredWithHosts, e.Attempt = "destroy-later", true, 1
	key, err := trust.ParseAuthorizedKey(string(hostPublicKey()))
	if err != nil {
		t.Fatal(err)
	}
	e.Material = map[string]secrets.Material{"fleet-key": secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: []byte("fleet-private")}),
		"tie-known-hosts": secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(key.Line(tieAddress, 22))})}
	for _, h := range r.Hosts {
		if !h.Provided {
			e.Material[h.HostKeyRef] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.PublicKeyPart: hostPublicKey()})
		}
	}
	return e
}

func providedStretchRequest(t *testing.T) Request {
	t.Helper()
	r := stretchRequest(t)
	for i := range r.Hosts {
		if !r.Hosts[i].Provided {
			r.Hosts[i].HostKeyRef = r.Hosts[i].Name + "-host-key"
		}
	}
	return r
}

func removedTiebreaker(r Request, removed, attempted bool) TiebreakerEvidence {
	return TiebreakerEvidence{Attempted: attempted, FSID: r.FSID, Host: "tie", Owner: r.Owner, Removed: removed, Request: strings.Repeat("b", 64), Version: TiebreakerEvidenceVersion}
}

func TestTheSurvivingHostBindingLeavesOutOnlyTheProvidedTiebreaker(t *testing.T) {
	r := providedStretchRequest(t)
	e := tiebreakerExecution(t, r)
	for _, kind := range []api.Kind{api.CephCluster, api.CephPool} {
		machines, err := New(kind, nil).HostMachines(e.Block)
		if err != nil || !slices.Equal(machines, []string{"node-1", "node-2", "node-3", "node-4"}) {
			t.Fatalf("%s host machines = %v (%v)", kind, machines, err)
		}
	}
	if !slices.Contains(requestMachines(r), "tie") || !slices.Equal(clusterRetirement(r).RetiredWith, requestMachines(r)) {
		t.Fatalf("the retirement record no longer names the tiebreaker: %v", clusterRetirement(r).RetiredWith)
	}
	q, err := New(api.CephCluster, nil).Quiescent(context.Background(), lifecycle.Probe{Block: e.Block, RetiredWithHosts: true})
	if err != nil || q.State != lifecycle.Quiescent {
		t.Fatalf("quiescence %+v (%v)", q, err)
	}
	unprovided := coreRequest()
	if machines, _ := New(api.CephCluster, nil).HostMachines(executionFor(t, unprovided).Block); !slices.Equal(machines, requestMachines(unprovided)) {
		t.Fatalf("an installed host left the binding: %v", machines)
	}
}

func TestACompleteTiebreakerDestroyRemovesItsFootprintBeforeTheRetirementRecordGolden(t *testing.T) {
	r := providedStretchRequest(t)
	runner := &tiebreakerRunner{evidence: removedTiebreaker(r, true, true)}
	result, err := New(api.CephCluster, runner).WithProofs(dependencyProofs{}).Destroy(context.Background(), tiebreakerExecution(t, r))
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("destroy %+v %+v", result, diagnostics.Of(err))
	}
	if len(runner.runs) != 1 {
		t.Fatalf("runs %d", len(runner.runs))
	}
	run := runner.runs[0]
	if run.Operation != "destroy" || run.MaterialValues["retirement"] != tiebreakerRetirement || len(run.MaterialValues) != 1 || len(run.Outputs) != 0 || run.ReadOnly {
		t.Fatalf("run %s values %v outputs %v", run.Operation, run.MaterialValues, run.Outputs)
	}
	for _, file := range run.Materials {
		if file.Secret == "cluster-key" || file.Secret == r.RegistryRef {
			t.Fatalf("the removal lent %s", file.Secret)
		}
	}
	for reason, machine := range map[string]string{"tiebreaker-foreign": "Machine/tie", "tiebreaker-unreachable": "Machine/tie at 198.51.100.10:22", "transport": "Machine/tie"} {
		if d := diagnostics.Of(run.Refusals[reason]); len(d) != 1 || !strings.Contains(d[0].Message, machine) || !strings.Contains(d[0].Remediation, "B938") {
			t.Fatalf("%s refusal %+v", reason, d)
		}
	}
	var recorded Retirement
	if err := json.Unmarshal(result.Evidence, &recorded); err != nil || recorded.Version != RetirementVersion || recorded.Tiebreaker == nil ||
		*recorded.Tiebreaker != (RetiredTiebreaker{Host: "tie", Machine: "tie", Removed: true}) || !slices.Contains(recorded.RetiredWith, "tie") {
		t.Fatalf("retirement %s (%v)", result.Evidence, err)
	}
	matchesGolden(t, "retirement-cluster-stretch", result.Evidence, false)
	data, _ := json.Marshal(runner.evidence)
	matchesGolden(t, "evidence-tiebreaker-removed", data, false)
	if read, err := decodeTiebreakerEvidence(data, r, r.Hosts[slices.IndexFunc(r.Hosts, func(h Host) bool { return h.Provided })], strings.Repeat("b", 64)); err != nil || read != runner.evidence {
		t.Fatalf("tiebreaker evidence read back %+v %v", read, err)
	}
	if effect, err := EvaluateRetirement(result.Evidence, r, true); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("own record %s %v", effect, err)
	}
}

func TestATiebreakerDestroyRecordsNoRetirementUnlessAbsenceIsProved(t *testing.T) {
	r := providedStretchRequest(t)
	lost := errors.New("the reply was lost")
	for name, answer := range map[string]func(lifecycle.RunRequest) (lifecycle.RunResult, error){
		"lost reply": func(lifecycle.RunRequest) (lifecycle.RunResult, error) { return lifecycle.RunResult{}, lost },
		"unreachable": func(request lifecycle.RunRequest) (lifecycle.RunResult, error) {
			return lifecycle.RunResult{}, request.Refusals["tiebreaker-unreachable"]
		},
		"foreign": func(request lifecycle.RunRequest) (lifecycle.RunResult, error) {
			return lifecycle.RunResult{}, request.Refusals["tiebreaker-foreign"]
		},
		"not removed": func(lifecycle.RunRequest) (lifecycle.RunResult, error) {
			data, _ := json.Marshal(removedTiebreaker(r, false, true))
			return lifecycle.RunResult{Outcome: "changed", Evidence: data}, nil
		},
		"another host": func(lifecycle.RunRequest) (lifecycle.RunResult, error) {
			e := removedTiebreaker(r, true, true)
			e.Host = "node-1"
			data, _ := json.Marshal(e)
			return lifecycle.RunResult{Outcome: "changed", Evidence: data}, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &tiebreakerRunner{answer: answer}
			result, err := New(api.CephCluster, runner).WithProofs(dependencyProofs{}).Destroy(context.Background(), tiebreakerExecution(t, r))
			if err == nil || result.Evidence != nil || result.Outcome == reconciliation.OutcomeChanged || len(runner.runs) != 1 {
				t.Fatalf("destroy %+v %v after %d runs", result, err, len(runner.runs))
			}
		})
	}
}

func TestEveryReadOnlyRefusalPrecedesTheTiebreakerRemoval(t *testing.T) {
	r := providedStretchRequest(t)
	e := tiebreakerExecution(t, r)
	e.ApplyOperation = ""
	runner := &tiebreakerRunner{evidence: removedTiebreaker(r, true, true)}
	if _, err := New(api.CephCluster, runner).WithProofs(dependencyProofs{}).Destroy(context.Background(), e); err == nil || len(runner.runs) != 0 {
		t.Fatalf("a refused retirement reached the tiebreaker: %v after %d runs", err, len(runner.runs))
	}
	e = tiebreakerExecution(t, r)
	e.ApplyProved = nil
	if _, err := New(api.CephCluster, runner).WithProofs(dependencyProofs{}).Destroy(context.Background(), e); err == nil || len(runner.runs) != 0 {
		t.Fatalf("an unproved image reached the tiebreaker: %v after %d runs", err, len(runner.runs))
	}
}

func TestOnlyTheStretchClusterBlockReachesItsProvidedTiebreaker(t *testing.T) {
	r := providedStretchRequest(t)
	r.Action, r.Pool = "pool", &Pool{Name: "volumes", Object: "volumes"}
	e := tiebreakerExecution(t, r)
	e.Block.ID, e.Block.Kind, e.Block.Object = "ceph-pool-volumes", string(api.CephPool), "volumes"
	runner := &tiebreakerRunner{}
	result, err := New(api.CephPool, runner).WithProofs(dependencyProofs{}).Destroy(context.Background(), e)
	var recorded Retirement
	if err != nil || len(runner.runs) != 0 || json.Unmarshal(result.Evidence, &recorded) != nil || recorded.Tiebreaker != nil || recorded.Version != RetirementVersion {
		t.Fatalf("pool retirement %s %+v after %d runs", result.Evidence, diagnostics.Of(err), len(runner.runs))
	}
}

func TestTheTiebreakerRemovalObservationReadsTheHostBeforeAnyRepeat(t *testing.T) {
	r := providedStretchRequest(t)
	for name, test := range map[string]struct {
		evidence TiebreakerEvidence
		fail     bool
		effect   reconciliation.EffectState
	}{
		"absent":                    {removedTiebreaker(r, true, true), false, reconciliation.EffectCompleted},
		"absent without an attempt": {removedTiebreaker(r, true, false), false, reconciliation.EffectCompleted},
		"partly removed":            {removedTiebreaker(r, false, true), false, reconciliation.EffectPartial},
		"untouched":                 {removedTiebreaker(r, false, false), false, reconciliation.EffectNoEffect},
		"unreachable":               {TiebreakerEvidence{}, true, reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &tiebreakerRunner{evidence: test.evidence}
			if test.fail {
				runner.answer = func(request lifecycle.RunRequest) (lifecycle.RunResult, error) {
					return lifecycle.RunResult{}, request.Refusals["tiebreaker-unreachable"]
				}
			}
			e := tiebreakerExecution(t, r)
			observed, err := New(api.CephCluster, runner).WithProofs(dependencyProofs{}).ObserveRemoval(context.Background(), e)
			if observed.Effect != test.effect || (err != nil) != test.fail || len(runner.runs) != 1 || runner.runs[0].Operation != "observe" ||
				runner.runs[0].MaterialValues["retirement"] != tiebreakerRetirement || len(runner.runs[0].Outputs) != 0 {
				t.Fatalf("observation %+v %v", observed, err)
			}
			if test.fail && !strings.Contains(diagnostics.Of(err)[0].Message, "never taken as cleaned") && !strings.Contains(diagnostics.Of(err)[0].Remediation, "never taken as cleaned") {
				t.Fatalf("unreachable %+v", diagnostics.Of(err))
			}
			if effect, _ := EvaluateRetirement(observed.Evidence, r, true); (test.effect == reconciliation.EffectCompleted) != (effect == reconciliation.EffectCompleted) {
				t.Fatalf("evidence %s", observed.Evidence)
			}
		})
	}
}

func TestARetirementRecordOfAnotherVersionRefusesByName(t *testing.T) {
	r := providedStretchRequest(t)
	older := retirementFor(r)
	data, _ := json.Marshal(older)
	_, err := EvaluateRetirement(data, r, true)
	refusedWith(t, err, "lifecycle.state", "the Ceph retirement record carries no version, and this build reads only ceph-core-retirement-v2",
		"continue this operation with the Bootwright build that recorded it; this build converts no older record")
	stray := clusterRetirement(r)
	stray.Tiebreaker.Removed = false
	data, _ = json.Marshal(stray)
	if effect, err := EvaluateRetirement(data, r, true); err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("an uncleaned tiebreaker was accepted: %s %v", effect, err)
	}
}
