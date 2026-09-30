//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const snoExampleFiles = 14

func snoExampleSources(t *testing.T) desiredstate.Sources {
	t.Helper()
	return exampleDirectory(t, "lab-sno")
}

// The example is admitted whole, and the clients its cluster needs are selected
// by that graph rather than by the baseline setup: the controller stage
// installs the installer whose release the cluster declares.
func TestLabSNOExampleSelectsItsInstallerFromTheDeclaredRelease(t *testing.T) {
	sources := snoExampleSources(t)
	if len(sources.Files) != snoExampleFiles {
		t.Fatalf("sno example discovery: got %d files, want %d", len(sources.Files), snoExampleFiles)
	}
	out, errOut := contextRun(t, isolatedServices(t), 0, "validate", "-f", sources.Roots[0], "--output", "json")
	var validation struct {
		OK          bool
		Result      struct{ Counts compilation.Counts }
		Diagnostics []any
	}
	if err := json.Unmarshal([]byte(out), &validation); err != nil || !validation.OK || errOut != "" || len(validation.Diagnostics) != 0 {
		t.Fatalf("sno example admission: out=%s err=%s %v", out, errOut, err)
	}
	state, _ := compileAcceptance(t, sources)
	effective := state.Effective()
	tools, err := controller.SelectTools(effective)
	if err != nil {
		t.Fatalf("tool selection: %v", err)
	}
	selected := map[string]string{}
	for _, tool := range tools {
		selected[tool.Kind] = tool.Version
	}
	for _, kind := range []string{"openshift-install", "openshift-clients"} {
		if selected[kind] != "4.21.15" {
			t.Fatalf("%s selected %q, want the declared release", kind, selected[kind])
		}
	}
	for _, object := range []struct {
		kind api.Kind
		name string
	}{
		{api.Machine, "controller"}, {api.Machine, "sno-01"}, {api.ContainerCluster, "sno"},
		{api.InfraProvider, "lab-libvirt"}, {api.NetworkConfig, "lab-guests"},
		{api.Proxy, "lab-proxy"}, {api.DNSServer, "lab-dns"},
		{api.NTPServer, "lab-ntp"}, {api.ArtifactServer, "lab-artifacts"},
	} {
		requireObject(t, effective, object.kind, object.name)
	}
	// The node is installer-provisioned: its substrate realizes the hardware
	// and the cluster installer writes its operating system, so it selects no
	// install profile and Bootwright installs nothing on it.
	node := requireObject(t, effective, api.Machine, "sno-01")
	if node.Spec().Has("os", "installProfileRef") || node.Spec().Get("os", "provided").Bool() {
		t.Fatal("the cluster node is not installer-provisioned", node.Spec().Get("os"))
	}
	if !slices.Contains(node.Spec().Get("capabilities").Strings(), "openshift-node") {
		t.Fatal("the cluster node does not declare the capability its cluster requires")
	}
}

// A single-node cluster answers at its own node, so normalization resolves all
// three endpoint slots from that one address rather than repeating it.
func TestLabSNOExampleResolvesEveryEndpointFromItsOneNode(t *testing.T) {
	state, _ := compileAcceptance(t, snoExampleSources(t))
	cluster := requireObject(t, state.Effective(), api.ContainerCluster, "sno")
	for _, slot := range []string{"api", "api-int", "ingress"} {
		endpoint := cluster.Spec().Get("install", "endpoints", slot)
		if endpoint.Get("address").Text() != "198.51.100.21" {
			t.Fatalf("%s endpoint = %+v", slot, endpoint)
		}
	}
	// The resolver the graph declares answers those names, so the installer
	// polling the cluster from the controller reaches it.
	records := managedservice.ClusterRecords(state.Effective())
	names := make([]string, 0, len(records))
	for _, record := range records {
		names = append(names, record.Name)
	}
	for _, want := range []string{
		"api-int.sno.lab.example.test", "api.sno.lab.example.test",
		"apps.sno.lab.example.test", "master-0.sno.lab.example.test",
	} {
		if !slices.Contains(names, want) {
			t.Fatalf("records = %v, missing %q", names, want)
		}
	}
}

// The example derives one complete frozen request per block, because that
// request is what an operator's apply would freeze.
func TestLabSNOExampleDerivesItsInstallerInputs(t *testing.T) {
	state, _ := compileAcceptance(t, snoExampleSources(t))
	if unsupported := agentinstall.Unsupported(state.Effective()); len(unsupported) != 0 {
		t.Fatalf("the example declares a cluster this contract cannot install: %v", unsupported)
	}
	media, installs, needs, err := agentinstall.Requests(state.Effective(), "controller", "lab-sno")
	if err != nil {
		t.Fatalf("deriving: %v", diagnostics.Of(err))
	}
	if len(media) != 1 || len(installs) != 1 {
		t.Fatalf("requests = %d media, %d install", len(media), len(installs))
	}
	if media[0].Identity.Block != "cluster-media-sno" || installs[0].Identity.Block != "cluster-install-sno" {
		t.Fatalf("identities = %+v %+v", media[0].Identity, installs[0].Identity)
	}
	if media[0].Placement.Machine != "controller" || media[0].Placement.Connection != "local" {
		t.Fatalf("placement = %+v", media[0].Placement)
	}
	// The image is private, because it carries the operator's pull secret.
	if media[0].Image.URL != "https://192.0.2.1:8443/private/clusters/sno" {
		t.Fatalf("image = %+v", media[0].Image)
	}
	if got := needs[0]; len(got.Machines) != 1 || got.Machines[0] != "sno-01" ||
		len(got.ArtifactServers) != 1 || len(got.DNSServers) != 1 || len(got.NTPServers) != 1 {
		t.Fatalf("requirements = %+v", got)
	}
	if _, err := media[0].Canonical(); err != nil {
		t.Fatalf("the media request is not canonical: %v", err)
	}
	if _, err := installs[0].Canonical(); err != nil {
		t.Fatalf("the install request is not canonical: %v", err)
	}
}

// snoExampleWithHints is the example with its node's root device selected by
// the given hints in place of its device name.
func snoExampleWithHints(t *testing.T, hints string) desiredstate.Sources {
	t.Helper()
	const selected = "deviceName: /dev/vda\n"
	sources := snoExampleSources(t)
	replaced := 0
	for index, file := range sources.Files {
		body := string(file.Bytes())
		replaced += strings.Count(body, selected)
		sources.Files[index] = desiredstate.NewSourceFile(file.Path(), []byte(strings.Replace(body, selected, hints, 1)))
	}
	if replaced != 1 {
		t.Fatalf("the example selects %d root devices, want 1", replaced)
	}
	return sources
}

// Every root-device hint a virtual node is admitted with is admitted on the
// example's node with its type and reaches the agent configuration unchanged:
// strings a YAML 1.1 reader would take for numbers, a size of zero and a disk
// that is not rotational.
func TestLabSNOExampleCarriesEveryRootDeviceHintToItsAgentConfig(t *testing.T) {
	sources := snoExampleWithHints(t, "deviceName: \"/dev/vda\"\n"+
		"        model: \"1e3\"\n"+
		"        vendor: \"0o17\"\n"+
		"        minSizeGigabytes: 0\n"+
		"        rotational: false\n")
	state, _ := compileAcceptance(t, sources)
	if unsupported := agentinstall.Unsupported(state.Effective()); len(unsupported) != 0 {
		t.Fatalf("the example declares a cluster this contract cannot install: %v", unsupported)
	}
	media, _, _, err := agentinstall.Requests(state.Effective(), "controller", "lab-sno")
	if err != nil || len(media) != 1 {
		t.Fatalf("deriving: %d media (%v)", len(media), diagnostics.Of(err))
	}
	hosts, _ := media[0].AgentConfig["hosts"].([]any)
	if len(hosts) != 1 {
		t.Fatalf("hosts = %#v", hosts)
	}
	host, _ := hosts[0].(map[string]any)
	want := map[string]any{
		"deviceName": "/dev/vda", "model": "1e3", "vendor": "0o17", "minSizeGigabytes": int64(0), "rotational": false,
	}
	if !reflect.DeepEqual(host["rootDeviceHints"], want) {
		t.Fatalf("hints = %#v, want %#v", host["rootDeviceHints"], want)
	}
}

// The example's node installs to a disk its libvirt provider creates, which
// carries no WWN, SCSI address or serial number, so admission refuses each of
// those hints at its own path, naming the Machine, before anything is planned.
func TestLabSNOExampleRefusesAHintItsCreatedDiskCannotMatch(t *testing.T) {
	for hint, value := range map[string]string{"hctl": "1:0:0:0", "serialNumber": "0987654321", "wwn": "0x5000c500a1b2c3d4"} {
		t.Run(hint, func(t *testing.T) {
			sources := snoExampleWithHints(t, "deviceName: /dev/vda\n        "+hint+": \""+value+"\"\n")
			state, _, err := wireCompiler().Compile(context.Background(), sources)
			if state != nil || err == nil {
				t.Fatalf("a %s hint on a created disk was admitted", hint)
			}
			for _, diagnostic := range diagnostics.Of(err) {
				if diagnostic.Code == "api.invariant" && diagnostic.Field == "$.spec.os.install.rootDeviceHints."+hint &&
					diagnostic.Object != nil && diagnostic.Object.Kind == string(api.Machine) && diagnostic.Object.Name == "sno-01" {
					return
				}
			}
			t.Fatalf("no %s refusal naming Machine/sno-01: %#v", hint, diagnostics.Of(err))
		})
	}
}

// Every capability derives its blocks from the example alone, and the whole set
// orders into one plan whose cluster work waits for the services it publishes
// through.
func TestLabSNOExamplePlansTheWholeGraph(t *testing.T) {
	state, _ := compileAcceptance(t, snoExampleSources(t))
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t))
	input := lifecycle.PlanInput{
		Verb: reconciliation.Apply, State: state, Controller: "controller",
		Context: lifecycle.ContextIdentity{Name: "lab-sno"},
	}
	if unsupported := lifecycle.Unrealizable(state.Effective(), claimedKinds(resolver)); len(unsupported) != 0 {
		t.Fatalf("the example declares objects no capability claims: %v", unsupported)
	}
	var definitions []reconciliation.BlockDefinition
	var claims []prerequisites.HostReservation
	for _, binding := range resolver.Bindings() {
		capability, ok := resolver.Resolve(binding.Kind, binding.Implementation)
		if !ok {
			t.Fatalf("%s/%s does not resolve", binding.Kind, binding.Implementation)
		}
		if reporter, ok := capability.(lifecycle.UnsupportedReporter); ok {
			if unsupported := reporter.Unsupported(state); len(unsupported) != 0 {
				t.Fatalf("a capability cannot realize %v", unsupported)
			}
		}
		contribution, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatalf("%s plan: %v", binding.Kind, err)
		}
		definitions = append(definitions, contribution.Definitions...)
		claims = append(claims, contribution.Reservations...)
	}
	requireCanonicalReservations(t, claims)
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal("the example produced a block the plan model refuses:", err)
	}
	blocks := make([]string, 0, len(plan.Blocks))
	for _, block := range plan.Blocks {
		blocks = append(blocks, block.ID)
	}
	ordered := slices.Clone(blocks)
	slices.Sort(ordered)
	if !slices.Equal(ordered, []string{
		"artifact-server-lab-artifacts", "cluster-install-sno", "cluster-media-sno", clients.BlockID,
		"dns-lab-dns", "machine-sno-01", "ntp-lab-ntp", "proxy-lab-proxy", "substrate-host-lab-libvirt",
	}) {
		t.Fatalf("blocks = %v", blocks)
	}
	// The plan's order is what makes it runnable: the image exists before a
	// node boots from it, and every node is realized before it is booted.
	for _, pair := range [][2]string{
		{"substrate-host-lab-libvirt", "machine-sno-01"},
		{"artifact-server-lab-artifacts", "cluster-media-sno"},
		{"cluster-media-sno", "cluster-install-sno"},
		{"machine-sno-01", "cluster-install-sno"},
		{"dns-lab-dns", "cluster-install-sno"},
		{"ntp-lab-ntp", "cluster-install-sno"},
	} {
		if slices.Index(blocks, pair[0]) > slices.Index(blocks, pair[1]) {
			t.Fatalf("%s is ordered after %s: %v", pair[0], pair[1], blocks)
		}
	}
}
