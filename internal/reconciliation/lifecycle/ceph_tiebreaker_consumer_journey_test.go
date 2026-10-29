package lifecycle_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage/ceph"
	"github.com/crmarques/bootwright/internal/trust"
)

var (
	stretchInstalled = []string{"node-1", "node-2", "node-3", "node-4"}
	stretchPeers     = []string{"peer-node-1", "peer-node-2", "peer-node-3"}
)

// tiebreakerFleetRunner resolves both clusters' images and answers the only
// host-contacting run a retiring stretch cluster makes: the removal of its
// provided tiebreaker's footprint, which reports absence as removed says.
type tiebreakerFleetRunner struct {
	mutex    sync.Mutex
	removed  bool
	runs     []string
	unwanted []string
}

func (r *tiebreakerFleetRunner) Run(_ context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if run.Implementation == ceph.ImageImplementation {
		request, err := ceph.DecodeImageRequest(run.Canonical)
		if err != nil {
			return lifecycle.RunResult{}, err
		}
		evidence := ceph.ImageEvidence{Absent: true, Postcondition: true, Request: run.Digest}
		if run.Operation != "destroy" {
			evidence = ceph.ImageEvidence{Base: request.Base, Digest: "sha256:" + strings.Repeat("ab", 32), Platform: ceph.PinnedPlatform, Postcondition: true, Release: request.Release, Request: run.Digest, Tag: request.Tag}
		}
		data, _ := json.Marshal(evidence)
		return lifecycle.RunResult{Outcome: "changed", Evidence: data}, nil
	}
	if run.Implementation != ceph.Implementation || run.MaterialValues["retirement"] != "tiebreaker" {
		r.unwanted = append(r.unwanted, run.Block+":"+run.Operation)
		return lifecycle.RunResult{}, errors.New("a retiring block contacted its hosts")
	}
	r.runs = append(r.runs, run.Block+":"+run.Operation)
	request, err := ceph.DecodeRequest(run.Canonical)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	data, _ := json.Marshal(ceph.TiebreakerEvidence{Attempted: true, FSID: request.FSID, Host: "tie", Owner: request.Owner, Removed: r.removed, Request: run.Digest, Version: ceph.TiebreakerEvidenceVersion})
	return lifecycle.RunResult{Outcome: "changed", Evidence: data}, nil
}

func (r *tiebreakerFleetRunner) took() ([]string, []string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	runs, unwanted := r.runs, r.unwanted
	r.runs, r.unwanted = nil, nil
	return runs, unwanted
}

// The apply's native work is out of this journey's scope: each Ceph block
// records the apply as done, so its destroy runs the real capability.
type appliedCluster struct{ ceph.Capability }

func (appliedCluster) Apply(context.Context, lifecycle.Execution) (lifecycle.Result, error) {
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"applied":true}`)}, nil
}

func (appliedCluster) Observe(context.Context, lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"applied":true}`)}, nil
}

type appliedConsumer struct{ ceph.ConsumerCapability }

func (appliedConsumer) Apply(context.Context, lifecycle.Execution) (lifecycle.Result, error) {
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"applied":true}`)}, nil
}

func (appliedConsumer) Observe(context.Context, lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"applied":true}`)}, nil
}

type appliedReplication struct{ ceph.ReplicationCapability }

func (appliedReplication) Apply(context.Context, lifecycle.Execution) (lifecycle.Result, error) {
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"applied":true}`)}, nil
}

func (appliedReplication) Observe(context.Context, lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"applied":true}`)}, nil
}

func cephGolden[T any](t *testing.T, name string) T {
	t.Helper()
	var value T
	data, err := os.ReadFile("../../storage/ceph/testdata/" + name + ".golden")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func canonicalOf(t *testing.T, value interface{ Canonical() ([]byte, error) }) json.RawMessage {
	t.Helper()
	data, err := value.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// stretchCluster is the frozen request of a stretch cluster over four
// installed hosts at two data sites and a provided MON-only tiebreaker.
func stretchCluster(t *testing.T) ceph.Request {
	t.Helper()
	r := cephGolden[ceph.Request](t, "request-cluster-stretch")
	r.Owner, r.FSID = "", ""
	for i := range r.Hosts {
		if !r.Hosts[i].Provided {
			r.Hosts[i].HostKeyRef = r.Hosts[i].Name + "-host-key"
		}
	}
	return r
}

func peerCluster(t *testing.T, stretch ceph.Request) ceph.Request {
	t.Helper()
	r := stretch
	r.Cluster, r.Stretch, r.Bootstrap, r.Delegated, r.Hosts = "secondary", nil, stretchPeers[0], nil, nil
	r.MonIP, r.PublicNetworks = "203.0.113.1", []string{"203.0.113.0/24"}
	r.Services = []json.RawMessage{
		json.RawMessage(`{"placement":{"hosts":["peer-node-1","peer-node-2","peer-node-3"]},"service_id":"data","service_type":"osd","spec":{"data_devices":{"paths":["/dev/vdb"]}}}`),
		json.RawMessage(`{"placement":{"hosts":["peer-node-1","peer-node-2","peer-node-3"]},"service_type":"mon"}`),
		json.RawMessage(`{"placement":{"count":2,"hosts":["peer-node-1","peer-node-2","peer-node-3"]},"service_type":"mgr"}`),
	}
	for i, host := range stretchPeers {
		r.Hosts = append(r.Hosts, ceph.Host{Name: host, Machine: host, Address: "203.0.113." + string(rune('1'+i)), User: r.ClusterSSHUser, Port: 22, SSHKeyRef: "fleet-key", HostKeyRef: host + "-host-key",
			Labels: []string{"mon", "storage"}, Location: map[string]string{}, Devices: []ceph.Device{{Path: "/dev/vdb", Serial: host, SizeBytes: 1 << 30}}})
	}
	return r
}

func machineBlock(host string) reconciliation.BlockDefinition {
	return reconciliation.BlockDefinition{ID: "machine-" + host, Description: "create the Machine " + host, Stage: reconciliation.StageMachines, Impacts: []string{"create-machine " + host},
		Consumes: []string{}, Kind: string(api.Machine), Object: host, Implementation: fleetMachineImplementation, ContentDigest: strings.Repeat("c", 64), Request: json.RawMessage(`{"name":"` + host + `"}`)}
}

func clusterBlocks(t *testing.T, r ceph.Request, hosts []string) []reconciliation.BlockDefinition {
	t.Helper()
	image := canonicalOf(t, ceph.ImageRequest{Version: ceph.ImageVersion, Cluster: r.Cluster, Controller: r.Controller, Release: r.Release, Base: "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9", Tag: "v9.9.1-25074",
		Image: r.Image, Registry: r.Registry, RegistryRef: r.RegistryRef})
	definitions := []reconciliation.BlockDefinition{{ID: ceph.ImageBlockID(r.Cluster), Description: "resolve the pinned Ceph image of " + r.Cluster, Stage: reconciliation.StageClusters,
		Impacts: []string{"resolve-image"}, Consumes: []string{}, Kind: string(api.CephCluster), Object: r.Cluster, Implementation: ceph.ImageImplementation, ContentDigest: ceph.ImageContentDigest(), Request: image}}
	dependencies := []string{ceph.ImageBlockID(r.Cluster)}
	for _, host := range hosts {
		definitions = append(definitions, machineBlock(host))
		dependencies = append(dependencies, "machine-"+host)
	}
	slices.Sort(dependencies)
	return append(definitions, reconciliation.BlockDefinition{ID: ceph.ClusterBlockID(r.Cluster), Description: "realize Ceph " + r.Cluster, Stage: reconciliation.StageClusters, Dependencies: dependencies,
		Exclusive: []string{"ceph/" + r.Cluster}, Impacts: []string{"realize Ceph " + r.Cluster}, Consumes: []string{}, Kind: string(api.CephCluster), Object: r.Cluster,
		Implementation: ceph.Implementation, ContentDigest: ceph.ContentDigest(), Request: canonicalOf(t, r)})
}

// keepingMachines removes every Machine but keep, which survives the destroy.
type keepingMachines struct {
	*fleetMachines
	keep string
}

func (m keepingMachines) RemovesHost(block reconciliation.Block) bool { return block.Object != m.keep }

// tiebreakerConsumerJourney applies a stretch cluster with a provided
// tiebreaker and, as replication says, its CephConsumer or a CephReplication
// to an installed peer cluster, and stops every installed host.
func tiebreakerConsumerJourney(t *testing.T, replication bool, keep string) (*lifecycle.CapabilityJourney, *tiebreakerFleetRunner, *fleetMachines, string) {
	t.Helper()
	stretch := stretchCluster(t)
	definitions := clusterBlocks(t, stretch, stretchInstalled)
	installed := slices.Clone(stretchInstalled)
	var dependent reconciliation.BlockDefinition
	if replication {
		peer := peerCluster(t, stretch)
		definitions = append(definitions, clusterBlocks(t, peer, stretchPeers)...)
		installed = append(installed, stretchPeers...)
		r := cephGolden[ceph.ReplicationRequest](t, "request-replication")
		r.Primary.Machines, r.Primary.Provided = []string{"node-1", "node-2", "node-3", "node-4", "tie"}, []string{"tie"}
		dependencies := []string{ceph.ClusterBlockID("example"), ceph.ClusterBlockID("secondary"), ceph.ImageBlockID("example"), ceph.ImageBlockID("secondary")}
		slices.Sort(dependencies)
		dependent = reconciliation.BlockDefinition{ID: ceph.ReplicationBlockID("replica"), Description: "replicate replica", Stage: reconciliation.StageClusters, Dependencies: dependencies,
			Exclusive: []string{"ceph/example", "ceph/secondary"}, Impacts: []string{"replicate replica"}, Consumes: []string{}, Kind: string(api.CephReplication), Object: "replica",
			Implementation: ceph.ReplicationImplementation, ContentDigest: ceph.ReplicationContentDigest(), Request: canonicalOf(t, r)}
	} else {
		r := cephGolden[ceph.ConsumerRequest](t, "request-export")
		r.Machines, r.Provided = []string{"node-1", "node-2", "node-3", "node-4", "tie"}, []string{"tie"}
		dependent = reconciliation.BlockDefinition{ID: ceph.ConsumerBlockID("consumer"), Description: "create per-consumer Ceph principals for consumer", Stage: reconciliation.StageClusters,
			Dependencies: []string{ceph.ClusterBlockID("example")}, Exclusive: []string{"ceph/example"}, Impacts: []string{"create per-consumer Ceph principals for consumer"}, Consumes: []string{},
			Kind: string(api.CephConsumer), Object: "consumer", Implementation: ceph.ConsumerImplementation, ContentDigest: ceph.ConsumerContentDigest(), Request: canonicalOf(t, r)}
	}
	definitions = append(definitions, dependent)
	secretNames := []string{"cluster-key", "fleet-key", "registry", "tie-known-hosts"}
	for _, host := range installed {
		secretNames = append(secretNames, host+"-host-key")
	}
	slices.Sort(secretNames)
	runner := &tiebreakerFleetRunner{removed: true}
	machines := &fleetMachines{}
	journey := lifecycle.NewBoundJourney(t, []lifecycle.BoundCapability{
		{Binding: lifecycle.CapabilityBinding{Kind: string(api.Machine), Implementation: fleetMachineImplementation}, Capability: keepingMachines{fleetMachines: machines, keep: keep}},
		{Binding: lifecycle.CapabilityBinding{Kind: string(api.CephCluster), Implementation: ceph.ImageImplementation}, Capability: ceph.NewImage(runner)},
		{Binding: lifecycle.CapabilityBinding{Kind: string(api.CephCluster), Implementation: ceph.Implementation}, Capability: appliedCluster{ceph.New(api.CephCluster, runner).WithProofs(fleetProofs{})}},
		{Binding: lifecycle.CapabilityBinding{Kind: string(api.CephConsumer), Implementation: ceph.ConsumerImplementation}, Capability: appliedConsumer{ceph.NewConsumer(runner)}},
		{Binding: lifecycle.CapabilityBinding{Kind: string(api.CephReplication), Implementation: ceph.ReplicationImplementation}, Capability: appliedReplication{ceph.NewReplication(runner)}},
	}, lifecycle.CapabilityPlan{Definitions: definitions, Secrets: secretNames})
	holdTiebreakerSecrets(t, journey, installed)
	result, err := journey.Service.Apply(context.Background(), lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the apply = %+v, %+v (%v)", result, diagnostics.Of(err), err)
	}
	if runs, unwanted := runner.took(); len(runs) != 0 || len(unwanted) != 0 {
		t.Fatalf("the apply ran %v and %v on the hosts", runs, unwanted)
	}
	holdTiebreakerSecrets(t, journey, installed)
	return journey, runner, machines, dependent.ID
}

func holdTiebreakerSecrets(t *testing.T, journey *lifecycle.CapabilityJourney, installed []string) {
	t.Helper()
	key, err := trust.ParseAuthorizedKey(string(fleetHostKey()))
	if err != nil {
		t.Fatal(err)
	}
	journey.Holding("fleet-key", secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: []byte("fleet-private")}))
	journey.Holding("cluster-key", secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: []byte("cluster-private"), secrets.PublicKeyPart: fleetHostKey()}))
	journey.Holding("registry", secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("registry-user"), secrets.PasswordPart: []byte("registry-password")}))
	journey.Holding("tie-known-hosts", secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(key.Line("198.51.100.10", 22))}))
	for _, host := range installed {
		journey.Holding(host+"-host-key", secrets.NewMaterial(map[secrets.Part][]byte{secrets.PublicKeyPart: fleetHostKey()}))
	}
}

func retirementOf(t *testing.T, journey *lifecycle.CapabilityJourney, block string) map[string]json.RawMessage {
	t.Helper()
	records := journey.Records(t, block)
	if len(records) == 0 || records[len(records)-1].Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("%s has no recorded retirement: %+v", block, records)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(records[len(records)-1].Evidence, &members); err != nil {
		t.Fatal(err)
	}
	return members
}

// A stretch cluster with a provided tiebreaker and a CephConsumer, or a
// CephReplication, retires with its installed hosts: no block refuses by the
// surviving-host rule, only the cluster block reaches the tiebreaker, and the
// cluster records its retirement only once the footprint there is absent.
func TestAStretchClusterWithAProvidedTiebreakerRetiresWithItsConsumerOrReplication(t *testing.T) {
	for name, replication := range map[string]bool{"consumer": false, "replication": true} {
		t.Run(name, func(t *testing.T) {
			journey, runner, machines, dependent := tiebreakerConsumerJourney(t, replication, "")
			destroy := lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, Authorizations: []string{reconciliation.AuthorizationDataLoss}, SkipConfirmation: true}
			runner.mutex.Lock()
			runner.removed = false
			runner.mutex.Unlock()
			if _, err := journey.Service.Destroy(context.Background(), destroy); err == nil {
				t.Fatal("the destroy completed while the tiebreaker's footprint remained")
			}
			runs, unwanted := runner.took()
			if !slices.Equal(runs, []string{ceph.ClusterBlockID("example") + ":destroy"}) || len(unwanted) != 0 || len(machines.removed) != 0 {
				t.Fatalf("the unproved removal ran %v and %v and removed %v, want only the tiebreaker removal and no Machine", runs, unwanted, machines.removed)
			}
			if records := journey.Records(t, ceph.ClusterBlockID("example")); slices.ContainsFunc(records, func(record operationstore.Attempt) bool { return record.Outcome == reconciliation.OutcomeChanged }) {
				t.Fatalf("the cluster recorded its retirement while the tiebreaker's footprint remained: %+v", records)
			}
			runner.mutex.Lock()
			runner.removed = true
			runner.mutex.Unlock()
			holdTiebreakerSecrets(t, journey, append(slices.Clone(stretchInstalled), stretchPeers...))
			result, err := journey.Service.Destroy(context.Background(), destroy)
			if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
				t.Fatalf("the destroy = %+v, %+v (%v)", result, diagnostics.Of(err), err)
			}
			runs, unwanted = runner.took()
			if len(unwanted) != 0 || len(runs) == 0 || slices.ContainsFunc(runs, func(run string) bool { return !strings.HasPrefix(run, ceph.ClusterBlockID("example")+":") }) {
				t.Fatalf("the retirement ran %v and %v, want only the cluster block's tiebreaker removal", runs, unwanted)
			}
			want := slices.Clone(stretchInstalled)
			if replication {
				want = append(want, stretchPeers...)
			}
			slices.Sort(want)
			if removed := slices.Sorted(slices.Values(machines.removed)); !slices.Equal(removed, want) {
				t.Fatalf("the destroy removed Machines %v, want every installed host", removed)
			}
			var retiredWith []string
			if err := json.Unmarshal(retirementOf(t, journey, dependent)["retiredWith"], &retiredWith); err != nil || !slices.Contains(retiredWith, "tie") {
				t.Fatalf("%s retired with %v (%v), want the cluster's every host, the tiebreaker included", dependent, retiredWith, err)
			}
			var tiebreaker ceph.RetiredTiebreaker
			if err := json.Unmarshal(retirementOf(t, journey, ceph.ClusterBlockID("example"))["tiebreaker"], &tiebreaker); err != nil || tiebreaker != (ceph.RetiredTiebreaker{Host: "tie", Machine: "tie", Removed: true}) {
				t.Fatalf("the cluster's retirement names tiebreaker %+v (%v)", tiebreaker, err)
			}
		})
	}
}

// A surviving Bootwright-installed host still refuses the destroy, naming the
// consumer or the replication; that their bindings keep every installed host
// is TestTheConsumerAndReplicationBindingsLeaveOutOnlyTheProvidedTiebreaker's.
func TestASurvivingInstalledHostStillRefusesTheConsumerAndReplication(t *testing.T) {
	for name, replication := range map[string]bool{"consumer": false, "replication": true} {
		t.Run(name, func(t *testing.T) {
			journey, runner, machines, _ := tiebreakerConsumerJourney(t, replication, "node-4")
			_, err := journey.Service.Destroy(context.Background(), lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, Authorizations: []string{reconciliation.AuthorizationDataLoss}, SkipConfirmation: true})
			reported := diagnostics.Of(err)
			dependent := map[bool]string{false: "CephConsumer/consumer (", true: "CephReplication/replica ("}[replication]
			if len(reported) == 0 || reported[0].Code != "lifecycle.live" || !strings.Contains(reported[0].Message, "B491") || !strings.Contains(reported[0].Message, dependent) {
				t.Fatalf("the destroy with node-4 surviving = %+v (%v), want lifecycle.live naming %s and B491", reported, err, dependent)
			}
			if runs, unwanted := runner.took(); len(runs) != 0 || len(unwanted) != 0 || len(machines.removed) != 0 {
				t.Fatalf("the refused destroy ran %v and %v and removed %v", runs, unwanted, machines.removed)
			}
		})
	}
}
