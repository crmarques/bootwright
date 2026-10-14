package lifecycle_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage/ceph"
	"github.com/crmarques/bootwright/internal/trust"
)

const fleetMachineImplementation = "machine-libvirt-v1"

var fleetHosts = []string{"rhel-01", "rhel-02", "rhel-03"}

type fleetRunner struct {
	mutex      sync.Mutex
	hostRuns   []string
	imageRuns  []string
	resolvedAt string
}

const fleetDashboardPassword = "Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jd5_"

func (r *fleetRunner) Run(_ context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if run.Implementation == ceph.ImageImplementation {
		r.imageRuns = append(r.imageRuns, run.Operation)
		request, err := ceph.DecodeImageRequest(run.Canonical)
		if err != nil {
			return lifecycle.RunResult{}, err
		}
		evidence := ceph.ImageEvidence{Absent: true, Postcondition: true, Request: run.Digest}
		if run.Operation != "destroy" {
			evidence = ceph.ImageEvidence{Base: request.Base, Digest: r.resolvedAt, Platform: ceph.PinnedPlatform, Postcondition: true, Release: request.Release, Request: run.Digest, Tag: request.Tag}
		}
		data, _ := json.Marshal(evidence)
		return lifecycle.RunResult{Outcome: "changed", Evidence: data}, nil
	}
	r.hostRuns = append(r.hostRuns, run.Block+":"+run.Operation)
	request, err := ceph.DecodeRequest(run.Canonical)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	evidence := ceph.Evidence{
		Action: request.Action, Exists: true, FSID: request.FSID, Health: "HEALTH_OK", Hosts: fleetHosts, MGRActive: fleetHosts[0], MGRStandbys: fleetHosts[1:2],
		Monitors: fleetHosts, OSDs: []ceph.OSDEvidence{}, Owned: true, Owner: request.Owner, PoolID: -1, Ready: true, Request: run.Digest, Version: ceph.EvidenceVersion,
		MonAddrs: []ceph.MonitorAddress{},
	}
	for index, host := range fleetHosts {
		evidence.OSDs = append(evidence.OSDs, ceph.OSDEvidence{Host: host, ID: index, Path: "/dev/vdb", UUID: "osd-" + host})
	}
	var produced []lifecycle.Produced
	switch request.Action {
	case "cluster":
		evidence.Dashboard = ceph.DashboardEvidence{URL: "https://" + fleetHosts[0] + ":8443/", CertificateSHA256: strings.Repeat("c", 64), Reachable: true}
		produced = []lifecycle.Produced{{Name: ceph.DashboardOutput, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(fleetDashboardPassword)})}}
	case "pool":
		evidence.PoolID = 1
	case "client":
		evidence.Client = "client." + request.Client.Name
		credential := `[{"caps":{"mgr":"profile rbd pool=volumes","mon":"profile rbd","osd":"profile rbd pool=volumes"},"entity":"client.consumer","key":"` + base64.StdEncoding.EncodeToString([]byte("consumer-key")) + `"}]`
		produced = []lifecycle.Produced{{Name: "client", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(credential)})}}
	}
	data, _ := json.Marshal(evidence)
	return lifecycle.RunResult{Outcome: "changed", Evidence: data, Produced: produced}, nil
}

func (r *fleetRunner) took() ([]string, []string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	hostRuns, imageRuns := r.hostRuns, r.imageRuns
	r.hostRuns, r.imageRuns = nil, nil
	return hostRuns, imageRuns
}

type fleetMachines struct {
	mutex   sync.Mutex
	running bool
	keeps   bool
	removed []string
}

func (*fleetMachines) Plan(context.Context, lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	return lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}, nil
}

func (*fleetMachines) Removal(_ context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	return lifecycle.Removal{Description: "remove Machine " + block.Object, Impacts: []string{"remove-machine " + block.Object}, Consumes: []string{}}, nil
}

func (*fleetMachines) Apply(_ context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"machine":"` + execution.Block.Object + `"}`)}, nil
}

func (*fleetMachines) Observe(_ context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"machine":"` + execution.Block.Object + `"}`)}, nil
}

func (m *fleetMachines) Quiescent(_ context.Context, probe lifecycle.Probe) (lifecycle.Quiescence, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.running {
		return lifecycle.Quiescence{State: lifecycle.Live, Reason: "its domain is running", Stop: "bootwright machine stop --context " + probe.Context + " --name " + probe.Block.Object}, nil
	}
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "its domain is shut off"}, nil
}

func (m *fleetMachines) Destroy(_ context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.removed = append(m.removed, execution.Block.Object)
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"absent":true}`)}, nil
}

func (*fleetMachines) ObserveRemoval(context.Context, lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"absent":true}`)}, nil
}

func (*fleetMachines) ProbesQuiescence() bool { return true }

func (m *fleetMachines) RemovesHost(reconciliation.Block) bool { return !m.keeps }

type fleetProofs struct{}

func (fleetProofs) DataDisk(block reconciliation.Block, _ []byte, path, serial string, _ int64) bool {
	return block.Kind == string(api.Machine) && path == "/dev/vdb" && serial == block.Object
}

func (fleetProofs) HostKey([]lifecycle.BlockEvidence, string, string) (trust.HostKey, error) {
	return trust.HostKey{}, errors.New("every fleet host delivers its own host key")
}

func (fleetProofs) PackageTree([]lifecycle.BlockEvidence, string) (ceph.PackageTree, error) {
	return ceph.PackageTree{}, errors.New("the fleet names no package source")
}

func fleetHostKey() []byte {
	blob := binary.BigEndian.AppendUint32(nil, uint32(len("ssh-ed25519")))
	blob = append(blob, []byte("ssh-ed25519")...)
	blob = binary.BigEndian.AppendUint32(blob, 32)
	blob = append(blob, bytes.Repeat([]byte{7}, 32)...)
	return []byte("ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob))
}

func fleetRequest(t *testing.T, change func(*ceph.Request)) json.RawMessage {
	t.Helper()
	r := ceph.Request{
		Version: ceph.Version, Action: "cluster", Cluster: "example", Controller: "controller", ClusterSSHTransport: "native", Bootstrap: fleetHosts[0],
		Image: ceph.PinnedRepository + ":" + ceph.PinnedTag, ClusterKeyRef: "cluster-key", RegistryRef: "registry", Registry: ceph.PinnedRegistry,
		Packages: &ceph.PackageRequest{LicenseAccepted: true, Sources: []ceph.PackageSource{}, Version: ceph.PackageVersion},
		Services: []json.RawMessage{
			json.RawMessage(`{"placement":{"hosts":["rhel-01","rhel-02","rhel-03"]},"service_id":"data","service_type":"osd","spec":{"data_devices":{"paths":["/dev/vdb"]}}}`),
			json.RawMessage(`{"placement":{"hosts":["rhel-01","rhel-02","rhel-03"]},"service_type":"mon"}`),
			json.RawMessage(`{"placement":{"count":2,"hosts":["rhel-01","rhel-02","rhel-03"]},"service_type":"mgr"}`),
		},
	}
	for index, host := range fleetHosts {
		r.Hosts = append(r.Hosts, ceph.Host{Name: host, Machine: host, Address: "192.0.2." + string(rune('1'+index)), User: "operator", Port: 22, SSHKeyRef: "fleet-key", HostKeyRef: host + "-host-key",
			Devices: []ceph.Device{{Path: "/dev/vdb", Serial: host, SizeBytes: 1 << 30}}})
	}
	if change != nil {
		r.Packages = nil
		change(&r)
	}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fleetJourney(t *testing.T, keeps bool) (*lifecycle.CapabilityJourney, *fleetRunner, *fleetMachines) {
	t.Helper()
	runner := &fleetRunner{resolvedAt: "sha256:" + strings.Repeat("ab", 32)}
	machines := &fleetMachines{running: true, keeps: keeps}
	journey := fleetEngine(t, machines, runner)
	result, err := journey.Service.Apply(context.Background(), lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the apply = %+v, %+v (%v)", result, diagnostics.Of(err), err)
	}
	hostRuns, imageRuns := runner.took()
	if !slices.Equal(hostRuns, []string{"ceph-cluster-example:apply", "ceph-pool-volumes:apply", "ceph-client-consumer:apply"}) || !slices.Equal(imageRuns, []string{"apply"}) {
		t.Fatalf("the apply ran host-contacting %v and controller-local image %v", hostRuns, imageRuns)
	}
	if kept := journey.Kept(); len(kept) != 2 || !strings.HasPrefix(kept[0], "ceph-client-consumer/client=") ||
		kept[1] != "ceph-cluster-example/dashboard="+fleetDashboardPassword {
		t.Fatalf("custody after the apply = %v, want the client credential and the dashboard password", kept)
	}
	return journey, runner, machines
}

// fleetEngine is the fleet's engine before any apply: its Machines, the real
// Ceph image and Ceph capabilities over runner, and the Secrets they bind.
func fleetEngine(t *testing.T, machines *fleetMachines, runner ceph.Runner) *lifecycle.CapabilityJourney {
	t.Helper()
	image, err := ceph.ImageRequest{Version: ceph.ImageVersion, Cluster: "example", Controller: "controller", Release: ceph.PinnedRelease, Base: ceph.PinnedRepository, Tag: ceph.PinnedTag,
		Image: ceph.PinnedRepository + ":" + ceph.PinnedTag, Registry: ceph.PinnedRegistry, RegistryRef: "registry"}.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	cephBlock := func(id string, kind api.Kind, object string, request json.RawMessage, dependencies ...string) reconciliation.BlockDefinition {
		slices.Sort(dependencies)
		return reconciliation.BlockDefinition{ID: id, Description: "realize Ceph " + object, Stage: reconciliation.StageClusters, Dependencies: dependencies, Exclusive: []string{"ceph/example"},
			Impacts: []string{"realize Ceph " + object}, Consumes: []string{}, Kind: string(kind), Object: object, Implementation: ceph.Implementation, ContentDigest: ceph.ContentDigest(), Request: request}
	}
	definitions := []reconciliation.BlockDefinition{}
	machineBlocks := []string{}
	for _, host := range fleetHosts {
		definitions = append(definitions, reconciliation.BlockDefinition{ID: "machine-" + host, Description: "create the Machine " + host, Stage: reconciliation.StageMachines,
			Impacts: []string{"create-machine " + host}, Consumes: []string{}, Kind: string(api.Machine), Object: host, Implementation: fleetMachineImplementation,
			ContentDigest: strings.Repeat("c", 64), Request: json.RawMessage(`{"name":"` + host + `"}`)})
		machineBlocks = append(machineBlocks, "machine-"+host)
	}
	definitions = append(definitions,
		reconciliation.BlockDefinition{ID: ceph.ImageBlockID("example"), Description: "resolve the pinned Ceph image of example", Stage: reconciliation.StageClusters, Impacts: []string{"resolve-image"},
			Consumes: []string{}, Kind: string(api.StorageCluster), Object: "example", Implementation: ceph.ImageImplementation, ContentDigest: ceph.ImageContentDigest(), Request: image},
		cephBlock("ceph-cluster-example", api.StorageCluster, "example", fleetRequest(t, nil), append([]string{ceph.ImageBlockID("example")}, machineBlocks...)...),
		cephBlock("ceph-pool-volumes", api.StoragePool, "volumes", fleetRequest(t, func(r *ceph.Request) {
			r.Action, r.Pool = "pool", &ceph.Pool{CrushRule: "replicated_rule", MinSize: 2, Name: "volumes", PGNum: 32, Size: 3}
		}), "ceph-cluster-example"),
		cephBlock("ceph-client-consumer", api.StorageClient, "consumer", fleetRequest(t, func(r *ceph.Request) {
			r.Action, r.Client = "client", &ceph.Client{Access: "read-write", Name: "consumer", Pool: "volumes"}
		}), ceph.ImageBlockID("example"), "ceph-pool-volumes"),
	)
	secretNames := []string{"cluster-key", "fleet-key", "registry"}
	for _, host := range fleetHosts {
		secretNames = append(secretNames, host+"-host-key")
	}
	slices.Sort(secretNames)
	journey := lifecycle.NewBoundJourney(t, []lifecycle.BoundCapability{
		{Binding: lifecycle.CapabilityBinding{Kind: string(api.Machine), Implementation: fleetMachineImplementation}, Capability: machines},
		{Binding: lifecycle.CapabilityBinding{Kind: string(api.StorageCluster), Implementation: ceph.ImageImplementation}, Capability: ceph.NewImage(runner)},
		{Binding: lifecycle.CapabilityBinding{Kind: string(api.StorageCluster), Implementation: ceph.Implementation}, Capability: ceph.New(api.StorageCluster, runner).WithProofs(fleetProofs{})},
		{Binding: lifecycle.CapabilityBinding{Kind: string(api.StoragePool), Implementation: ceph.Implementation}, Capability: ceph.New(api.StoragePool, runner).WithProofs(fleetProofs{})},
		{Binding: lifecycle.CapabilityBinding{Kind: string(api.StorageClient), Implementation: ceph.Implementation}, Capability: ceph.New(api.StorageClient, runner).WithProofs(fleetProofs{})},
	}, lifecycle.CapabilityPlan{Definitions: definitions, Secrets: secretNames})
	holdFleetSecrets(journey)
	return journey
}

// holdFleetSecrets gives the fleet's Secrets fresh material. An invocation
// clears what it was lent, so each one after the first needs it again, as
// the real binder reads it anew.
func holdFleetSecrets(journey *lifecycle.CapabilityJourney) {
	journey.Holding("fleet-key", secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: []byte("fleet-private")}))
	journey.Holding("cluster-key", secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: []byte("cluster-private"), secrets.PublicKeyPart: fleetHostKey()}))
	journey.Holding("registry", secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("registry-user"), secrets.PasswordPart: []byte("registry-password")}))
	for _, host := range fleetHosts {
		journey.Holding(host+"-host-key", secrets.NewMaterial(map[secrets.Part][]byte{secrets.PublicKeyPart: fleetHostKey()}))
	}
}

func TestACephClusterRetiresWithEveryHostItRunsOnWithoutContactingThem(t *testing.T) {
	journey, runner, machines := fleetJourney(t, false)
	destroy := lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, Authorizations: []string{reconciliation.AuthorizationDataLoss}, SkipConfirmation: true}
	preview, err := journey.Service.Plan(context.Background(), lifecycle.PlanRequest{ContextName: lifecycle.JourneyContext})
	if err != nil || !slices.Equal(preview.Stops, fleetHosts) || len(preview.StopsAfterOnline) != 0 {
		t.Fatalf("the destroy preview stops %v first and %v after online removal (%v), want every host first", preview.Stops, preview.StopsAfterOnline, err)
	}
	_, err = journey.Service.Destroy(context.Background(), destroy)
	reported := diagnostics.Of(err)
	if len(reported) == 0 || reported[0].Code != "lifecycle.live" {
		t.Fatalf("the destroy while the hosts run = %+v (%v), want lifecycle.live", reported, err)
	}
	for _, host := range fleetHosts {
		if !strings.Contains(reported[0].Message, "Machine/"+host) || !strings.Contains(reported[0].Remediation, "bootwright machine stop --context "+lifecycle.JourneyContext+" --name "+host) {
			t.Fatalf("the refusal = %+v, want %s named with its machine stop", reported[0], host)
		}
	}
	if strings.Contains(reported[0].Message, "B491") || len(machines.removed) != 0 {
		t.Fatalf("the refusal = %+v and removed %v, want only the running hosts and nothing removed", reported[0], machines.removed)
	}
	if hostRuns, imageRuns := runner.took(); len(hostRuns) != 0 || len(imageRuns) != 0 {
		t.Fatalf("the refused destroy ran host-contacting %v and controller-local image %v", hostRuns, imageRuns)
	}
	machines.mutex.Lock()
	machines.running = false
	machines.mutex.Unlock()
	result, err := journey.Service.Destroy(context.Background(), destroy)
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the destroy once the hosts stopped = %+v, %+v (%v)", result, diagnostics.Of(err), err)
	}
	hostRuns, imageRuns := runner.took()
	if len(hostRuns) != 0 {
		t.Fatalf("the retirement made host-contacting Ceph runs %v, want none", hostRuns)
	}
	if !slices.Equal(imageRuns, []string{"destroy"}) {
		t.Fatalf("the controller-local image release ran %v, want its one destroy", imageRuns)
	}
	if removed := slices.Sorted(slices.Values(machines.removed)); !slices.Equal(removed, fleetHosts) {
		t.Fatalf("the destroy removed Machines %v, want every host", removed)
	}
	if kept := journey.Kept(); len(kept) != 0 || slices.ContainsFunc(kept, func(entry string) bool { return strings.HasPrefix(entry, "ceph-cluster-example/dashboard=") }) {
		t.Fatalf("custody after the retirement holds %v, want no Ceph produced entry and no dashboard password", kept)
	}
}

func TestACephClusterWhoseHostsSurviveNamesItsOwnRemedy(t *testing.T) {
	journey, runner, machines := fleetJourney(t, true)
	destroy := lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, Authorizations: []string{reconciliation.AuthorizationDataLoss}, SkipConfirmation: true}
	for _, running := range []bool{true, false} {
		machines.mutex.Lock()
		machines.running = running
		machines.mutex.Unlock()
		_, err := journey.Service.Destroy(context.Background(), destroy)
		reported := diagnostics.Of(err)
		if len(reported) == 0 || reported[0].Code != "lifecycle.live" || !strings.Contains(reported[0].Message, "B491") {
			t.Fatalf("the destroy with surviving hosts (running %t) = %+v (%v), want lifecycle.live naming B491", running, reported, err)
		}
		remedy := reported[0].Remediation
		if !strings.Contains(remedy, "same destroy as every Bootwright-owned virtual host Machine") || strings.Count(remedy, "B491") != 1 || strings.Contains(remedy, "stop what is running") || strings.Contains(remedy, "machine stop") {
			t.Fatalf("the refusal's remedy (running %t) = %q, want the Ceph capability's own remedy once", running, remedy)
		}
		if hostRuns, imageRuns := runner.took(); len(hostRuns) != 0 || len(imageRuns) != 0 || len(machines.removed) != 0 {
			t.Fatalf("the refused destroy ran host-contacting %v and controller-local image %v and removed %v", hostRuns, imageRuns, machines.removed)
		}
	}
}
