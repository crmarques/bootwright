package hardware

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
)

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func text(name, value string) api.FieldValue { return field(name, api.StringValue(value)) }

func sshHost(name, address string, extra ...api.FieldValue) api.Object {
	fields := append([]api.FieldValue{
		field("os", api.MapValue(field("provided", api.BoolValue(true)))),
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
		field("os", api.MapValue(field("provided", api.BoolValue(false)), text("installProfileRef", "rhel")))))
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
	if want := []prerequisites.HostReservation{{Context: "lab", Kind: ClaimKind, Service: "services"}}; !slices.EqualFunc(plan.Reservations, want, func(x, y prerequisites.HostReservation) bool {
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
// address the host reported, in canonical form; a host reporting none claims
// nothing; one reporting more than one reservation holds refuses rather than
// claim the server by a part of its addresses; and evidence that proves no
// complete read claims nothing.
func TestTheProvedClaimIsOneBMCMACKeyPerPermanentAddress(t *testing.T) {
	block := servicesBlock(t)
	claim, found, err := New(nil).ProvedClaim("lab", block, evidenceOf(t, block.RequestDigest, "52:54:00:9a:1b:01", "52:54:00:9a:1b:02"))
	if err != nil || !found || claim.Context != "lab" || claim.Kind != ClaimKind || claim.Service != "services" || claim.Shared ||
		!slices.Equal(claim.Keys, []string{"bmc-mac:52:54:00:9a:1b:01", "bmc-mac:52:54:00:9a:1b:02"}) {
		t.Fatalf("claim = %+v, %v (%v)", claim, found, err)
	}
	if _, found, err := New(nil).ProvedClaim("lab", block, evidenceOf(t, block.RequestDigest)); err != nil || found {
		t.Fatalf("a host with no permanent address claimed %v (%v)", found, err)
	}
	many := make([]string, 0, prerequisites.MaxReservationKeys+1)
	for index := range prerequisites.MaxReservationKeys + 1 {
		many = append(many, fmt.Sprintf("52:54:00:00:%02x:%02x", index/256, index%256))
	}
	if _, found, err := New(nil).ProvedClaim("lab", block, evidenceOf(t, block.RequestDigest, many[:prerequisites.MaxReservationKeys]...)); err != nil || !found {
		t.Fatalf("the largest address set was refused: %v", err)
	}
	_, _, err = New(nil).ProvedClaim("lab", block, evidenceOf(t, block.RequestDigest, many...))
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.state" || !strings.Contains(reported[0].Message, "65 permanent hardware addresses") {
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

func physicalClaim(t *testing.T, contextName, endpoint string, macs ...string) prerequisites.HostReservation {
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
	for name, pair := range map[string][2]prerequisites.HostReservation{
		"the BMC context holds the server": {driven, reached},
		"the SSH context holds the server": {reached, driven},
	} {
		held, wanted := pair[0], pair[1]
		conflicts := prerequisites.Conflicts([]prerequisites.HostReservation{held}, []prerequisites.HostReservation{wanted})
		if len(conflicts) != 1 || conflicts[0].Held.Context != held.Context || !strings.HasPrefix(conflicts[0].HeldKey, "bmc-mac:") {
			t.Fatalf("%s: conflicts = %+v", name, conflicts)
		}
		diagnostic := prerequisites.ConflictDiagnostic(conflicts[0])
		if diagnostic.Code != "controller.conflict" || !strings.HasPrefix(diagnostic.Message, "context "+held.Context+" holds ") ||
			!strings.Contains(diagnostic.Message, "Machine/") || strings.Contains(diagnostic.Message, "52:54") || diagnostic.Object == nil || diagnostic.Object.Kind != "Machine" {
			t.Fatalf("%s: diagnostic = %+v", name, diagnostic)
		}
	}
	other := physicalClaim(t, "bmc", "https://192.0.2.41/redfish/v1/Systems/1", "52:54:00:9a:1b:03")
	if conflicts := prerequisites.Conflicts([]prerequisites.HostReservation{other}, []prerequisites.HostReservation{reached}); len(conflicts) != 0 {
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
	seen   lifecycle.RunRequest
}

func (s *scripted) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	s.seen = request
	return s.result, nil
}

func TestMachineHardwareRequestAndEvidenceGolden(t *testing.T) {
	block := servicesBlock(t)
	matchesGolden(t, "machine-hardware-request", block.Request)
	evidence := evidenceOf(t, block.RequestDigest, "52:54:00:9a:1b:01")
	if _, err := presence(evidence, block.RequestDigest); err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "machine-hardware-evidence", evidence)
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
			field("os", api.MapValue(field("provided", api.BoolValue(true)))),
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
	for _, pair := range [][2]prerequisites.HostReservation{{driven, reached}, {reached, driven}} {
		if conflicts := prerequisites.Conflicts(pair[:1], pair[1:]); len(conflicts) != 1 || conflicts[0].Held.Context != pair[0].Context {
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
				objects[index] = object.WithSpec(object.Spec().WithPath(api.BoolValue(false), "os", "provided"))
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
			objects[index] = object.WithSpec(object.Spec().WithPath(api.BoolValue(true), "os", "provided"))
		}
	}
	if plan := planned(t, api.NewCatalog(objects), reconciliation.Apply); len(plan.Definitions) != 1 {
		t.Fatalf("a provided node of a cluster planned %+v", plan.Definitions)
	}
}

// A host that reports no permanent hardware address completes with nothing
// claimed and warns, naming the Machine, rather than refusing.
func TestAHostReportingNoPermanentAddressWarns(t *testing.T) {
	block := servicesBlock(t)
	var warned []string
	execution := lifecycle.Execution{Block: block, Context: "lab", Attempt: 1, Warn: func(_ context.Context, warnings []operationstore.Warning) {
		for _, warning := range warnings {
			warned = append(warned, warning.Code+" "+warning.Message)
		}
	}}
	runner := &scripted{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: evidenceOf(t, block.RequestDigest)}}
	result, err := New(runner).Apply(context.Background(), execution)
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged || len(warned) != 1 ||
		!strings.HasPrefix(warned[0], UnclaimedServer+" Machine/services reports no permanent hardware address") {
		t.Fatalf("apply = %+v (%v), warned %v", result, err, warned)
	}
	runner.result = lifecycle.RunResult{Outcome: "unchanged", Evidence: evidenceOf(t, block.RequestDigest, "52:54:00:9a:1b:01")}
	if _, err := New(runner).Apply(context.Background(), execution); err != nil || len(warned) != 1 {
		t.Fatalf("a claimed host warned %v (%v)", warned, err)
	}
}
