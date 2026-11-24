package hardware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
)

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func text(name, value string) api.FieldValue { return field(name, api.StringValue(value)) }

func sshHost(name, address string, extra ...api.FieldValue) api.Object {
	fields := append([]api.FieldValue{
		field("os", api.MapValue(field("provided", api.MapValue()))),
		field("network", api.MapValue(field("addresses", api.ListValue(api.MapValue(text("name", "ip"), text("address", address)))))),
		field("access", api.MapValue(field("ssh", api.MapValue(text("addressRef", "ip"), text("knownHostsRef", name+"-known-hosts"),
			field("auth", api.MapValue(text("privateKeyRef", name+"-key"))))))),
	}, extra...)
	return api.NewObject(api.Machine, name, api.Value{}, api.MapValue(fields...))
}

func proxyOn(machine string) api.Object {
	return api.NewObject(api.Proxy, "proxy-"+machine, api.Value{}, api.MapValue(text("management", "managed"), text("machineRef", machine)))
}

func catalogOf(objects ...api.Object) api.Catalog {
	return api.NewCatalog(append([]api.Object{
		api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(field("controller", api.MapValue(text("machineRef", "controller"))))),
		api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue(field("access", api.MapValue(field("local", api.BoolValue(true)))))),
	}, objects...))
}

func planned(t *testing.T, catalog api.Catalog, verb reconciliation.Verb) lifecycle.CapabilityPlan {
	t.Helper()
	plan, err := New(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: verb, Context: lifecycle.ContextIdentity{Name: "lab"},
		State: compilation.NewState(catalog, catalog, nil), Controller: "controller"})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return plan
}

func servicesBlock(t *testing.T) reconciliation.Block {
	t.Helper()
	definition := planned(t, catalogOf(sshHost("services", "192.0.2.9"), proxyOn("services")), reconciliation.Apply).Definitions[0]
	digest, err := reconciliation.RequestDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.Block{BlockDefinition: definition, RequestDigest: digest}
}

func evidenceOf(t *testing.T, digest string, addresses ...string) []byte {
	t.Helper()
	if addresses == nil {
		addresses = []string{}
	}
	data, err := json.Marshal(Evidence{Addresses: addresses, Postcondition: true, Request: digest, Version: EvidenceVersion})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Every OS-ready Machine a placement reaches over SSH gets one block, which
// reads the permanent hardware addresses of its host in the infrastructure
// services stage, plans its claim with no key for a fresh apply to keep what
// it proved, and binds the placement's SSH Secrets; a placement on the
// controller, a Machine Bootwright installs and a Machine nothing is placed
// on get none (D257 (2)).
func TestAnOSReadyMachineReachedOverSSHIsClaimedByItsHardware(t *testing.T) {
	installed := api.NewObject(api.Machine, "installed", api.Value{}, api.MapValue(
		field("os", api.MapValue(field("install", api.MapValue(text("profileRef", "rhel")))))))
	catalog := catalogOf(sshHost("services", "192.0.2.9"), sshHost("idle", "192.0.2.10"), installed,
		proxyOn("services"), proxyOn("controller"), proxyOn("installed"),
		api.NewObject(api.InfraProvider, "lab-libvirt", api.Value{}, api.MapValue(field("libvirt", api.MapValue(text("machineRef", "services"))))))
	plan := planned(t, catalog, reconciliation.Apply)
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %+v", plan.Definitions)
	}
	definition := plan.Definitions[0]
	if definition.ID != "machine-hardware-services" || definition.Kind != "Machine" || definition.Object != "services" || definition.Family != machine.HardwareFamily ||
		definition.Implementation != Implementation || definition.Stage != reconciliation.StageInfrastructureServices || len(definition.Requires) != 0 ||
		!slices.Equal(definition.Impacts, []string{"claim-machine 192.0.2.9:22"}) || definition.ContentDigest != ContentDigest() {
		t.Fatalf("definition = %+v", definition)
	}
	if want := []infrastructureservices.HostReservation{{Context: "lab", Kind: ClaimKind, Service: "services"}}; !slices.EqualFunc(plan.Reservations, want, func(x, y infrastructureservices.HostReservation) bool {
		return x.Context == y.Context && x.Kind == y.Kind && x.Service == y.Service && len(x.Keys) == 0
	}) || len(plan.SSHReservations) != 0 {
		t.Fatalf("reservations = %+v, SSH %+v", plan.Reservations, plan.SSHReservations)
	}
	if !slices.Equal(plan.Secrets, []string{"services-key", "services-known-hosts"}) {
		t.Fatalf("secrets = %v", plan.Secrets)
	}
	request, err := DecodeRequest(definition.Request)
	if err != nil || request.Placement.Connection != machine.ConnectionSSH || request.Placement.Address != "192.0.2.9" || request.Placement.Port != 22 {
		t.Fatalf("request = %+v (%v)", request, err)
	}
	removal := planned(t, catalog, reconciliation.Destroy).Definitions[0]
	if len(removal.Impacts) != 0 || !strings.HasPrefix(removal.Description, "release the claim") {
		t.Fatalf("removal = %+v", removal)
	}
}

// The claim is one exclusive, unqualified bmc-mac: key per permanent hardware
// address the host reported, in canonical form; a host reporting more than one
// reservation holds refuses rather than claim the server by a part of its
// addresses; and evidence that proves no complete read claims nothing.
func TestTheProvedClaimIsOneBMCMACKeyPerPermanentAddress(t *testing.T) {
	block := servicesBlock(t)
	claim, found, err := New(nil).ProvedClaim("lab", block, evidenceOf(t, block.RequestDigest, "52:54:00:9a:1b:01", "52:54:00:9a:1b:02"))
	if err != nil || !found || claim.Context != "lab" || claim.Kind != ClaimKind || claim.Service != "services" || claim.Shared ||
		!slices.Equal(claim.Keys, []string{"bmc-mac:52:54:00:9a:1b:01", "bmc-mac:52:54:00:9a:1b:02"}) {
		t.Fatalf("claim = %+v, %v (%v)", claim, found, err)
	}
	many := make([]string, 0, infrastructureservices.MaxReservationKeys+1)
	for index := range infrastructureservices.MaxReservationKeys + 1 {
		many = append(many, fmt.Sprintf("52:54:00:00:%02x:%02x", index/256, index%256))
	}
	if _, found, err := New(nil).ProvedClaim("lab", block, evidenceOf(t, block.RequestDigest, many[:infrastructureservices.MaxReservationKeys]...)); err != nil || !found {
		t.Fatalf("the largest address set was refused: %v", err)
	}
	_, _, err = New(nil).ProvedClaim("lab", block, evidenceOf(t, block.RequestDigest, many...))
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != hardwareAddressesTooMany.Code || !strings.Contains(reported[0].Message, "65 permanent hardware addresses") {
		t.Fatalf("an address set beyond the bound = %+v", reported)
	}
	for name, data := range map[string][]byte{
		"unsorted":     evidenceOf(t, block.RequestDigest, "52:54:00:9a:1b:02", "52:54:00:9a:1b:01"),
		"not lower":    evidenceOf(t, block.RequestDigest, "52:54:00:9A:1B:01"),
		"not a MAC":    evidenceOf(t, block.RequestDigest, "52-54-00-9a-1b-01"),
		"released":     []byte(`{"absent":true,"addresses":[],"postcondition":true,"request":"` + block.RequestDigest + `","version":"` + EvidenceVersion + `"}`),
		"unproved":     []byte(`{"absent":false,"addresses":["52:54:00:9a:1b:01"],"postcondition":false,"request":"` + block.RequestDigest + `","version":"` + EvidenceVersion + `"}`),
		"another kind": []byte(`{"absent":false,"addresses":[],"postcondition":true,"request":"` + block.RequestDigest + `","version":"other-v1"}`),
	} {
		if _, found, err := New(nil).ProvedClaim("lab", block, data); err == nil || found {
			t.Errorf("%s evidence claimed %v", name, found)
		}
	}
}

func physicalClaim(t *testing.T, contextName, endpoint string, macs ...string) infrastructureservices.HostReservation {
	t.Helper()
	request := baremetal.Request{
		Controller: baremetal.Controller{CredentialsRef: "metal-bmc", Endpoint: endpoint, TLSVerify: true},
		Identity:   baremetal.Identity{Block: baremetal.BlockID("server"), Context: contextName, Object: "server"},
		Placement:  machine.Placement{Connection: machine.ConnectionLocal, Machine: "controller"},
		Version:    "machine-baremetal-v1",
	}
	for index, mac := range macs {
		request.Hardware = append(request.Hardware, baremetal.Interface{MACAddress: mac, Name: fmt.Sprintf("eno%d", index+1)})
	}
	frozen, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := json.Marshal(baremetal.Evidence{Addresses: []string{"52:54:00:9a:1b:01", "52:54:00:9a:1b:02"}, Postcondition: true, Power: "Off", Request: "digest", Serial: "SN1"})
	if err != nil {
		t.Fatal(err)
	}
	claim, found, err := baremetal.NewMachine(nil).ProvedClaim(contextName, reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: frozen}}, evidence)
	if err != nil || !found {
		t.Fatalf("physical claim = %+v, %v (%v)", claim, found, diagnostics.Of(err))
	}
	return claim
}

// A context that uses a server as an OS-ready Machine over SSH and a context
// that drives the same server by its BMC, declaring its MACs in another
// spelling, conflict whichever claims it first: each claim's registration
// meets the other's bmc-mac: keys and refuses controller.conflict naming the
// holder and the Machine, never the address (audit 004 REC-74, D257 (2)).
func TestAnSSHReachedServerAndItsBMCDriverConflictInEitherOrder(t *testing.T) {
	block := servicesBlock(t)
	reached, found, err := New(nil).ProvedClaim("ssh", block, evidenceOf(t, block.RequestDigest, "52:54:00:9a:1b:01", "52:54:00:9a:1b:02"))
	if err != nil || !found {
		t.Fatalf("claim = %v (%v)", found, err)
	}
	driven := physicalClaim(t, "bmc", "https://192.0.2.40/redfish/v1/Systems/1", "52-54-00-9A-1B-01", "52:54:00:9A:1B:02")
	for name, pair := range map[string][2]infrastructureservices.HostReservation{
		"the BMC context holds the server": {driven, reached},
		"the SSH context holds the server": {reached, driven},
	} {
		held, wanted := pair[0], pair[1]
		conflicts := infrastructureservices.Conflicts([]infrastructureservices.HostReservation{held}, []infrastructureservices.HostReservation{wanted})
		if len(conflicts) != 1 || conflicts[0].Held.Context != held.Context || !strings.HasPrefix(conflicts[0].HeldKey, "bmc-mac:") {
			t.Fatalf("%s: conflicts = %+v", name, conflicts)
		}
		diagnostic := infrastructureservices.ConflictDiagnostic(conflicts[0])
		if diagnostic.Code != "controller.conflict" || !strings.HasPrefix(diagnostic.Message, "context "+held.Context+" holds ") ||
			!strings.Contains(diagnostic.Message, "Machine/") || strings.Contains(diagnostic.Message, "52:54") || diagnostic.Object == nil || diagnostic.Object.Kind != "Machine" {
			t.Fatalf("%s: diagnostic = %+v", name, diagnostic)
		}
	}
	other := physicalClaim(t, "bmc", "https://192.0.2.41/redfish/v1/Systems/1", "52:54:00:9a:1b:03")
	if conflicts := infrastructureservices.Conflicts([]infrastructureservices.HostReservation{other}, []infrastructureservices.HostReservation{reached}); len(conflicts) != 0 {
		t.Fatalf("another server conflicts: %+v", conflicts)
	}
}

// The apply reads the host and completes unchanged only on a complete read of
// this request, observation reads it again changing nothing, and the removal
// releases the claim without reading the host.
func TestTheHardwareReadChangesNothingAndItsReleaseReadsNothing(t *testing.T) {
	block := servicesBlock(t)
	runner := &scripted{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: evidenceOf(t, block.RequestDigest, "52:54:00:9a:1b:01")}}
	execution := lifecycle.Execution{Block: block, Context: "lab", Attempt: 1}
	result, err := New(runner).Apply(context.Background(), execution)
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged || runner.seen.Operation != "apply" || runner.seen.Placement.Connection != machine.ConnectionSSH {
		t.Fatalf("apply = %+v (%v), ran %+v", result, err, runner.seen)
	}
	runner.result = lifecycle.RunResult{Outcome: "maybe", Evidence: evidenceOf(t, block.RequestDigest, "52:54:00:9a:1b:01")}
	if result, err := New(runner).Apply(context.Background(), execution); err == nil || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("a read with no usable outcome = %+v (%v)", result, err)
	}
	runner.result = lifecycle.RunResult{Outcome: "unchanged", Evidence: evidenceOf(t, "another", "52:54:00:9a:1b:01")}
	if observed, err := New(runner).Observe(context.Background(), execution); err != nil || observed.Effect != reconciliation.EffectUnknown {
		t.Fatalf("another request's read observed %+v (%v)", observed, err)
	}
	removal, err := New(nil).ObserveRemoval(context.Background(), execution)
	if err != nil || removal.Effect != reconciliation.EffectCompleted {
		t.Fatalf("removal = %+v (%v)", removal, err)
	}
}

type scripted struct {
	result lifecycle.RunResult
	err    error
	seen   lifecycle.RunRequest
}

func (s *scripted) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	s.seen = request
	return s.result, s.err
}

func TestHardwareClaimAdmissionRequiresItsBoundHostPin(t *testing.T) {
	ctx := context.Background()
	block := servicesBlock(t)
	capability := New(nil)
	if err := capability.ProveBinding(ctx, "lab", block, nil); err == nil || codeOf(t, err) != "trust.identity" {
		t.Fatalf("missing pin = %v", err)
	}
	block.Request = nil
	if err := capability.ProveBinding(ctx, "lab", block, nil); err == nil {
		t.Fatal("invalid frozen request admitted")
	}
}

func TestMachineHardwareRequestAndEvidenceGolden(t *testing.T) {
	block := servicesBlock(t)
	matchesGolden(t, "machine-hardware-request", block.Request, false)
	evidence := evidenceOf(t, block.RequestDigest, "52:54:00:9a:1b:01")
	if _, err := presence(evidence, block.RequestDigest); err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "machine-hardware-evidence", evidence, false)
}

func tiebreakerCatalog(fleet bool) api.Catalog {
	environment := api.MapValue(field("controller", api.MapValue(text("machineRef", "controller"))))
	if fleet {
		environment = environment.With("remoteMachinesAccessKey", api.MapValue(text("keyRef", "fleet-key")))
	}
	return api.NewCatalog([]api.Object{
		api.NewObject(api.Environment, "lab", api.Value{}, environment),
		api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue(field("access", api.MapValue(field("local", api.BoolValue(true)))))),
		api.NewObject(api.Secret, "fleet-key", api.Value{}, api.MapValue(text("type", "sshKeyPair"))),
		api.NewObject(api.Machine, "tie", api.Value{}, api.MapValue(
			field("os", api.MapValue(field("provided", api.MapValue()))),
			field("network", api.MapValue(field("addresses", api.ListValue(api.MapValue(text("name", "ip"), text("address", "198.51.100.10")))))),
			field("access", api.MapValue(field("ssh", api.MapValue(text("addressRef", "ip"), text("user", "operator"), text("knownHostsRef", "tie-known-hosts"),
				field("auth", api.MapValue(field("operatorIdentity", api.MapValue()))))))))),
		api.NewObject(api.CephCluster, "example", api.Value{}, api.MapValue(field("topology", api.MapValue(field("nodes", api.ListValue(
			api.MapValue(text("name", "tie"), text("machineRef", "tie")))))))),
	})
}

// A Ceph node on an OS-ready Machine, which Ceph dials over SSH as the cluster
// SSH user with the fleet key and which hosts no placement, is claimed by its
// hardware too, read over that same connection without escalation, and meets
// a context that drives the same server by its BMC; a Machine Ceph could not
// dial plans no claim (D257 (2)).
func TestACephNodeOnAnOSReadyServerIsClaimedAndMeetsItsBMCDriver(t *testing.T) {
	if plan := planned(t, tiebreakerCatalog(false), reconciliation.Apply); len(plan.Definitions) != 0 {
		t.Fatalf("a Machine Ceph cannot dial planned %+v", plan.Definitions)
	}
	plan := planned(t, tiebreakerCatalog(true), reconciliation.Apply)
	if len(plan.Definitions) != 1 || plan.Definitions[0].ID != "machine-hardware-tie" {
		t.Fatalf("definitions = %+v", plan.Definitions)
	}
	request, err := DecodeRequest(plan.Definitions[0].Request)
	if err != nil || request.Placement.User != "operator" || request.Placement.PrivateKeyRef != "fleet-key" || request.Placement.KnownHostsRef != "tie-known-hosts" ||
		request.Placement.Address != "198.51.100.10" || !slices.Equal(plan.Secrets, []string{"fleet-key", "tie-known-hosts"}) {
		t.Fatalf("request = %+v (%v), secrets %v", request, err, plan.Secrets)
	}
	digest, err := reconciliation.RequestDigest(plan.Definitions[0])
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{BlockDefinition: plan.Definitions[0], RequestDigest: digest}
	reached, found, err := New(nil).ProvedClaim("storage", block, evidenceOf(t, digest, "52:54:00:9a:1b:01"))
	if err != nil || !found {
		t.Fatalf("claim = %v (%v)", found, err)
	}
	driven := physicalClaim(t, "bmc", "https://bmc.example.test/redfish/v1/Systems/1", "52:54:00:9A:1B:01", "52:54:00:9a:1b:02")
	for _, pair := range [][2]infrastructureservices.HostReservation{{driven, reached}, {reached, driven}} {
		if conflicts := infrastructureservices.Conflicts(pair[:1], pair[1:]); len(conflicts) != 1 || conflicts[0].Held.Context != pair[0].Context {
			t.Fatalf("held by %s: conflicts = %+v", pair[0].Context, conflicts)
		}
	}
}

// An OpenShift node is no OS-ready Machine: its operating system is installed
// by its cluster, after this stage, so even with SSH access and a fleet key it
// plans no claim of its own that would dial a server not yet installed.
func TestAClusterNodeWhoseOperatingSystemIsNotProvidedPlansNoClaim(t *testing.T) {
	objects := tiebreakerCatalog(true).Objects()
	for index, object := range objects {
		switch object.Kind() {
		case api.Machine:
			if object.Name() == "tie" {
				objects[index] = object.WithSpec(object.Spec().WithPath(api.MapValue(field("clusterInstalled", api.MapValue())), "os"))
			}
		case api.CephCluster:
			objects[index] = api.NewObject(api.OpenShiftCluster, "ocp", api.Value{}, api.MapValue(field("nodes", api.ListValue(
				api.MapValue(text("name", "tie"), text("machineRef", "tie"))))))
		}
	}
	if plan := planned(t, api.NewCatalog(objects), reconciliation.Apply); len(plan.Definitions) != 0 {
		t.Fatalf("a node whose OS is not provided planned %+v", plan.Definitions)
	}
	objects[len(objects)-1] = api.NewObject(api.OpenShiftCluster, "ocp", api.Value{}, api.MapValue(field("nodes", api.ListValue(
		api.MapValue(text("name", "tie"), text("machineRef", "tie"))))))
	for index, object := range objects {
		if object.Kind() == api.Machine && object.Name() == "tie" {
			objects[index] = object.WithSpec(object.Spec().WithPath(api.MapValue(field("provided", api.MapValue())), "os"))
		}
	}
	if plan := planned(t, api.NewCatalog(objects), reconciliation.Apply); len(plan.Definitions) != 1 {
		t.Fatalf("a provided node of a cluster planned %+v", plan.Definitions)
	}
}

// A host that reports no permanent hardware address refuses, naming the
// Machine, both at its apply and as proved evidence, because an empty set
// claims nothing and so serializes nothing against a context that drives the
// server by its BMC.
func TestAHostReportingNoPermanentAddressRefuses(t *testing.T) {
	block := servicesBlock(t)
	var warned []string
	execution := lifecycle.Execution{Block: block, Context: "lab", Attempt: 1, Warn: func(_ context.Context, warnings []operationstore.Warning) {
		for _, warning := range warnings {
			warned = append(warned, warning.Code+" "+warning.Message)
		}
	}}
	runner := &scripted{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: evidenceOf(t, block.RequestDigest)}}
	result, err := New(runner).Apply(context.Background(), execution)
	if err == nil || codeOf(t, err) != "hardware.addresses-none" || result.Outcome != reconciliation.OutcomeFailed || len(warned) != 0 ||
		!strings.Contains(diagnostics.Of(err)[0].Message, "Machine/services reports no permanent hardware address") {
		t.Fatalf("apply = %+v (%v), warned %v", result, err, warned)
	}
	if _, found, err := New(nil).ProvedClaim("lab", block, evidenceOf(t, block.RequestDigest)); err == nil || found || codeOf(t, err) != "hardware.addresses-none" {
		t.Fatalf("an empty proved claim = %v (%v)", found, err)
	}
	runner.result = lifecycle.RunResult{Outcome: "unchanged", Evidence: evidenceOf(t, block.RequestDigest, "52:54:00:9a:1b:01")}
	if _, err := New(runner).Apply(context.Background(), execution); err != nil || len(warned) != 0 {
		t.Fatalf("a claimed host warned %v (%v)", warned, err)
	}
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 {
		t.Fatalf("diagnostics = %+v (%v)", reported, err)
	}
	return reported[0].Code
}

func blockWithRequest(t *testing.T, block reconciliation.Block, edit func(*Request)) reconciliation.Block {
	t.Helper()
	request, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	edit(&request)
	block.Request, err = request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return block
}

// Planning refuses a cancelled context and a plan with no compiled desired
// state, and plans no definition either way.
func TestPlanningRefusesWhatItCannotCompile(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(nil).Plan(cancelled, lifecycle.PlanInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled plan = %v", err)
	}
	plan, err := New(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply})
	if code := codeOf(t, err); code != hardwareDesiredStateMissing.Code || plan.Definitions == nil || len(plan.Definitions) != 0 {
		t.Fatalf("plan with no state = %+v, %s", plan, code)
	}
}

// The frozen request is refused unless it names a Machine reached over SSH
// whose block identity is its own, and a run refuses before any adapter
// starts when the block names another Machine or no adapter is bound.
func TestARunRefusesARequestThatIsNotItsBlocks(t *testing.T) {
	block := servicesBlock(t)
	for name, edit := range map[string]func(*Request){
		"local placement":        func(r *Request) { r.Placement.Connection = machine.ConnectionLocal },
		"no known hosts":         func(r *Request) { r.Placement.KnownHostsRef = "" },
		"no private key":         func(r *Request) { r.Placement.PrivateKeyRef = "" },
		"no address":             func(r *Request) { r.Placement.Address = "" },
		"another placed Machine": func(r *Request) { r.Placement.Machine = "other" },
		"another block":          func(r *Request) { r.Identity.Block = BlockID("other") },
		"no context":             func(r *Request) { r.Identity.Context = "" },
	} {
		if _, err := DecodeRequest(blockWithRequest(t, block, edit).Request); err == nil || codeOf(t, err) != hardwareRequestInvalid.Code {
			t.Errorf("%s: decoded (%v)", name, err)
		}
	}
	runner := &scripted{}
	execution := lifecycle.Execution{Block: block, Context: "lab", Attempt: 1}
	execution.Block.Object = "other"
	result, err := New(runner).Apply(context.Background(), execution)
	if code := codeOf(t, err); code != hardwareRequestMismatch.Code || result.Outcome != reconciliation.OutcomeFailed || runner.seen.Operation != "" {
		t.Fatalf("a mismatched block = %+v, %s, ran %+v", result, code, runner.seen)
	}
	execution.Block = block
	result, err = New(nil).Apply(context.Background(), execution)
	if code := codeOf(t, err); code != hardwareUnconfigured.Code || result.Outcome != reconciliation.OutcomeFailed {
		t.Fatalf("an unbound adapter = %+v, %s", result, code)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(runner).Observe(cancelled, execution); !errors.Is(err, context.Canceled) || runner.seen.Operation != "" {
		t.Fatalf("a cancelled run = %v, ran %+v", err, runner.seen)
	}
}

// An adapter that fails or answers with evidence of another read leaves an
// apply unknown and an observation unknown, never completed.
func TestAnAdapterFailureOrUnreadableEvidenceProvesNoRead(t *testing.T) {
	block := servicesBlock(t)
	execution := lifecycle.Execution{Block: block, Context: "lab", Attempt: 1}
	failure := hardwareUnproved.Fail("the adapter ended", "")
	runner := &scripted{err: failure}
	if result, err := New(runner).Apply(context.Background(), execution); err == nil || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("a failed adapter apply = %+v (%v)", result, err)
	}
	if observed, err := New(runner).Observe(context.Background(), execution); err == nil || observed.Effect != reconciliation.EffectUnknown {
		t.Fatalf("a failed adapter observe = %+v (%v)", observed, err)
	}
	runner = &scripted{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: evidenceOf(t, "another", "52:54:00:9a:1b:01")}}
	result, err := New(runner).Apply(context.Background(), execution)
	if code := codeOf(t, err); code != hardwareEvidenceIncomplete.Code || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("another request's read applied %+v, %s", result, code)
	}
	runner.result = lifecycle.RunResult{Outcome: "unchanged", Evidence: evidenceOf(t, block.RequestDigest, "52:54:00:9a:1b:01")}
	observed, err := New(runner).Observe(context.Background(), execution)
	if err != nil || observed.Effect != reconciliation.EffectCompleted || observed.Outcome != reconciliation.OutcomeUnchanged || runner.seen.Operation != "observe" {
		t.Fatalf("observe = %+v (%v), ran %+v", observed, err, runner.seen)
	}
	runner.result = lifecycle.RunResult{Outcome: "unchanged", Evidence: []byte(`{"version":"` + EvidenceVersion + `","addresses":["a"]}`)}
	if result, err := New(runner).Apply(context.Background(), execution); codeOf(t, err) != hardwareEvidenceMalformed.Code || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("malformed evidence applied %+v (%v)", result, err)
	}
}

// A removal releases the claim and retains the server: its plan names the
// release with no impact, its destroy completes only on evidence that this
// request's claim is absent, its observation proves that absence without
// reading the host, it is always quiescent, and it runs before the controller.
func TestTheRemovalReleasesTheClaimAndProvesItsAbsence(t *testing.T) {
	block := servicesBlock(t)
	capability := New(nil)
	removal, err := capability.Removal(context.Background(), block)
	if err != nil || !strings.HasPrefix(removal.Description, "release the claim on the server behind services") || len(removal.Impacts) != 0 ||
		len(removal.Groups) != 1 || removal.Groups[0].ID != "release-claim" || !slices.Equal(removal.Groups[0].Machines, []string{"services"}) || len(removal.Consumes) != 0 {
		t.Fatalf("removal = %+v (%v)", removal, err)
	}
	if _, err := capability.Removal(context.Background(), reconciliation.Block{}); err == nil {
		t.Fatal("a removal with no frozen request was planned")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := capability.Removal(cancelled, block); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled removal = %v", err)
	}
	if _, err := capability.ObserveRemoval(cancelled, lifecycle.Execution{Block: block}); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled removal observation = %v", err)
	}
	if observed, err := capability.ObserveRemoval(context.Background(), lifecycle.Execution{Block: reconciliation.Block{}}); err == nil || observed.Effect != reconciliation.EffectUnknown {
		t.Fatalf("a removal observation with no request = %+v (%v)", observed, err)
	}
	observed, err := capability.ObserveRemoval(context.Background(), lifecycle.Execution{Block: block})
	if err != nil || observed.Effect != reconciliation.EffectCompleted {
		t.Fatalf("observed removal = %+v (%v)", observed, err)
	}
	proved, err := decodeEvidence(observed.Evidence)
	if err != nil || !proved.Absent || !proved.Postcondition || proved.Request != block.RequestDigest || len(proved.Addresses) != 0 {
		t.Fatalf("removal evidence = %+v (%v)", proved, err)
	}
	request, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	execution := lifecycle.Execution{Block: block, Context: "lab", Attempt: 1, Material: map[string]secrets.Material{
		request.Placement.KnownHostsRef: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("malformed")}),
		request.Placement.PrivateKeyRef: secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: []byte("test key")}),
	}}
	runner := &scripted{result: lifecycle.RunResult{Outcome: "changed", Evidence: observed.Evidence}}
	result, err := New(runner).Destroy(context.Background(), execution)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged || runner.seen.Operation != "destroy" {
		t.Fatalf("destroy = %+v (%v), ran %+v", result, err, runner.seen)
	}
	if runner.seen.Placement != (machine.Placement{Connection: machine.ConnectionLocal, Machine: block.Object}) ||
		len(runner.seen.Materials) != 0 || len(runner.seen.Material) != 0 {
		t.Fatalf("destroy used placement %+v and materials %+v", runner.seen.Placement, runner.seen.Materials)
	}
	if runner.seen.Digest != block.RequestDigest || !slices.Equal(runner.seen.Canonical, block.Request) {
		t.Fatal("destroy changed the frozen request or its digest")
	}
	for name, evidence := range map[string][]byte{
		"a read of the host": evidenceOf(t, block.RequestDigest, "52:54:00:9a:1b:01"),
		"another request":    evidenceOf(t, "another"),
		"malformed":          []byte(`{}`),
	} {
		runner.result = lifecycle.RunResult{Outcome: "changed", Evidence: evidence}
		result, err := New(runner).Destroy(context.Background(), execution)
		if code := codeOf(t, err); code != hardwareUnproved.Code || result.Outcome != reconciliation.OutcomeUnknown {
			t.Errorf("destroy on %s = %+v, %s", name, result, code)
		}
	}
	runner.result = lifecycle.RunResult{Outcome: "maybe", Evidence: observed.Evidence}
	if result, err := New(runner).Destroy(context.Background(), execution); codeOf(t, err) != hardwareUnproved.Code || result.Outcome != reconciliation.OutcomeUnknown {
		t.Errorf("destroy with no usable outcome = %+v (%v)", result, err)
	}
	runner.err = hardwareUnproved.Fail("the adapter ended", "")
	if result, err := New(runner).Destroy(context.Background(), execution); err == nil || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("a failed adapter destroy = %+v (%v)", result, err)
	}
	if state, err := capability.Quiescent(context.Background(), lifecycle.Probe{}); err != nil || state.State != lifecycle.Quiescent || !strings.Contains(state.Reason, "retains the server") {
		t.Fatalf("quiescence = %+v (%v)", state, err)
	}
	if !capability.PrecedesController(block.BlockDefinition) || capability.Claimed(api.Catalog{}) != nil {
		t.Fatal("the hardware read follows the controller or claims a catalog object")
	}
}

// A block's secret references are the SSH key and known-hosts Secrets of its
// placement, classed as an access credential and a trust anchor, and a block
// with no valid request names none.
func TestABlocksSecretsAreItsPlacementsSSHSecrets(t *testing.T) {
	block := servicesBlock(t)
	references, err := New(nil).SecretReferences(block)
	if err != nil || !slices.Equal(references, []string{"services-key", "services-known-hosts"}) {
		t.Fatalf("references = %v (%v)", references, err)
	}
	classes, err := New(nil).SecretClasses(block)
	if want := (secrets.Classes{}).Add(secrets.AccessCredential, "services-key").Add(secrets.TrustAnchor, "services-known-hosts"); err != nil || !maps.Equal(classes, want) {
		t.Fatalf("classes = %v, want %v (%v)", classes, want, err)
	}
	if _, err := New(nil).SecretReferences(reconciliation.Block{}); err == nil {
		t.Fatal("references of a block with no request")
	}
	if _, err := New(nil).SecretClasses(reconciliation.Block{}); err == nil {
		t.Fatal("classes of a block with no request")
	}
	if _, found, err := New(nil).ProvedClaim("lab", reconciliation.Block{}, nil); err == nil || found {
		t.Fatalf("a claim of a block with no request = %v (%v)", found, err)
	}
}

// An apply whose evidence reads as this request's but proves no complete
// read stays unknown with the drift named on its host; a removal, a missing
// reading, another request's reading, a complete read and a block with no
// request are never drift.
func TestAnIncompleteReadOfThisRequestIsNamedAsDrift(t *testing.T) {
	block := servicesBlock(t)
	incomplete := []byte(`{"absent":false,"addresses":[],"postcondition":false,"request":"` + block.RequestDigest + `","version":"` + EvidenceVersion + `"}`)
	unresolved, drifted := New(nil).Unresolved(reconciliation.Apply, block, incomplete)
	if !drifted || !strings.Contains(unresolved.Reason, "the network interfaces of Machine services on Machine services at 192.0.2.9") ||
		!strings.Contains(unresolved.Reason, "is not what its apply froze") || !strings.HasPrefix(unresolved.Remedy, "restore ") {
		t.Fatalf("unresolved = %+v, %v", unresolved, drifted)
	}
	for name, probe := range map[string]struct {
		verb     reconciliation.Verb
		block    reconciliation.Block
		evidence []byte
	}{
		"a removal":          {reconciliation.Destroy, block, incomplete},
		"no reading":         {reconciliation.Apply, block, nil},
		"malformed reading":  {reconciliation.Apply, block, []byte(`{}`)},
		"another request":    {reconciliation.Apply, block, evidenceOf(t, "another")},
		"a complete read":    {reconciliation.Apply, block, evidenceOf(t, block.RequestDigest, "52:54:00:9a:1b:01")},
		"a block no request": {reconciliation.Apply, reconciliation.Block{}, incomplete},
	} {
		if got, drifted := New(nil).Unresolved(probe.verb, probe.block, probe.evidence); drifted || got != (lifecycle.Unresolved{}) {
			t.Errorf("%s drifted as %+v", name, got)
		}
	}
}

// The stops a package declares are a copy, so a caller sorting or editing
// them leaves the declared catalog as it was.
func TestTheDeclaredStopsAreACopy(t *testing.T) {
	first := Stops()
	first[0].Code = "edited"
	if second := Stops(); second[0].Code != hardwareDesiredStateMissing.Code || len(second) != len(stops) {
		t.Fatalf("stops = %+v", second)
	}
}

func TestTheHardwareClaimUsesTheLoginWithoutSudo(t *testing.T) {
	block := servicesBlock(t)
	request, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	request.Placement.User, request.Placement.Privilege = "automation", "sudo"
	block.Request, err = request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	runner := &scripted{result: lifecycle.RunResult{
		Outcome:  reconciliation.OutcomeUnchanged,
		Evidence: evidenceOf(t, block.RequestDigest, "52:54:00:9a:1b:01"),
	}}
	_, err = New(runner).Apply(context.Background(), lifecycle.Execution{Block: block, Context: "lab", Attempt: 1})
	if err != nil || runner.seen.Placement.User != "automation" || runner.seen.Placement.Sudo() {
		t.Fatalf("claim used %+v (%v)", runner.seen.Placement, err)
	}
}

func TestTheHardwareClaimBorrowsTheBootstrapLoginBeforeEnrollment(t *testing.T) {
	block := servicesBlock(t)
	request, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	request.Bootstrap = &machine.Bootstrap{Auth: "askPassword", Sudo: true, SudoPrompt: true}
	request.Placement.User, request.Placement.Privilege = "automation", "sudo"
	block.Request, err = request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	reader := func(_ context.Context, contextName string, place machine.Placement, access machine.Bootstrap, _ map[string]secrets.Material) ([]string, error) {
		calls++
		if contextName != "lab" || place.Machine != block.Object || access.Auth != "askPassword" {
			t.Fatal("the hardware claim changed the bootstrap route")
		}
		return []string{"52:54:00:9a:1b:01"}, nil
	}
	prompted := 0
	refused := errors.New("missing credential")
	prompt := func(_ context.Context, contextName string, place machine.Placement, access machine.Bootstrap, _ map[string]secrets.Material) error {
		prompted++
		if contextName != "lab" || place.Machine != block.Object || access.Auth != "askPassword" {
			t.Fatal("the prompt changed the bootstrap route")
		}
		return refused
	}
	capability := New(nil).WithBootstrap(reader, prompt)
	if err := capability.PromptApplyStart(context.Background(), lifecycle.Probe{Block: block, Context: "lab"}); !errors.Is(err, refused) || prompted != 1 || calls != 0 {
		t.Fatalf("start prompt = %v, prompts = %d, hardware reads = %d", err, prompted, calls)
	}
	if err := capability.PromptApplyStart(context.Background(), lifecycle.Probe{Block: servicesBlock(t), Context: "lab"}); err != nil || prompted != 1 {
		t.Fatalf("a claim without bootstrap prompted %d times (%v)", prompted, err)
	}
	if !capability.IndependentOfController(block.BlockDefinition) {
		t.Fatal("the native hardware claim depends on controller stage work")
	}
	result, err := capability.Apply(context.Background(), lifecycle.Execution{Block: block, Context: "lab", Attempt: 1})
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged || calls != 1 {
		t.Fatalf("bootstrap claim = %+v, calls=%d (%v)", result, calls, err)
	}
	if _, err := New(nil).Apply(context.Background(), lifecycle.Execution{Block: block, Context: "lab", Attempt: 1}); err == nil {
		t.Fatal("an enrollment claim without its bootstrap reader succeeded")
	}
}

func TestStandaloneEnrollmentPlansAClaimWithItsBoundBootstrapKey(t *testing.T) {
	host := sshHost("services", "192.0.2.9")
	lifecycleLogin := api.MapValue(
		text("privateKeyRef", "managed-key"), text("knownHostsRef", "services-known-hosts"),
		text("user", "automation"), text("privilege", "sudo"),
		field("bootstrap", api.MapValue(text("user", "operator"),
			field("auth", api.MapValue(text("privateKeyRef", "bootstrap-key"))),
			field("privilege", api.MapValue(field("sudo", api.MapValue()))))),
	)
	host = host.WithSpec(host.Spec().WithPath(lifecycleLogin, "access", "lifecycle"))
	plan := planned(t, catalogOf(host), reconciliation.Apply)
	if len(plan.Definitions) != 1 || !slices.Equal(plan.Secrets, []string{"bootstrap-key", "managed-key", "services-known-hosts"}) {
		t.Fatalf("standalone enrollment plan: %+v", plan)
	}
	request, err := DecodeRequest(plan.Definitions[0].Request)
	if err != nil || request.Bootstrap.AuthRef != "bootstrap-key" || request.SecretClasses()["bootstrap-key"] != secrets.AccessCredential {
		t.Fatal("the claim did not bind the bootstrap credential")
	}
}
