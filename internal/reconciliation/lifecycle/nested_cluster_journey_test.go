package lifecycle_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/kubevirt"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

const (
	nestedHost        = "host"
	nestedHostInstall = "stub-host-install-v1"
	nestedHostAddon   = "stub-host-addon-v1"
	nestedHostNetwork = "stub-host-network-v1"
	nestedTokenCanary = "minted-token-canary-"
)

// nestedClusterCatalog is lab-kubevirt-nested's child: the three KubeVirt
// Machines of kubevirtClusterCatalog on a provider a cluster of this context
// hosts, attached to that host's secondary network.
func nestedClusterCatalog() api.Catalog {
	f, s := kubevirtJourneyField, kubevirtJourneyText
	var objects []api.Object
	for _, object := range kubevirtClusterCatalog().Objects() {
		if object.Kind() == api.InfraProvider {
			object = api.NewObject(api.InfraProvider, object.Name(), api.Value{}, api.MapValue(
				f("kubevirt", object.Spec().Get("kubevirt").Without("kubeconfigRef").With("hostClusterRef", api.StringValue(nestedHost))),
				f("segments", api.ListValue(api.MapValue(s("name", "lab-guests"), f("kubevirt", api.MapValue(f("networkRef", api.MapValue(
					s("apiGroup", "k8s.ovn.org"), s("kind", "ClusterUserDefinedNetwork"), s("name", "guest-l2"))))))))))
		}
		objects = append(objects, object)
	}
	return api.NewCatalog(objects)
}

// nestedHostBlocks stands for what makes the host a KubeVirt host: its
// installation, which proves the administrator kubeconfig, its kubevirt
// instance and its cluster network block. Each records when it ran on the
// shared host.
type nestedHostBlocks struct{ host *kubevirtHost }

func (nestedHostBlocks) Plan(context.Context, lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	return lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}, nil
}

func (nestedHostBlocks) Claimed(api.Catalog) []string { return nil }

func (nestedHostBlocks) Removal(_ context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	return lifecycle.Removal{Description: "retire " + block.Object}, nil
}

func (c nestedHostBlocks) record(block reconciliation.Block, verb string) {
	c.host.mutex.Lock()
	defer c.host.mutex.Unlock()
	c.host.calls = append(c.host.calls, block.Implementation+" "+verb)
}

func (c nestedHostBlocks) produced(block reconciliation.Block) []lifecycle.Produced {
	if block.Implementation != nestedHostInstall {
		return nil
	}
	return adminKubeconfig()
}

func (c nestedHostBlocks) Apply(_ context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	c.record(execution.Block, "apply")
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"done":true}`), Produced: c.produced(execution.Block)}, nil
}

func (c nestedHostBlocks) Observe(_ context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Outcome: reconciliation.OutcomeUnchanged, Evidence: json.RawMessage(`{"done":true}`), Produced: c.produced(execution.Block)}, nil
}

func (nestedHostBlocks) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent}, nil
}

func (c nestedHostBlocks) Destroy(_ context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	c.record(execution.Block, "destroy")
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"absent":true}`)}, nil
}

func (nestedHostBlocks) ObserveRemoval(context.Context, lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"absent":true}`)}, nil
}

func (nestedHostBlocks) ProducedOutputs(block reconciliation.Block) ([]string, error) {
	if block.Implementation != nestedHostInstall {
		return nil, nil
	}
	return []string{agentinstall.KubeconfigOutput}, nil
}

// lend is the host's TokenRequest: it is handed the host installation's
// administrator kubeconfig, which no other run is, and answers one kubeconfig
// carrying a token minted for this run alone.
func (h *kubevirtHost) lend(run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	var request kubevirt.LendRequest
	if err := json.Unmarshal(run.Canonical, &request); err != nil {
		return lifecycle.RunResult{}, err
	}
	if h.unreachable {
		return lifecycle.RunResult{}, errors.New("the host cluster did not answer the token request")
	}
	if !lentHolds(run, adminAccess) || request.Cluster != nestedHost || request.ServiceAccount != "bootwright-kubevirt" || request.ExpirationSeconds < 600 {
		return lifecycle.RunResult{}, fmt.Errorf("the lend was not handed the host's administrator kubeconfig for its account: %+v", request)
	}
	h.minted++
	token := fmt.Sprintf("%s%d", nestedTokenCanary, h.minted)
	h.tokens = append(h.tokens, token)
	if h.bound == nil {
		h.bound = map[string]bool{}
	}
	h.bound[request.Secret] = true
	data, err := json.Marshal(kubevirt.LendEvidence{ExpirationSeconds: request.ExpirationSeconds, Postcondition: true, Request: run.Digest,
		Secret: request.Secret, Server: "https://api.host.example.test:6443", Version: "substrate-access-kubevirt-evidence-v1"})
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	return lifecycle.RunResult{Outcome: "unchanged", Evidence: data, Produced: []lifecycle.Produced{{Name: "kubeconfig",
		Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("users: [{user: {token: " + token + "}}]")})}}}, nil
}

// revoke deletes the per-lend Secret a lend bound its token to, through the
// host's administrator kubeconfig alone.
func (h *kubevirtHost) revoke(run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	var request kubevirt.LendRequest
	if err := json.Unmarshal(run.Canonical, &request); err != nil {
		return lifecycle.RunResult{}, err
	}
	if !lentHolds(run, adminAccess) || len(run.Outputs) != 0 {
		return lifecycle.RunResult{}, fmt.Errorf("the revoke was not handed the host's administrator kubeconfig alone: %+v", request)
	}
	delete(h.bound, request.Secret)
	h.revoked++
	data, err := json.Marshal(kubevirt.LendEvidence{Postcondition: true, Request: run.Digest, Secret: request.Secret,
		Server: "https://api.host.example.test:6443", Version: "substrate-access-kubevirt-evidence-v1"})
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	return lifecycle.RunResult{Outcome: "unchanged", Evidence: data}, nil
}

func lentHolds(run lifecycle.RunRequest, text string) bool {
	for _, held := range run.Material {
		value, ok := held.Part(secrets.ValuePart)
		found := ok && strings.Contains(string(value), text)
		clear(value)
		if found {
			return true
		}
	}
	return false
}

// minted reports whether a run on the host was lent the last token minted,
// and never the administrator kubeconfig.
func (h *kubevirtHost) mintedFor(run lifecycle.RunRequest) error {
	if lentHolds(run, adminAccess) {
		return errors.New(run.Implementation + " " + run.Operation + " was lent the host's administrator kubeconfig")
	}
	if len(h.tokens) == 0 || !lentHolds(run, h.tokens[len(h.tokens)-1]) {
		return errors.New(run.Implementation + " " + run.Operation + " was lent no freshly minted token")
	}
	return nil
}

// hostDomains is the hypervisor the host cluster's libvirt Machines run on.
// A running domain refuses its removal, as the adapter's revalidation does,
// and the host's API answers only while some domain of it runs. Bare-metal
// host Machines are the operator's: their removal releases the claim alone
// and leaves them, and the host, running.
type hostDomains struct {
	mutex   sync.Mutex
	host    *kubevirtHost
	running map[string]bool
	defined map[string]bool
	calls   []string
}

var nestedHostMachines = []string{"host-01", "host-02", "host-03"}

func nestedHostRequest(machine string, index int) libvirt.MachineRequest {
	request := lostMachineRequest()
	suffix := fmt.Sprintf("%02d", index+1)
	uuid := "5c1d3a52-0b3e-4f6a-9d2c-7e8f9a0b1c" + suffix
	request.Identity = libvirt.Identity{Block: "machine-" + machine, Context: lifecycle.JourneyContext, Object: machine}
	request.Controller.Endpoint = "http://192.0.2.1:80" + suffix + "/redfish/v1/Systems/" + uuid
	request.Controller.Port, request.Controller.Unit = 8000+index+1, "bootwright-bmc-lab-"+machine
	request.Directory = "/var/lib/bootwright/machines/lab/" + machine
	request.Disks = []libvirt.Disk{{Name: "root", Path: request.Directory + "/root.qcow2", SizeGiB: 60, Target: "vda"}}
	request.Domain, request.UUID = "bootwright-lab-"+machine, uuid
	return request
}

func nestedPhysicalRequest(machine string, index int) baremetal.Request {
	placement := lostMachineRequest().Placement
	return baremetal.Request{
		Controller: baremetal.Controller{CredentialsRef: "lab-bmc-credentials", Endpoint: fmt.Sprintf("https://192.0.2.%d/redfish/v1/Systems/1", 31+index), TLSVerify: true},
		Hardware:   []baremetal.Interface{{MACAddress: fmt.Sprintf("52:54:00:00:31:%02x", index+1), Name: "eno1"}},
		Identity:   baremetal.Identity{Block: "machine-" + machine, Context: lifecycle.JourneyContext, Object: machine},
		Placement:  placement, Provider: "lab-metal", Version: "machine-baremetal-v2",
	}
}

func (h *hostDomains) physical(run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	request, err := baremetal.DecodeRequest(run.Canonical)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.calls = append(h.calls, request.Identity.Object+" "+run.Operation)
	h.host.mutex.Lock()
	h.host.calls = append(h.host.calls, baremetal.Implementation+" "+run.Operation)
	h.host.mutex.Unlock()
	if run.Operation == "destroy" {
		data, err := json.Marshal(baremetal.Evidence{Absent: true, Addresses: []string{}, Postcondition: true, Request: run.Digest})
		return lifecycle.RunResult{Outcome: "changed", Evidence: data}, err
	}
	data, err := json.Marshal(baremetal.Evidence{Addresses: request.Addresses(), Postcondition: true, Power: "On", Request: run.Digest,
		UUID: "4c4c4544-0000-4000-8000-00000000" + strings.ReplaceAll(request.Hardware[0].MACAddress[12:], ":", "")})
	return lifecycle.RunResult{Outcome: "unchanged", Evidence: data}, err
}

func (h *hostDomains) Run(_ context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	if run.Implementation == baremetal.Implementation {
		return h.physical(run)
	}
	request, err := libvirt.DecodeMachineRequest(run.Canonical)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	h.mutex.Lock()
	defer h.mutex.Unlock()
	name := request.Identity.Object
	h.calls = append(h.calls, name+" "+run.Operation)
	h.host.mutex.Lock()
	h.host.calls = append(h.host.calls, libvirt.MachineImplementation+" "+run.Operation)
	h.host.mutex.Unlock()
	switch run.Operation {
	case "apply":
		h.defined[name], h.running[name] = true, true
	case "destroy":
		if h.running[name] {
			return lifecycle.RunResult{}, errors.New("the domain of " + name + " is running")
		}
		delete(h.defined, name)
	}
	outcome := "unchanged"
	if run.Operation != "observe" {
		outcome = "changed"
	}
	if !h.defined[name] {
		return lifecycle.RunResult{Outcome: outcome, Evidence: evidenceOf(libvirt.MachineEvidence{Absent: true, Answered: true,
			Disks: []libvirt.DiskEvidence{{Name: "root"}}, Listener: held(false), Postcondition: true, Request: run.Digest})}, nil
	}
	state, power := "running", "On"
	if !h.running[name] {
		state, power = "shut off", "Off"
	}
	return lifecycle.RunResult{Outcome: outcome, Evidence: evidenceOf(libvirt.MachineEvidence{
		Answered: true, Controller: request.Controller.Image, Disks: []libvirt.DiskEvidence{{Name: "root", Present: true, SizeGiB: 60}},
		Domain: request.Domain, Listener: held(true), Owned: true, Postcondition: true, Power: power, Request: run.Digest,
		State: state, System: request.UUID, Unit: "active",
	})}, nil
}

// stop shuts every host domain off, as bootwright machine stop leaves it, so
// the host's API no longer answers.
func (h *hostDomains) stop() {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	clear(h.running)
	h.host.mutex.Lock()
	h.host.unreachable = true
	h.host.mutex.Unlock()
}

func (h *hostDomains) left() []string {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	var names []string
	for name := range h.defined {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func nestedHostRequirements(api.Catalog, string) ([]reconciliation.ObjectRef, bool) {
	return []reconciliation.ObjectRef{{Kind: "ClusterAddonBinding", Object: nestedHost + ".kubevirt"}, {Kind: "OpenShiftClusterNetwork", Object: nestedHost}}, true
}

func nestedClusterJourney(t *testing.T, physical bool) (*lifecycle.CapabilityJourney, *kubevirtHost, *hostDomains) {
	t.Helper()
	catalog := nestedClusterCatalog()
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: lifecycle.JourneyContext}}
	host := newKubevirtHost()
	host.nested = true
	stub := nestedHostBlocks{host: host}
	domains := &hostDomains{host: host, running: map[string]bool{}, defined: map[string]bool{}}
	bound := []lifecycle.BoundCapability{
		{Binding: lifecycle.CapabilityBinding{Kind: kubevirt.HostKind, Implementation: kubevirt.HostImplementation}, Capability: kubevirt.NewHost(host, nestedHostRequirements)},
		{Binding: lifecycle.CapabilityBinding{Kind: kubevirt.MachineKind, Implementation: kubevirt.MachineImplementation}, Capability: kubevirt.NewMachine(host)},
		{Binding: lifecycle.CapabilityBinding{Kind: agentinstall.Kind, Implementation: agentinstall.MediaImplementation}, Capability: locatedMedia{agentinstall.NewMedia(host, material.AuthorizedKey)}},
		{Binding: lifecycle.CapabilityBinding{Kind: agentinstall.Kind, Implementation: agentinstall.InstallImplementation},
			Capability: locatedInstaller{agentinstall.NewInstall(host).WithIdentities(recordedPins{}).WithHostAccess(kubevirt.NewLender(host))}},
		{Binding: lifecycle.CapabilityBinding{Kind: "OpenShiftCluster", Implementation: nestedHostInstall}, Capability: stub},
		{Binding: lifecycle.CapabilityBinding{Kind: "ClusterAddonBinding", Implementation: nestedHostAddon}, Capability: stub},
		{Binding: lifecycle.CapabilityBinding{Kind: "OpenShiftClusterNetwork", Implementation: nestedHostNetwork}, Capability: stub},
		{Binding: lifecycle.CapabilityBinding{Kind: libvirt.MachineKind, Implementation: libvirt.MachineImplementation}, Capability: libvirt.NewMachine(domains)},
		{Binding: lifecycle.CapabilityBinding{Kind: baremetal.Kind, Implementation: baremetal.Implementation}, Capability: baremetal.NewMachine(domains)},
	}
	var hostNodes []reconciliation.ObjectRef
	plan := lifecycle.CapabilityPlan{}
	for index, machine := range nestedHostMachines {
		var canonical []byte
		var err error
		implementation, keys, references := libvirt.MachineImplementation, []string(nil), []string(nil)
		if physical {
			request := nestedPhysicalRequest(machine, index)
			canonical, err = request.Canonical()
			implementation, keys, references = baremetal.Implementation, request.ReservationKeys(), request.SecretReferences()
		} else {
			request := nestedHostRequest(machine, index)
			canonical, err = request.Canonical()
			keys, references = request.ReservationKeys(), request.SecretReferences()
		}
		if err != nil {
			t.Fatal(err)
		}
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{
			ID: "machine-" + machine, Description: "realize the host machine " + machine, Stage: reconciliation.StageMachines,
			Kind: libvirt.MachineKind, Object: machine, Implementation: implementation,
			ContentDigest: strings.Repeat("d", 64), Request: canonical,
			Groups: []reconciliation.Group{{ID: "realize-machine", Description: "realize the machine", Machines: []string{machine}}},
		})
		plan.Reservations = append(plan.Reservations, prerequisites.HostReservation{Context: lifecycle.JourneyContext, Kind: "substrate-machine", Service: machine, Keys: keys})
		plan.Secrets = append(plan.Secrets, references...)
		hostNodes = append(hostNodes, reconciliation.ObjectRef{Kind: libvirt.MachineKind, Object: machine})
	}
	plan.Definitions = append(plan.Definitions, []reconciliation.BlockDefinition{
		{ID: "cluster-install-" + nestedHost, Description: "install the host", Stage: reconciliation.StageClusters, Kind: "OpenShiftCluster", Object: nestedHost,
			Requires: hostNodes, Implementation: nestedHostInstall, ContentDigest: strings.Repeat("1", 64), Request: json.RawMessage(`{"cluster":"host"}`)},
		{ID: "addon-kubevirt-" + nestedHost, Description: "install kubevirt on the host", Stage: reconciliation.StageAddOns, Kind: "ClusterAddonBinding", Object: nestedHost + ".kubevirt",
			Requires:       []reconciliation.ObjectRef{{Kind: "OpenShiftCluster", Object: nestedHost}},
			Implementation: nestedHostAddon, ContentDigest: strings.Repeat("2", 64), Request: json.RawMessage(`{"package":"kubevirt"}`)},
		{ID: "cluster-network-" + nestedHost, Description: "configure the host's networks", Stage: reconciliation.StageAddOns, Kind: "OpenShiftClusterNetwork", Object: nestedHost,
			Requires:       []reconciliation.ObjectRef{{Kind: "OpenShiftCluster", Object: nestedHost}},
			Implementation: nestedHostNetwork, ContentDigest: strings.Repeat("3", 64), Request: json.RawMessage(`{"cluster":"host"}`)},
	}...)
	for _, capability := range bound[:4] {
		planned, err := capability.Capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatalf("planning %s: %v", capability.Binding.Implementation, diagnostics.Of(err))
		}
		for _, definition := range planned.Definitions {
			definition.Family = ""
			plan.Definitions = append(plan.Definitions, definition)
		}
		plan.Secrets = append(plan.Secrets, planned.Secrets...)
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return lifecycle.NewBoundJourney(t, bound, plan), host, domains
}

func holdNestedSecrets(journey *lifecycle.CapabilityJourney) {
	journey.Holding("openshift-pull-secret", secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(`{"auths":{}}`)}))
	journey.Holding("kubevirt-cluster-admin-ssh-key", secrets.NewMaterial(map[secrets.Part][]byte{
		secrets.PrivateKeyPart: []byte("cluster-private"), secrets.PublicKeyPart: append(fleetHostKey(), []byte(" cluster")...)}))
}

func positionOf(t *testing.T, calls []string, call string) int {
	t.Helper()
	position := slices.Index(calls, call)
	if position < 0 {
		t.Fatalf("%s never ran: %v", call, calls)
	}
	return position
}

// The nested journey: the child's provider waits for the host's installation,
// its kubevirt instance and its network block; every run on the host is lent
// a token minted for it from the host's custodied kubeconfig, never that
// kubeconfig, and a continuation and the removal mint again; no record and no
// custody entry holds either. The removal of a host this context builds on
// libvirt removes the stopped child while the host still answers, the hosted
// VirtualMachines being online effects, releases the proof without reaching
// the host, then pauses for the host's Machines, which stop after online
// cleanup; once the operator stops them, the same removal completes.
func TestANestedClusterIsAppliedOnAHostThisContextBuildsAndDestroyedFirst(t *testing.T) {
	journey, host, domains := nestedClusterJourney(t, false)
	host.failAt = "boot"
	apply := lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
	holdNestedSecrets(journey)
	if _, err := journey.Service.Apply(context.Background(), apply); err == nil {
		t.Fatal("an interrupted installation was recorded done")
	}
	interrupted := host.minted
	holdNestedSecrets(journey)
	continued, err := journey.Service.Apply(context.Background(), apply)
	if err != nil {
		t.Fatalf("continuation: %v (calls %v)", diagnostics.Of(err), host.calls)
	}
	if slices.ContainsFunc(continued.Warnings, func(warning diagnostics.Diagnostic) bool { return warning.Code == kubevirt.LendNotRevoked }) || host.revoked != host.minted {
		t.Fatalf("a revoke was not proved: %d of %d minted, warnings %+v", host.revoked, host.minted, continued.Warnings)
	}
	if host.failAt != "" || host.minted <= interrupted || len(host.lendRefused) != 0 {
		t.Fatalf("the continuation did not mint again, or a run was lent other access: minted %d then %d, refused %v", interrupted, host.minted, host.lendRefused)
	}
	if len(host.bound) != 0 {
		t.Fatalf("a lent token outlived its run, interrupted or completed: %v", host.bound)
	}
	calls := host.calls
	if positionOf(t, calls, libvirt.MachineImplementation+" apply") > positionOf(t, calls, nestedHostInstall+" apply") {
		t.Fatalf("the host installed before its Machines: %v", calls)
	}
	proof := positionOf(t, calls, kubevirt.HostImplementation+" apply")
	for _, prior := range []string{nestedHostInstall + " apply", nestedHostAddon + " apply", nestedHostNetwork + " apply"} {
		if positionOf(t, calls, prior) > proof {
			t.Fatalf("%s did not precede the provider's proof: %v", prior, calls)
		}
	}
	if positionOf(t, calls, kubevirt.HostImplementation+" lend") > proof || positionOf(t, calls, kubevirt.MachineImplementation+" apply") < proof {
		t.Fatalf("the proof was not lent its access first, or a Machine preceded it: %v", calls)
	}
	for _, kept := range journey.Kept() {
		if strings.Contains(kept, nestedTokenCanary) {
			t.Fatalf("custody kept a minted token: %s", kept)
		}
	}
	for _, block := range []string{kubevirt.HostBlockID("lab-kubevirt"), kubevirt.MachineBlockID(kubevirtNodes[0]), agentinstall.InstallBlockID("kubevirt")} {
		for _, record := range journey.Records(t, block) {
			if data, _ := json.Marshal(record); strings.Contains(string(data), nestedTokenCanary) || strings.Contains(string(data), "admin-canary") {
				t.Fatalf("block %s recorded lent material: %s", block, data)
			}
		}
	}
	destroy := lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, Authorizations: []string{reconciliation.AuthorizationDataLoss}, SkipConfirmation: true}
	holdNestedSecrets(journey)
	_, err = journey.Service.Destroy(context.Background(), destroy)
	if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Code != "lifecycle.live" || !strings.Contains(reported[0].Message, "Machine/"+kubevirtNodes[0]) ||
		strings.Contains(reported[0].Message, "Machine/host-01") || slices.ContainsFunc(host.calls, func(call string) bool { return strings.HasSuffix(call, " destroy") }) {
		t.Fatalf("a removal registered while the child runs: %+v (calls %v)", reported, host.calls)
	}
	for _, name := range kubevirtNodes {
		host.mutex.Lock()
		host.running[name], host.claims[name] = false, false
		host.mutex.Unlock()
	}
	beforeRemoval := host.minted
	holdNestedSecrets(journey)
	_, err = journey.Service.Destroy(context.Background(), destroy)
	if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Code != "lifecycle.live" || !strings.Contains(reported[0].Message, "Machine/host-01") {
		t.Fatalf("the removal did not pause for the running host: %+v (calls %v)", reported, host.calls)
	}
	if host.minted <= beforeRemoval || len(host.lendRefused) != 0 || len(host.machines) != 0 || len(host.bound) != 0 {
		t.Fatalf("the removal minted nothing, left the child's Machines or a lent token: minted %d, refused %v, machines %v, bound %v", host.minted, host.lendRefused, host.machines, host.bound)
	}
	if left := domains.left(); !slices.Equal(left, nestedHostMachines) {
		t.Fatalf("a running host Machine was removed: %v left", left)
	}
	removal := host.calls[slices.Index(host.calls, agentinstall.InstallImplementation+" destroy"):]
	if released := positionOf(t, removal, kubevirt.HostImplementation+" destroy"); removal[released-1] == kubevirt.HostImplementation+" lend" {
		t.Fatalf("the proof's removal minted a token: %v", removal)
	}
	for _, child := range []string{kubevirt.MachineImplementation + " destroy", kubevirt.HostImplementation + " destroy"} {
		for _, parent := range []string{nestedHostInstall + " destroy", nestedHostAddon + " destroy", nestedHostNetwork + " destroy"} {
			if positionOf(t, removal, child) > positionOf(t, removal, parent) {
				t.Fatalf("%s ran after %s: %v", child, parent, removal)
			}
		}
	}
	domains.stop()
	paused, refused := host.minted, len(host.lendRefused)
	holdNestedSecrets(journey)
	if _, err := journey.Service.Destroy(context.Background(), destroy); err != nil {
		t.Fatalf("destroy after the host stopped: %v (calls %v)", diagnostics.Of(err), host.calls)
	}
	if left := domains.left(); len(left) != 0 || host.minted != paused || len(host.lendRefused) != refused {
		t.Fatalf("the continuation left %v, or reached the stopped host: minted %d then %d", left, paused, host.minted)
	}
	if positionOf(t, host.calls, libvirt.MachineImplementation+" destroy") < positionOf(t, host.calls, nestedHostInstall+" destroy") {
		t.Fatalf("a host Machine left before the host's installation: %v", host.calls)
	}
}

// A host the lend cannot reach leaves the child's proof unknown, never
// absent, and no Machine is touched.
func TestAnUnreachableHostLeavesTheNestedProviderUnknown(t *testing.T) {
	journey, host, _ := nestedClusterJourney(t, false)
	host.unreachable = true
	holdNestedSecrets(journey)
	if _, err := journey.Service.Apply(context.Background(), lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err == nil {
		t.Fatal("an unreachable host was proved")
	}
	if slices.Contains(host.calls, kubevirt.MachineImplementation+" apply") || slices.Contains(host.calls, kubevirt.HostImplementation+" apply") {
		t.Fatalf("a run reached the host without its access: %v", host.calls)
	}
	records := journey.Records(t, kubevirt.HostBlockID("lab-kubevirt"))
	if len(records) == 0 || records[len(records)-1].Outcome == reconciliation.OutcomeChanged || records[len(records)-1].Outcome == reconciliation.OutcomeUnchanged {
		t.Fatalf("the unreachable host's proof was recorded %+v", records)
	}
}

// A bare-metal host is the operator's and never stops for a removal: its
// Machines probe no quiescence, so none waits for online cleanup, and the
// removal of the stopped child, the proof and the host's claims completes in
// one invocation while the host keeps answering.
func TestANestedClusterOnABareMetalHostIsDestroyedInOneRemoval(t *testing.T) {
	journey, host, domains := nestedClusterJourney(t, true)
	holdNestedSecrets(journey)
	if _, err := journey.Service.Apply(context.Background(), lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err != nil {
		t.Fatalf("apply: %v (calls %v)", diagnostics.Of(err), host.calls)
	}
	for _, name := range kubevirtNodes {
		host.mutex.Lock()
		host.running[name], host.claims[name] = false, false
		host.mutex.Unlock()
	}
	holdNestedSecrets(journey)
	if _, err := journey.Service.Destroy(context.Background(), lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext,
		Authorizations: []string{reconciliation.AuthorizationDataLoss}, SkipConfirmation: true}); err != nil {
		t.Fatalf("destroy: %v (calls %v)", diagnostics.Of(err), host.calls)
	}
	if len(host.machines) != 0 || len(host.lendRefused) != 0 || host.unreachable {
		t.Fatalf("the removal left %v, or was refused access %v", host.machines, host.lendRefused)
	}
	for _, machine := range nestedHostMachines {
		if !slices.Contains(domains.calls, machine+" destroy") {
			t.Fatalf("the claim of %s was not released: %v", machine, domains.calls)
		}
	}
	if positionOf(t, host.calls, baremetal.Implementation+" destroy") < positionOf(t, host.calls, kubevirt.MachineImplementation+" destroy") {
		t.Fatalf("a host Machine left before the child: %v", host.calls)
	}
}
