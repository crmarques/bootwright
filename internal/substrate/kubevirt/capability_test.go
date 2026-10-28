package kubevirt

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

func TestNamesStayWithin63BytesAndDeriveFromContextAndMachine(t *testing.T) {
	long := strings.Repeat("a", 47) + "-" + strings.Repeat("b", 15)
	for _, machine := range []string{"master-0", long, strings.Repeat("c", 63)} {
		vm := substrate.KubeVirtName(testContext, machine)
		if len(vm+"-media") > 63 || len(vm+"-root") > 63 || strings.HasSuffix(strings.TrimSuffix(vm, vm[len(vm)-9:]), "-") {
			t.Fatalf("%q derives %q, past 63 bytes or ending in a dash before its digits", machine, vm)
		}
		if vm != substrate.KubeVirtName(testContext, machine) || vm == substrate.KubeVirtName("other", machine) {
			t.Fatalf("%q: the name is not deterministic per context and Machine", machine)
		}
	}
	if got := substrate.KubeVirtName(testContext, long); !strings.HasPrefix(got, strings.Repeat("a", 47)+"-") || len(got) != 56 {
		t.Fatalf("a 48-byte cut ending in a dash is trimmed: %q", got)
	}
	// The collection's derivation (kubevirt_machine.name) is held to this value.
	if got := substrate.KubeVirtName("lab", "master-0"); got != "master-0-"+kubevirtNameDigits {
		t.Fatalf("KubeVirtName(lab, master-0) = %q", got)
	}
}

const kubevirtNameDigits = "538954ed"

func TestTheMachineRequestPinsDerivedMACsAndResolvesEachNetwork(t *testing.T) {
	request := machineRequest(t)
	vm := substrate.KubeVirtName(testContext, "master-0")
	if request.Name != vm || request.RootVolume != vm+"-root" || request.MediaVolume != vm+"-media" || request.Namespace != "guests" ||
		request.StorageClass != "guest-block" || request.CPU != 8 || request.MemoryMiB != 16384 || request.DiskGiB != 120 || request.TPM != nil {
		t.Fatalf("request = %+v", request)
	}
	want := []Interface{
		{MACAddress: substrate.InterfaceMAC(testContext, "master-0", "enp1s0"), Name: "enp1s0", Network: "guest-l2"},
		{MACAddress: substrate.InterfaceMAC(testContext, "master-0", "enp2s0"), Name: "enp2s0", Network: "storage-net"},
	}
	if !slices.Equal(request.Interfaces, want) || !strings.HasPrefix(want[0].MACAddress, "52:54:00:") {
		t.Fatalf("interfaces = %+v, want %+v", request.Interfaces, want)
	}
	persistent, err := MachineRequests(labCatalog(provider(field("tpm", api.MapValue()))), "controller", testContext)
	if err != nil || persistent[0].TPM == nil || !persistent[0].TPM.Persistent {
		t.Fatalf("a declared tpm renders persistent by default: %+v, %v", persistent, err)
	}
	transient, err := MachineRequests(labCatalog(provider(field("tpm", api.MapValue(field("persistent", api.BoolValue(false)))))), "controller", testContext)
	if err != nil || transient[0].TPM == nil || transient[0].TPM.Persistent {
		t.Fatalf("an authored persistent: false is frozen: %+v, %v", transient, err)
	}
	host := hostRequest(t)
	if host.KubeconfigRef != "host-kubeconfig" || len(host.Access) != len(verbs) || !slices.Equal(host.MACAddresses, slices.Sorted(slices.Values([]string{want[0].MACAddress, want[1].MACAddress}))) ||
		!slices.Equal(host.Networks, []Network{{Attachment: "lab-guests", Name: "guest-l2", Namespace: "guests"}, {Attachment: "storage", Derived: "storage-net", Name: "storage-net", Namespace: "guests"}}) {
		t.Fatalf("host request = %+v", host)
	}
}

func TestTheTargetNamesTheVirtualMachineAndItsKubeconfig(t *testing.T) {
	catalog := labCatalog()
	machine, _ := catalog.Find(api.Machine, "master-0")
	target, err := substrate.TargetFor(catalog, machine, testContext, "controller")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if target.Port != substrate.ArmKubeVirt || target.Delivery != substrate.DeliveryPush || target.Physical ||
		target.Controller.Endpoint != "kubevirt:guests/"+substrate.KubeVirtName(testContext, "master-0") ||
		target.Controller.CredentialsRef != "host-kubeconfig" || target.Identity.Channel != substrate.ChannelNone ||
		target.PlacementMachine.Name() != "controller" || len(target.Interfaces) != 2 {
		t.Fatalf("target = %+v", target)
	}
	if user, password := substrate.ControllerCredential(substrate.ArmKubeVirt); user != secrets.ValuePart || password != "" {
		t.Fatalf("the kubeconfig is lent as %q and %q", user, password)
	}
}

func TestPlansAProofBlockAndAMachineBlockThatRequiresIt(t *testing.T) {
	host, err := NewHost(nil).Plan(context.Background(), planInput(labCatalog(), reconciliation.Apply))
	if err != nil || len(host.Definitions) != 1 || host.Definitions[0].Stage != reconciliation.StageSubstrates || !slices.Equal(host.Secrets, []string{"host-kubeconfig"}) {
		t.Fatalf("host plan = %+v, %v", host, err)
	}
	for verb, consumes := range map[reconciliation.Verb][]string{reconciliation.Apply: nil, reconciliation.Destroy: {reconciliation.AuthorizationDataLoss}} {
		plan, err := NewMachine(nil).Plan(context.Background(), planInput(labCatalog(), verb))
		if err != nil || len(plan.Definitions) != 1 {
			t.Fatalf("machine plan = %+v, %v", plan, err)
		}
		definition := plan.Definitions[0]
		if definition.Stage != reconciliation.StageMachines || !slices.Equal(definition.Requires, []reconciliation.ObjectRef{{Kind: HostKind, Object: "lab-kubevirt"}}) ||
			!slices.Equal(definition.Consumes, consumes) || len(plan.Reservations) != 0 || !slices.Equal(plan.Secrets, []string{"host-kubeconfig"}) {
			t.Fatalf("%s definition = %+v", verb, definition)
		}
	}
}

func TestBothBlocksReadTheKubeconfigAsAnAccessCredential(t *testing.T) {
	for _, block := range []reconciliation.Block{hostBlock(t), machineBlock(t)} {
		var capability lifecycle.AccessCredentialReader = NewHost(nil)
		if block.Implementation == MachineImplementation {
			capability = NewMachine(nil)
		}
		access, err := capability.AccessCredentials(block)
		if err != nil || !slices.Equal(access, []string{"host-kubeconfig"}) {
			t.Fatalf("%s reads %q as access credentials, %v", block.Implementation, access, err)
		}
	}
}

func hostBlock(t *testing.T) reconciliation.Block {
	t.Helper()
	canonical, err := hostRequest(t).Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: HostBlockID("lab-kubevirt"), Kind: HostKind, Object: "lab-kubevirt",
		Implementation: HostImplementation, Request: canonical}, RequestDigest: strings.Repeat("a", 64)}
}

func machineBlock(t *testing.T) reconciliation.Block {
	t.Helper()
	canonical, err := machineRequest(t).Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: MachineBlockID("master-0"), Kind: MachineKind, Object: "master-0",
		Implementation: MachineImplementation, Request: canonical}, RequestDigest: strings.Repeat("b", 64)}
}

func provedHostEvidence(t *testing.T, block reconciliation.Block, uid string) lifecycle.BlockEvidence {
	t.Helper()
	request := hostRequest(t)
	networks := []NetworkEvidence{}
	for _, network := range request.Networks {
		networks = append(networks, NetworkEvidence{Attachment: network.Attachment, Present: true})
	}
	evidence := encoded(t, HostEvidence{Failures: []string{}, KubeSystemUID: uid, MACPool: "outside", Networks: networks, Postcondition: true,
		ReadWriteMany: true, Request: block.RequestDigest, Server: "https://api.host.example.test:6443", StorageClass: "guest-block",
		UploadProxy: "https://cdi-uploadproxy.apps.host.example.test", Version: "v1.34.2"})
	return lifecycle.BlockEvidence{Block: block, Kind: HostKind, Object: "lab-kubevirt", Implementation: HostImplementation,
		Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: evidence}
}

func machineFixture(t *testing.T, digest string, change func(*MachineEvidence)) []byte {
	t.Helper()
	request := machineRequest(t)
	evidence := MachineEvidence{
		Answered: true, KubeSystemUID: "cluster-1", Owned: true, Postcondition: true,
		Receipts: []Receipt{{Basis: "created", Kind: "VirtualMachine", Name: request.Name, UID: "vm-1"}},
		Request:  digest, Server: "https://api.host.example.test:6443", VirtualMachine: request.Name,
		Volumes: []VolumeEvidence{{Kind: "DataVolume", Name: request.RootVolume, Present: true}, {Kind: "DataVolume", Name: request.MediaVolume}},
	}
	if change != nil {
		change(&evidence)
	}
	return encoded(t, evidence)
}

func TestTheMachineReadsEachEvidenceFormForItsVerb(t *testing.T) {
	request, digest := machineRequest(t), strings.Repeat("b", 64)
	absent := func(e *MachineEvidence) {
		e.Absent, e.Owned, e.VirtualMachine, e.Volumes = true, false, "", []VolumeEvidence{{Kind: "DataVolume", Name: request.RootVolume}, {Kind: "DataVolume", Name: request.MediaVolume}}
	}
	for name, test := range map[string]struct {
		change         func(*MachineEvidence)
		apply, destroy reconciliation.EffectState
	}{
		"present":            {nil, reconciliation.EffectCompleted, reconciliation.EffectNoEffect},
		"absent":             {absent, reconciliation.EffectNoEffect, reconciliation.EffectCompleted},
		"root still binding": {func(e *MachineEvidence) { e.Postcondition, e.Volumes[0].Present = false, false }, reconciliation.EffectPartial, reconciliation.EffectPartial},
		"claim left behind": {func(e *MachineEvidence) {
			absent(e)
			e.Absent, e.Postcondition = false, false
			e.Volumes = append(e.Volumes, VolumeEvidence{Kind: "PersistentVolumeClaim", Name: request.RootVolume, Present: true})
		}, reconciliation.EffectPartial, reconciliation.EffectPartial},
		"foreign same name": {func(e *MachineEvidence) { e.Owned, e.Foreign, e.Postcondition = false, true, false }, reconciliation.EffectUnknown, reconciliation.EffectUnknown},
		"host unanswered": {func(e *MachineEvidence) {
			absent(e)
			e.Answered = false
		}, reconciliation.EffectUnknown, reconciliation.EffectUnknown},
		"running instance": {func(e *MachineEvidence) {
			absent(e)
			e.Instance = "Running"
		}, reconciliation.EffectUnknown, reconciliation.EffectUnknown},
		"no receipt": {func(e *MachineEvidence) { e.Receipts = nil }, reconciliation.EffectUnknown, reconciliation.EffectNoEffect},
	} {
		evidence := machineFixture(t, digest, test.change)
		if got, _ := machineEffect(reconciliation.Apply, evidence, request, digest); got != test.apply {
			t.Errorf("%s: apply reads %s, want %s", name, got, test.apply)
		}
		if got, _ := machineEffect(reconciliation.Destroy, evidence, request, digest); got != test.destroy {
			t.Errorf("%s: destroy reads %s, want %s", name, got, test.destroy)
		}
	}
	if ValidateMachinePresence(machineFixture(t, strings.Repeat("c", 64), nil), request, digest) == nil {
		t.Fatal("evidence for another request was accepted")
	}
}

func TestTheMachineRunPinsTheProvedHostAndCarriesItsReceipts(t *testing.T) {
	block := machineBlock(t)
	proved := provedHostEvidence(t, hostBlock(t), "cluster-1")
	run := &runner{result: lifecycle.RunResult{Outcome: "changed", Evidence: machineFixture(t, block.RequestDigest, nil)}}
	execution := lifecycle.Execution{Block: block, ApplyOperation: "op-1", ApplyRegistered: "2026-10-28T10:00:00Z", Context: testContext,
		ApplyProved: []lifecycle.BlockEvidence{proved}, PreviousEvidence: machineFixture(t, block.RequestDigest, nil)}
	result, err := NewMachine(run).Apply(context.Background(), execution)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("apply = %+v, %v", result, diagnostics.Of(err))
	}
	sent := run.requests[0]
	var pinned proof
	if err := json.Unmarshal([]byte(sent.MaterialValues["pinned"]), &pinned); err != nil || pinned.KubeSystemUID != "cluster-1" ||
		pinned.StorageClass != "guest-block" || len(pinned.Receipts) != 1 || pinned.Receipts[0].UID != "vm-1" {
		t.Fatalf("pinned = %+v, %v", pinned, err)
	}
	if sent.MaterialValues["operation"] != "op-1" || sent.MaterialValues["block"] != block.ID || sent.Deadline != machineDeadline || sent.Deadline > lifecycle.MaxDeadline {
		t.Fatalf("values = %v, deadline %s", sent.MaterialValues, sent.Deadline)
	}
	if !slices.ContainsFunc(sent.Materials, func(file lifecycle.MaterialFile) bool {
		return file.Secret == "host-kubeconfig" && file.Part == secrets.ValuePart && file.Variable == "kubeconfig"
	}) {
		t.Fatalf("materials = %+v", sent.Materials)
	}
	moved := execution
	moved.ApplyProved = []lifecycle.BlockEvidence{provedHostEvidence(t, hostBlock(t), "cluster-2")}
	if _, err := NewMachine(&runner{}).Apply(context.Background(), moved); err == nil || !strings.Contains(diagnostics.Of(err)[0].Message, "not the one") {
		t.Fatalf("a kubeconfig reaching another cluster was not refused: %v", err)
	}
	unproved := execution
	unproved.ApplyProved = nil
	if _, err := NewMachine(&runner{}).Apply(context.Background(), unproved); err == nil {
		t.Fatal("a machine block without its provider's proof ran")
	}
}

func TestQuiescenceIsTheInstanceAbsenceAHostAnswered(t *testing.T) {
	block := machineBlock(t)
	for name, test := range map[string]struct {
		change func(*MachineEvidence)
		state  string
	}{
		"absent instance":  {nil, lifecycle.Quiescent},
		"running instance": {func(e *MachineEvidence) { e.Instance = "Running" }, lifecycle.Live},
		"host unanswered":  {func(e *MachineEvidence) { e.Answered = false }, lifecycle.Unproved},
	} {
		run := &runner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: machineFixture(t, block.RequestDigest, test.change)}}
		probe := lifecycle.Probe{ApplyOperation: "op-1", Context: testContext, Block: block,
			ApplyProved: []lifecycle.BlockEvidence{provedHostEvidence(t, hostBlock(t), "cluster-1")}}
		got, err := NewMachine(run).Quiescent(context.Background(), probe)
		if err != nil || got.State != test.state || got.State != lifecycle.Quiescent && !strings.Contains(got.Stop, "machine stop") {
			t.Errorf("%s: %+v, %v", name, got, err)
		}
	}
}

func TestTheClaimIsTheServerNamespaceAndName(t *testing.T) {
	block := machineBlock(t)
	reservation, claimed, err := NewMachine(nil).ProvedClaim(testContext, block, machineFixture(t, block.RequestDigest, nil))
	request := machineRequest(t)
	if err != nil || !claimed || !slices.Equal(reservation.Keys, []string{"kubevirt-vm:api.host.example.test:6443/guests/" + request.Name}) {
		t.Fatalf("claim = %+v, %v, %v", reservation, claimed, err)
	}
	for server, want := range map[string]string{"https://192.0.2.10": "kubevirt-vm:192.0.2.10:443/ns/vm", "https://[2001:db8::1]": "kubevirt-vm:[2001:db8::1]:443/ns/vm"} {
		if got, ok := ClaimKey(server, "ns", "vm"); !ok || got != want {
			t.Errorf("%s claims %q", server, got)
		}
	}
	for _, server := range []string{"http://api.example.test", "https://user@api.example.test", "https://api.example.test/prefix"} {
		if _, ok := ClaimKey(server, "ns", "vm"); ok {
			t.Errorf("%s names a claim", server)
		}
	}
	if _, claimed, _ := NewMachine(nil).ProvedClaim(testContext, block, machineFixture(t, block.RequestDigest, func(e *MachineEvidence) { e.Owned, e.Foreign = false, true })); claimed {
		t.Fatal("a foreign VirtualMachine was claimed")
	}
}

func TestTheProofRefusesWhatItCouldNotProve(t *testing.T) {
	block := hostBlock(t)
	request := hostRequest(t)
	held := provedHostEvidence(t, block, "cluster-1").Evidence
	if err := ValidateHostPresence(held, request, block.RequestDigest); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	var evidence HostEvidence
	if err := json.Unmarshal(held, &evidence); err != nil {
		t.Fatal(err)
	}
	failing := evidence
	failing.Failures = []string{"the storage class guest-block offers no ReadWriteMany claim"}
	if err := ValidateHostPresence(encoded(t, failing), request, block.RequestDigest); err == nil || !strings.Contains(diagnostics.Of(err)[0].Message, "ReadWriteMany") {
		t.Fatalf("a failed check passed: %v", err)
	}
	missing := evidence
	missing.Networks = missing.Networks[:1]
	if ValidateHostPresence(encoded(t, missing), request, block.RequestDigest) == nil {
		t.Fatal("a proof missing a network passed")
	}
	run := &runner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encoded(t, HostEvidence{Absent: true, Failures: []string{}, Networks: []NetworkEvidence{}, Postcondition: true, Request: block.RequestDigest})}}
	if result, err := NewHost(run).Destroy(context.Background(), lifecycle.Execution{Block: block}); err != nil || result.Outcome != reconciliation.OutcomeUnchanged ||
		len(run.requests[0].Materials) != 1 {
		t.Fatalf("destroy = %+v, %v", result, err)
	}
}

func TestUnsupportedRefusesWhatTheArmDoesNotRealize(t *testing.T) {
	installed := node("master-0")
	installed = installed.WithSpec(installed.Spec().WithPath(api.StringValue("rhel"), "os", "installProfileRef"))
	udn := provider()
	attachments := udn.Spec().Get("networkAttachments").Items()
	attachments[1] = attachments[1].WithPath(api.MapValue(text("apiGroup", "k8s.ovn.org"), text("kind", "UserDefinedNetwork"), text("name", "tenant")), "kubevirt", "networkRef")
	udn = udn.WithSpec(udn.Spec().With("networkAttachments", api.ListValue(attachments...)))
	for name, catalog := range map[string]api.Catalog{
		"install profile": api.NewCatalog([]api.Object{controller(), networkConfig(), installed, provider()}),
		"zero size":       labCatalog(provider(number("diskGiB", "0"))),
		"UDN":             labCatalog(udn),
	} {
		refused := NewHost(nil).Unsupported(compilation.NewState(catalog, catalog, nil))
		if len(refused) != 1 || refused[0].Name != "master-0" {
			t.Errorf("%s: %+v", name, refused)
		}
	}
	if refused := NewHost(nil).Unsupported(compilation.NewState(labCatalog(), labCatalog(), nil)); len(refused) != 0 {
		t.Fatalf("the lab is refused: %+v", refused)
	}
}

func TestEveryCapabilityDeadlineCoversItsFrozenBudgets(t *testing.T) {
	if hostDeadline > lifecycle.MaxDeadline || machineDeadline > lifecycle.MaxDeadline {
		t.Fatal("a deadline passes the run ceiling")
	}
	if machineDeadline < substrate.KubeVirtPowerWait+2*substrate.KubeVirtPowerWait || machineDeadline < 1100*time.Second+time.Minute {
		t.Fatalf("the machine deadline %s does not cover the role's 1100 s waits", machineDeadline)
	}
	run := &runner{result: lifecycle.RunResult{Outcome: "unchanged"}}
	_, _ = NewHost(run).Apply(context.Background(), lifecycle.Execution{Block: hostBlock(t)})
	if len(run.requests) != 1 || run.requests[0].Deadline != hostDeadline {
		t.Fatalf("host runs = %+v", run.requests)
	}
}
