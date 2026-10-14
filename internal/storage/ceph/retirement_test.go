package ceph

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func clientExecution(t *testing.T) lifecycle.Execution {
	t.Helper()
	r := coreRequest()
	r.Action, r.Client = "client", &Client{Name: "consumer", Pool: "volumes", Access: "read-write"}
	e := executionFor(t, r)
	e.Block.ID, e.Block.Kind, e.Block.Object = "ceph-client-consumer", string(api.StorageClient), "consumer"
	return e
}

func retiredExecutions(t *testing.T) map[api.Kind]lifecycle.Execution {
	t.Helper()
	cluster := executionFor(t, coreRequest())
	cluster.Operation, cluster.RetiredWithHosts = "destroy-later", true
	client := clientExecution(t)
	client.Operation, client.RetiredWithHosts = "destroy-later", true
	return map[api.Kind]lifecycle.Execution{api.StorageCluster: cluster, api.StorageClient: client}
}

func TestHostMachinesNameEveryHostMachineOnce(t *testing.T) {
	r := coreRequest()
	r.Hosts = append(r.Hosts, r.Hosts[0])
	r.Hosts[0].Name = "node-1-alias"
	e := executionFor(t, r)
	machines, err := New(api.StorageCluster, nil).HostMachines(e.Block)
	if err != nil || !slices.Equal(machines, []string{"node-1", "node-2", "node-3"}) {
		t.Fatalf("host machines = %v (%v)", machines, err)
	}
	e.Block.Request = json.RawMessage(`{"version":"ceph-core-v0"}`)
	if machines, err := New(api.StorageCluster, nil).HostMachines(e.Block); err == nil {
		t.Fatalf("an undecodable request named hosts %v", machines)
	}
}

func TestQuiescenceDerivesFromRetirementWithEveryHostOnly(t *testing.T) {
	noHosts := coreRequest()
	noHosts.Hosts = nil
	for name, tc := range map[string]struct {
		block   reconciliation.Block
		retired bool
		state   string
		reason  string
		remedy  string
	}{
		"retired with its hosts":         {executionFor(t, coreRequest()).Block, true, lifecycle.Quiescent, retiredReason, ""},
		"its hosts outlive this destroy": {executionFor(t, coreRequest()).Block, false, lifecycle.Unproved, survivingReason, survivingRemedy},
		"retired naming no host":         {executionFor(t, noHosts).Block, true, lifecycle.Unproved, survivingReason, survivingRemedy},
		"retired with an undecodable request": {reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: json.RawMessage(`{}`)}},
			true, lifecycle.Unproved, survivingReason, survivingRemedy},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &recordingRunner{}
			q, err := New(api.StorageCluster, runner).Quiescent(context.Background(), lifecycle.Probe{Block: tc.block, RetiredWithHosts: tc.retired})
			if err != nil || q.State != tc.state || q.Reason != tc.reason || q.Remedy != tc.remedy || q.Stop != "" || runner.calls != 0 {
				t.Fatalf("quiescence = %+v (%v), runner calls %d", q, err, runner.calls)
			}
		})
	}
	if !strings.Contains(survivingReason, "(B491)") || !strings.Contains(survivingReason, "hosts outlive this destroy") {
		t.Fatalf("surviving-host reason = %q", survivingReason)
	}
	if !strings.Contains(survivingRemedy, "same destroy as every Bootwright-owned virtual host Machine") || !strings.Contains(survivingRemedy, "B491") || strings.Contains(survivingRemedy, "stop what is running") {
		t.Fatalf("surviving-host remedy = %q", survivingRemedy)
	}
}

func TestRetiredDestroyAndItsObservationRecordRetirementWithoutTheRunner(t *testing.T) {
	for kind, e := range retiredExecutions(t) {
		t.Run(string(kind), func(t *testing.T) {
			runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: json.RawMessage(`{}`)}}
			capability := New(kind, runner).WithProofs(dependencyProofs{})
			unflagged := withMaterial(e, coreRequest())
			unflagged.RetiredWithHosts = false
			if _, err := capability.Destroy(context.Background(), unflagged); err == nil || runner.calls != 1 || runner.request.Operation != "destroy" {
				t.Fatalf("the unflagged destroy did not reach the runner: calls %d (%v)", runner.calls, err)
			}
			if _, err := capability.ObserveRemoval(context.Background(), unflagged); err == nil || runner.calls != 2 || runner.request.Operation != "observe" {
				t.Fatalf("the unflagged removal observation did not reach the runner: calls %d (%v)", runner.calls, err)
			}
			runner.calls = 0
			e.ApplyProved = nil
			result, err := capability.Destroy(context.Background(), e)
			if err != nil || result.Outcome != reconciliation.OutcomeChanged || len(result.Produced) != 0 || runner.calls != 0 {
				t.Fatalf("retired destroy = %+v (%v), runner calls %d", result, err, runner.calls)
			}
			var recorded Retirement
			if err := json.Unmarshal(result.Evidence, &recorded); err != nil {
				t.Fatal(err)
			}
			r, err := capability.identity(context.Background(), e)
			if err != nil {
				t.Fatal(err)
			}
			want := Retirement{Action: r.Action, Cluster: "example", FSID: FSIDFor("apply-original", "example"), RetiredWith: []string{"node-1", "node-2", "node-3"}}
			if recorded.Action != want.Action || recorded.Cluster != want.Cluster || recorded.FSID != want.FSID || !slices.Equal(recorded.RetiredWith, want.RetiredWith) {
				t.Fatalf("retirement evidence = %+v, want %+v", recorded, want)
			}
			observed, err := capability.ObserveRemoval(context.Background(), e)
			if err != nil || observed.Effect != reconciliation.EffectCompleted || string(observed.Evidence) != string(result.Evidence) || len(observed.Produced) != 0 || runner.calls != 0 {
				t.Fatalf("retired removal observation = %+v (%v), runner calls %d", observed, err, runner.calls)
			}
		})
	}
}

func TestRetirementEvidenceIsAcceptedOnlyForARetiredDestroyOfItsOwnScope(t *testing.T) {
	e := retiredExecutions(t)[api.StorageCluster]
	capability := New(api.StorageCluster, nil)
	result, err := capability.Destroy(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	r, err := capability.identity(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if effect, err := EvaluateRetirement(result.Evidence, r, true); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("own retirement = %s (%v)", effect, err)
	}
	if _, err := EvaluateRetirement(result.Evidence, r, false); err == nil {
		t.Fatal("retirement evidence accepted without the retirement flag")
	}
	for _, removal := range []bool{false, true} {
		if _, err := EvaluateEvidence(result.Evidence, r, e.Block.RequestDigest, removal); err == nil {
			t.Fatalf("native evaluation accepted the retirement shape (removal %v)", removal)
		}
	}
	tampered := map[string]string{
		"another fsid":          strings.Replace(string(result.Evidence), r.FSID, FSIDFor("apply-replacement", "example"), 1),
		"a host left behind":    strings.Replace(string(result.Evidence), `,"node-3"`, "", 1),
		"another action":        strings.Replace(string(result.Evidence), `"action":"cluster"`, `"action":"pool"`, 1),
		"another cluster":       strings.Replace(string(result.Evidence), `"cluster":"example"`, `"cluster":"other"`, 1),
		"an extra member":       strings.Replace(string(result.Evidence), `{"action"`, `{"absent":true,"action"`, 1),
		"a null host list":      strings.Replace(string(result.Evidence), `["node-1","node-2","node-3"]`, "null", 1),
		"non-canonical spacing": strings.Replace(string(result.Evidence), `,"cluster"`, `, "cluster"`, 1),
	}
	for name, data := range tampered {
		if data == string(result.Evidence) {
			t.Fatalf("%s: the tampering did not change %s", name, data)
		}
		if _, err := EvaluateRetirement([]byte(data), r, true); err == nil {
			t.Fatalf("%s: tampered retirement accepted: %s", name, data)
		}
	}
}

func TestRetiredDestroyStillRefusesAnotherFrozenIdentity(t *testing.T) {
	runner := &recordingRunner{}
	e := retiredExecutions(t)[api.StorageCluster]
	e.Block.Object = "other"
	result, err := New(api.StorageCluster, runner).Destroy(context.Background(), e)
	if d := diagnostics.Of(err); result.Outcome != reconciliation.OutcomeFailed || len(d) != 1 || d[0].Code != "lifecycle.state" || runner.calls != 0 {
		t.Fatalf("a mismatched retirement = %+v (%v), runner calls %d", result, err, runner.calls)
	}
	e = retiredExecutions(t)[api.StorageCluster]
	e.ApplyOperation = ""
	if observed, err := New(api.StorageCluster, runner).ObserveRemoval(context.Background(), e); err == nil || observed.Effect != reconciliation.EffectUnknown || runner.calls != 0 {
		t.Fatalf("a retirement without the original apply = %+v (%v)", observed, err)
	}
}
