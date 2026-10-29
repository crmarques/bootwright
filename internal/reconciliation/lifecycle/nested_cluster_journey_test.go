package lifecycle_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/substrate/kubevirt"
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
				f("networkAttachments", api.ListValue(api.MapValue(s("name", "lab-guests"), f("kubevirt", api.MapValue(f("networkRef", api.MapValue(
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
	data, err := json.Marshal(kubevirt.LendEvidence{ExpirationSeconds: request.ExpirationSeconds, Postcondition: true, Request: run.Digest,
		Server: "https://api.host.example.test:6443", Version: "substrate-access-kubevirt-evidence-v1"})
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	return lifecycle.RunResult{Outcome: "unchanged", Evidence: data, Produced: []lifecycle.Produced{{Name: "kubeconfig",
		Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("users: [{user: {token: " + token + "}}]")})}}}, nil
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

func nestedHostRequirements(api.Catalog, string) ([]reconciliation.ObjectRef, bool) {
	return []reconciliation.ObjectRef{{Kind: "ClusterAddonBinding", Object: nestedHost + ".kubevirt"}, {Kind: "OpenShiftClusterNetwork", Object: nestedHost}}, true
}

func nestedClusterJourney(t *testing.T) (*lifecycle.CapabilityJourney, *kubevirtHost) {
	t.Helper()
	catalog := nestedClusterCatalog()
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: lifecycle.JourneyContext}}
	host := newKubevirtHost()
	host.nested = true
	stub := nestedHostBlocks{host: host}
	bound := []lifecycle.BoundCapability{
		{Binding: lifecycle.CapabilityBinding{Kind: kubevirt.HostKind, Implementation: kubevirt.HostImplementation}, Capability: kubevirt.NewHost(host, nestedHostRequirements)},
		{Binding: lifecycle.CapabilityBinding{Kind: kubevirt.MachineKind, Implementation: kubevirt.MachineImplementation}, Capability: kubevirt.NewMachine(host)},
		{Binding: lifecycle.CapabilityBinding{Kind: agentinstall.Kind, Implementation: agentinstall.MediaImplementation}, Capability: locatedMedia{agentinstall.NewMedia(host, material.AuthorizedKey)}},
		{Binding: lifecycle.CapabilityBinding{Kind: agentinstall.Kind, Implementation: agentinstall.InstallImplementation},
			Capability: locatedInstaller{agentinstall.NewInstall(host).WithIdentities(recordedPins{}).WithHostAccess(kubevirt.NewLender(host))}},
		{Binding: lifecycle.CapabilityBinding{Kind: "OpenShiftCluster", Implementation: nestedHostInstall}, Capability: stub},
		{Binding: lifecycle.CapabilityBinding{Kind: "ClusterAddonBinding", Implementation: nestedHostAddon}, Capability: stub},
		{Binding: lifecycle.CapabilityBinding{Kind: "OpenShiftClusterNetwork", Implementation: nestedHostNetwork}, Capability: stub},
	}
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{
		{ID: "cluster-install-" + nestedHost, Description: "install the host", Stage: reconciliation.StageClusters, Kind: "OpenShiftCluster", Object: nestedHost,
			Implementation: nestedHostInstall, ContentDigest: strings.Repeat("1", 64), Request: json.RawMessage(`{"cluster":"host"}`)},
		{ID: "addon-kubevirt-" + nestedHost, Description: "install kubevirt on the host", Stage: reconciliation.StageAddOns, Kind: "ClusterAddonBinding", Object: nestedHost + ".kubevirt",
			Requires:       []reconciliation.ObjectRef{{Kind: "OpenShiftCluster", Object: nestedHost}},
			Implementation: nestedHostAddon, ContentDigest: strings.Repeat("2", 64), Request: json.RawMessage(`{"package":"kubevirt"}`)},
		{ID: "cluster-network-" + nestedHost, Description: "configure the host's networks", Stage: reconciliation.StageAddOns, Kind: "OpenShiftClusterNetwork", Object: nestedHost,
			Requires:       []reconciliation.ObjectRef{{Kind: "OpenShiftCluster", Object: nestedHost}},
			Implementation: nestedHostNetwork, ContentDigest: strings.Repeat("3", 64), Request: json.RawMessage(`{"cluster":"host"}`)},
	}}
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
	return lifecycle.NewBoundJourney(t, bound, plan), host
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
// custody entry holds either; and the removal takes the child down before the
// host's blocks, in the inverse order.
func TestANestedClusterIsAppliedOnAHostThisContextBuildsAndDestroyedFirst(t *testing.T) {
	journey, host := nestedClusterJourney(t)
	host.failAt = "boot"
	apply := lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
	holdNestedSecrets(journey)
	if _, err := journey.Service.Apply(context.Background(), apply); err == nil {
		t.Fatal("an interrupted installation was recorded done")
	}
	interrupted := host.minted
	holdNestedSecrets(journey)
	if _, err := journey.Service.Apply(context.Background(), apply); err != nil {
		t.Fatalf("continuation: %v (calls %v)", diagnostics.Of(err), host.calls)
	}
	if host.failAt != "" || host.minted <= interrupted || len(host.lendRefused) != 0 {
		t.Fatalf("the continuation did not mint again, or a run was lent other access: minted %d then %d, refused %v", interrupted, host.minted, host.lendRefused)
	}
	calls := host.calls
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
	for _, name := range kubevirtNodes {
		host.mutex.Lock()
		host.running[name], host.claims[name] = false, false
		host.mutex.Unlock()
	}
	beforeRemoval := host.minted
	destroy := lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, Authorizations: []string{reconciliation.AuthorizationDataLoss}, SkipConfirmation: true}
	holdNestedSecrets(journey)
	if _, err := journey.Service.Destroy(context.Background(), destroy); err != nil {
		t.Fatalf("destroy: %v (calls %v)", diagnostics.Of(err), host.calls)
	}
	if host.minted <= beforeRemoval || len(host.lendRefused) != 0 || len(host.machines) != 0 {
		t.Fatalf("the removal minted nothing, or left Machines: minted %d, refused %v, machines %v", host.minted, host.lendRefused, host.machines)
	}
	removal := host.calls[slices.Index(host.calls, agentinstall.InstallImplementation+" destroy"):]
	for _, child := range []string{kubevirt.MachineImplementation + " destroy", kubevirt.HostImplementation + " destroy"} {
		for _, parent := range []string{nestedHostInstall + " destroy", nestedHostAddon + " destroy", nestedHostNetwork + " destroy"} {
			if positionOf(t, removal, child) > positionOf(t, removal, parent) {
				t.Fatalf("%s ran after %s: %v", child, parent, removal)
			}
		}
	}
}

// A host the lend cannot reach leaves the child's proof unknown, never
// absent, and no Machine is touched.
func TestAnUnreachableHostLeavesTheNestedProviderUnknown(t *testing.T) {
	journey, host := nestedClusterJourney(t)
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
