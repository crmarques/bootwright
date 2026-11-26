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

type keptRunner struct {
	runs    []lifecycle.RunRequest
	removed map[string]bool
	tried   map[string]bool
}

func (r *keptRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.runs = append(r.runs, request)
	var frozen Request
	if err := json.Unmarshal(request.Canonical, &frozen); err != nil {
		return lifecycle.RunResult{}, err
	}
	teardown := KeptTeardown{FSID: frozen.FSID, Hosts: []KeptTeardownHost{}, Owner: frozen.Owner, Request: request.Digest, Version: KeptTeardownVersion}
	for _, host := range frozen.Hosts {
		removed := r.removed == nil || r.removed[host.Name]
		teardown.Hosts = append(teardown.Hosts, KeptTeardownHost{Attempted: removed || r.tried[host.Name], Host: host.Name, Removed: removed})
	}
	data, _ := json.Marshal(teardown)
	outcome := reconciliation.OutcomeChanged
	if request.Operation == "observe" {
		outcome = ""
	}
	return lifecycle.RunResult{Outcome: outcome, Evidence: data}, nil
}

func keptExecution(t *testing.T, r Request) lifecycle.Execution {
	t.Helper()
	e := tiebreakerExecution(t, r)
	e.RetiredWithHosts = false
	e.Block.Inverse = &reconciliation.Inverse{KeptHosts: requestMachines(r)}
	return e
}

func keptRequestBlock(t *testing.T, r Request) reconciliation.Block {
	t.Helper()
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: string(api.CephCluster), Object: r.Cluster, Request: data}}
}

// B1862: a Ceph block is taken back while its hosts stay running only while
// no step of the cluster that hands out access ever completed or may have:
// the cluster (its dashboard login) and each access-producing block refuse
// once done, and while unknown or running, since resolving them may prove
// them done; they are admitted only failed or pending. A pool never refuses,
// and a physical host always refuses (B1885).
func TestACephBlockIsTakenBackFromKeptHostsOnlyWhileNothingHandedOutAccess(t *testing.T) {
	capability := New(api.CephCluster, nil)
	access := map[string]func(*Request){
		"its dashboard login":              func(*Request) {},
		"the CephX key of client app":      func(r *Request) { r.Action, r.Client = "client", &Client{Name: "app"} },
		"the S3 keys of object user alice": func(r *Request) { r.Action, r.ObjectUser = "objectUser", &ObjectUser{Name: "alice"} },
		"object bucket media":              func(r *Request) { r.Action, r.ObjectBucket = "objectBucket", &ObjectBucket{Name: "media"} },
		"NFS export share":                 func(r *Request) { r.Action, r.NFSExport = "nfsExport", &NFSExport{Name: "share"} },
		"an NVMe-oF subsystem":             func(r *Request) { r.Action, r.NVMeOF = "nvmeof", &NVMeOFSubsystem{} },
		"an object gateway endpoint":       func(r *Request) { r.Action, r.Gateway = "gateway", &Gateway{} },
		"service mgmt-gateway":             func(r *Request) { r.Action, r.Service = "service", &ServiceUnit{Type: "mgmt-gateway"} },
	}
	for handed, shape := range access {
		r := coreRequest()
		shape(&r)
		block := keptRequestBlock(t, r)
		for state, refused := range map[reconciliation.BlockState]string{
			reconciliation.BlockDone: "B491", reconciliation.BlockUnknown: "bootwright apply", reconciliation.BlockRunning: "bootwright apply",
			reconciliation.BlockFailed: "", reconciliation.BlockPending: "",
		} {
			refusal := capability.KeptHostsRefusal(block, lifecycle.RemovalRecords{State: state}, false)
			if refused == "" && refusal != "" || refused != "" && (!strings.Contains(refusal, handed) || !strings.Contains(refusal, refused)) {
				t.Fatalf("a %s block handing out %s answered %q", state, handed, refusal)
			}
		}
	}
	pool := coreRequest()
	pool.Action, pool.Pool = "pool", &Pool{Name: "data"}
	for _, state := range []reconciliation.BlockState{reconciliation.BlockDone, reconciliation.BlockUnknown} {
		if refusal := capability.KeptHostsRefusal(keptRequestBlock(t, pool), lifecycle.RemovalRecords{State: state}, false); refusal != "" {
			t.Fatalf("a %s pool refused %q", state, refusal)
		}
	}
	physical := coreRequest()
	physical.Hosts[0].Physical = true
	if refusal := capability.KeptHostsRefusal(keptRequestBlock(t, physical), lifecycle.RemovalRecords{State: reconciliation.BlockFailed}, false); !strings.Contains(refusal, "B1885") {
		t.Fatalf("a cluster with a physical host answered %q", refusal)
	}
	hosts, err := capability.OnlineKeptHosts(keptRequestBlock(t, providedStretchRequest(t)))
	if err != nil || !slices.Equal(hosts, requestMachines(providedStretchRequest(t))) || !slices.Contains(hosts, "tie") {
		t.Fatalf("the cluster runs its teardown on %v (%v), want every host Machine, the provided one included", hosts, err)
	}
	if implementation, digest := capability.KeptHostsContract(); implementation != KeptImplementation || digest != KeptContentDigest() {
		t.Fatalf("the cluster's kept-host contract is %s %s", implementation, digest)
	}
	if implementation, digest := NewHost(nil).KeptHostsContract(); implementation != KeptHostImplementation || digest != KeptHostContentDigest() {
		t.Fatalf("the host preparation's kept-host contract is %s %s", implementation, digest)
	}
}

// The cluster block's removal on kept hosts is the native kept teardown on
// every host, never the online destroy B491 holds closed nor the retirement
// that assumes deleted hosts; a host that is not proved clean refuses.
func TestTheClusterBlockTearsTheClusterDownFromItsKeptHosts(t *testing.T) {
	r := coreRequest()
	r.Owner, r.FSID = "apply-original", FSIDFor("apply-original", r.Cluster)
	runner := &keptRunner{}
	result, err := New(api.CephCluster, runner).WithProofs(dependencyProofs{}).Destroy(context.Background(), keptExecution(t, r))
	if err != nil || result.Outcome != reconciliation.OutcomeChanged || len(runner.runs) != 1 {
		t.Fatalf("destroy %+v %+v after %d runs", result, diagnostics.Of(err), len(runner.runs))
	}
	if run := runner.runs[0]; run.Operation != "destroy" || run.MaterialValues["retirement"] != keptRetirement {
		t.Fatalf("the teardown ran %q with %v", run.Operation, run.MaterialValues)
	}
	teardown, err := decodeKeptTeardown(result.Evidence, r, strings.Repeat("b", 64))
	if err != nil || len(teardown.Hosts) != len(r.Hosts) {
		t.Fatalf("the teardown recorded %s (%v)", result.Evidence, err)
	}
	runner = &keptRunner{removed: map[string]bool{"node-1": true, "node-2": true}}
	if _, err := New(api.CephCluster, runner).WithProofs(dependencyProofs{}).Destroy(context.Background(), keptExecution(t, r)); err == nil {
		t.Fatal("a teardown that left node-3 unclean completed")
	}
	quiescence, err := New(api.CephCluster, nil).Quiescent(context.Background(), lifecycle.Probe{Block: keptExecution(t, r).Block})
	if err != nil || !quiescence.Settled() {
		t.Fatalf("a kept-host removal answered quiescence %+v (%v)", quiescence, err)
	}
}

// Observing the teardown reads it complete only when every host is clean,
// partial once any host recorded an attempt, and without effect otherwise; a
// pool's removal records that it goes with its cluster and calls nothing.
func TestTheKeptTeardownIsObservedPerHost(t *testing.T) {
	r := coreRequest()
	r.Owner, r.FSID = "apply-original", FSIDFor("apply-original", r.Cluster)
	for name, runner := range map[string]struct {
		runner *keptRunner
		effect reconciliation.EffectState
	}{
		"clean":   {&keptRunner{}, reconciliation.EffectCompleted},
		"partial": {&keptRunner{removed: map[string]bool{"node-1": true}}, reconciliation.EffectPartial},
		"none":    {&keptRunner{removed: map[string]bool{}}, reconciliation.EffectNoEffect},
		"tried":   {&keptRunner{removed: map[string]bool{}, tried: map[string]bool{"node-2": true}}, reconciliation.EffectPartial},
	} {
		observed, err := New(api.CephCluster, runner.runner).WithProofs(dependencyProofs{}).ObserveRemoval(context.Background(), keptExecution(t, r))
		if err != nil || observed.Effect != runner.effect || runner.runner.runs[0].MaterialValues["retirement"] != keptRetirement {
			t.Fatalf("%s: observed %+v (%v)", name, observed, err)
		}
	}
	pool := coreRequest()
	pool.Action, pool.Pool = "pool", &Pool{Name: "data"}
	e := keptExecution(t, pool)
	e.Block.Kind, e.Block.Object = string(api.CephPool), "data"
	runner := &keptRunner{}
	result, err := New(api.CephPool, runner).WithProofs(dependencyProofs{}).Destroy(context.Background(), e)
	var recorded KeptTeardown
	if err != nil || len(runner.runs) != 0 || json.Unmarshal(result.Evidence, &recorded) != nil || recorded.Version != KeptTeardownVersion || len(recorded.Hosts) != 0 {
		t.Fatalf("a pool's kept removal recorded %s %+v after %d runs", result.Evidence, diagnostics.Of(err), len(runner.runs))
	}
}

// The host preparation block's removal on a kept host removes its own records
// there and keeps the packages and the image.
func TestAKeptHostReleaseRemovesOnlyItsPreparationRecords(t *testing.T) {
	e, r, proofs := hostExecution(t, "node-1")
	e.Block.Inverse = &reconciliation.Inverse{KeptHosts: []string{"node-1"}}
	evidence := HostEvidence{Version: HostEvidenceVersion, Request: e.Block.RequestDigest, Host: r.Host, Owner: e.ApplyOperation, FSID: FSIDFor(e.ApplyOperation, "example"),
		Image: PinnedRepository + "@" + resolvedDigest, Removed: true, Devices: []HostDevice{}}
	data, _ := json.Marshal(evidence)
	runner := &capturingRunner{recordingRunner: recordingRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: data}}}
	result, err := NewHost(runner).WithProofs(proofs).Destroy(context.Background(), e)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged || runner.calls != 1 || runner.request.MaterialValues["kept"] != "true" || len(runner.known) == 0 {
		t.Fatalf("the kept host release returned %+v %+v over %+v", result, diagnostics.Of(err), runner.request.MaterialValues)
	}
	removal, err := NewHost(nil).KeptHostsRemoval(context.Background(), e.Block)
	if err != nil || len(removal.Retains) != 1 || !strings.Contains(removal.Retains[0], "packages") || !strings.Contains(removal.Impacts[0], "Machine/node-1") {
		t.Fatalf("the kept host release reads %+v (%v)", removal, err)
	}
}
