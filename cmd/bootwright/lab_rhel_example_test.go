//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const labExampleFiles = 14

func exampleDirectory(t *testing.T, name string) desiredstate.Sources {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	sources, err := (inputfs.Reader{}).Read(context.Background(), []string{root})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return sources
}

func labExampleSources(t *testing.T) desiredstate.Sources {
	t.Helper()
	return exampleDirectory(t, "lab-rhel")
}

func copySources(t *testing.T, sources desiredstate.Sources, destination string) {
	t.Helper()
	for _, collection := range [][]desiredstate.SourceFile{sources.Files, sources.Markers} {
		for _, file := range collection {
			relative, err := filepath.Rel(sources.Roots[0], file.Path())
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(destination, relative)
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, file.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func claimedKinds(resolver capabilityResolver) []string {
	kinds := make([]string, 0, len(resolver))
	for _, binding := range resolver.Bindings() {
		kinds = append(kinds, binding.Kind)
	}
	return kinds
}

func TestLabRHELExampleSelectsControllerDependenciesFromDesiredState(t *testing.T) {
	sources := labExampleSources(t)
	if len(sources.Files) != labExampleFiles {
		t.Fatalf("lab example discovery: got %d files, want %d", len(sources.Files), labExampleFiles)
	}
	out, errOut := contextRun(t, isolatedServices(t), 0, "validate", "-f", sources.Roots[0], "--output", "json")
	var validation struct {
		OK          bool
		Result      struct{ Counts compilation.Counts }
		Diagnostics []any
	}
	if err := json.Unmarshal([]byte(out), &validation); err != nil || !validation.OK || errOut != "" || len(validation.Diagnostics) != 0 || validation.Result.Counts != (compilation.Counts{FilesSeen: labExampleFiles, ObjectsDecoded: labExampleFiles}) {
		t.Fatalf("lab example admission: out=%s err=%s %v", out, errOut, err)
	}
	state, _ := compileAcceptance(t, sources)
	effective := state.Effective()
	// The provider host is the controller, so the libvirt client the substrate
	// needs is selected by this graph rather than by the baseline setup.
	selection, err := controller.Select(effective)
	if err != nil || selection.MachineName() != "controller" || !selection.ContainerRuntime() || !selection.LibvirtClient() || !selection.Route().Direct() {
		t.Fatalf("controller selection: %#v err=%v", selection, err)
	}
	tools, err := controller.SelectTools(effective)
	if err != nil || len(tools) != 0 {
		t.Fatalf("a graph without a container cluster selected target tools: %#v %v", tools, err)
	}
	for _, object := range []struct {
		kind api.Kind
		name string
	}{
		{api.Machine, "controller"}, {api.Machine, "rhel-01"},
		{api.InfraProvider, "lab-libvirt"}, {api.NetworkConfig, "lab-guests"},
		{api.MachineImage, "rhel-9-7-boot"}, {api.MachineInstallProfile, "rhel-9-7"},
		{api.Proxy, "lab-proxy"}, {api.DNSServer, "lab-dns"},
		{api.NTPServer, "lab-ntp"}, {api.ArtifactServer, "lab-artifacts"},
	} {
		requireObject(t, effective, object.kind, object.name)
	}
	provider := requireObject(t, effective, api.InfraProvider, "lab-libvirt")
	libvirt := provider.Spec().Get("libvirt")
	if libvirt.Get("machineRef").Text() != "controller" || libvirt.Get("bmcEmulationDefaults", "port").Text() != "8000" || libvirt.Get("bmcEmulationDefaults", "bindAddress").Text() != "192.0.2.1" {
		t.Fatal("libvirt provider lost its emulated BMC defaults", provider.Spec())
	}
	attachment := provider.Spec().Get("networkAttachments").Items()[0].Get("libvirt")
	if attachment.Get("management").Text() != "managed" || attachment.Get("address").Text() != "198.51.100.1/24" || attachment.Get("forward").Text() != "nat" {
		t.Fatal("the managed attachment lost the network Bootwright defines", attachment)
	}
	guest := requireObject(t, effective, api.Machine, "rhel-01")
	if guest.Spec().Get("network", "attachmentRef").Text() != "lab-guests" || guest.Spec().Has("hardware") {
		t.Fatal("the installed guest acquired hardware it must not author", guest.Spec())
	}
	// A Bootwright-installed Machine authors no access; the fleet account and
	// key the install authorizes are derived, and nothing else reaches it.
	if requireObject(t, state.Authored(), api.Machine, "rhel-01").Spec().Has("access") {
		t.Fatal("the installed guest authored its own access")
	}
	access := guest.Spec().Get("access", "ssh")
	if access.Get("user").Text() != "bootwright" || access.Get("auth", "privateKeyRef").Text() != "bootwright-machine-key" {
		t.Fatal("the installed guest did not derive the fleet account", access)
	}
	anaconda := requireObject(t, effective, api.MachineInstallProfile, "rhel-9-7").Spec().Get("installer", "anaconda")
	if anaconda.Get("imageRef").Text() != "rhel-9-7-boot" || !anaconda.Has("packageSource", "hostedTree") || anaconda.Has("packageSource", "fromSubscription") {
		t.Fatal("the install profile is not the secret-free hosted-tree shape", anaconda)
	}
	for _, service := range []struct {
		kind api.Kind
		name string
	}{{api.Proxy, "lab-proxy"}, {api.DNSServer, "lab-dns"}, {api.NTPServer, "lab-ntp"}, {api.ArtifactServer, "lab-artifacts"}} {
		object := requireObject(t, effective, service.kind, service.name)
		if object.Spec().Get("management").Text() != "managed" || object.Spec().Get("machineRef").Text() != "controller" || object.Spec().Get("bindAddress").Text() != "192.0.2.1" {
			t.Fatalf("%s/%s is not a managed controller service", service.kind, service.name)
		}
	}
}

// The substrate and managed-OS capabilities are not in this build yet, so an
// operation must name the provider and the guest it cannot realize rather than
// apply the services and leave the rest silently undone.
func TestLabRHELExampleNamesTheKindsThisBuildCannotRealize(t *testing.T) {
	state, _ := compileAcceptance(t, labExampleSources(t))
	resolver := buildCapabilities(systemClock{}, localControllerDependencies(nil, processDependencies{}))
	unsupported := lifecycle.Unrealizable(state.Effective(), claimedKinds(resolver))
	if !slices.Equal(unsupported, []string{"InfraProvider/lab-libvirt", "Machine/rhel-01"}) {
		t.Fatalf("unsupported = %v", unsupported)
	}
	for _, realizable := range []string{
		"ArtifactServer/lab-artifacts", "DNSServer/lab-dns", "NTPServer/lab-ntp", "Proxy/lab-proxy",
	} {
		if slices.Contains(unsupported, realizable) {
			t.Fatalf("%s was reported unsupported", realizable)
		}
	}
}

// Each capability must derive a complete frozen request from the example
// alone, because that request is what an operator's apply would freeze.
func TestLabRHELExamplePlansOneBlockPerService(t *testing.T) {
	state, _ := compileAcceptance(t, labExampleSources(t))
	resolver := buildCapabilities(systemClock{}, localControllerDependencies(nil, processDependencies{}))
	input := lifecycle.PlanInput{
		Verb: reconciliation.Apply, State: state, Controller: "controller",
		Context: lifecycle.ContextIdentity{Name: "lab-rhel"},
	}
	var definitions []reconciliation.BlockDefinition
	reservations := map[string][]string{}
	for _, binding := range resolver.Bindings() {
		capability, ok := resolver.Resolve(binding.Kind, binding.Implementation)
		if !ok {
			t.Fatalf("%s/%s does not resolve", binding.Kind, binding.Implementation)
		}
		contribution, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatalf("%s plan: %v", binding.Kind, err)
		}
		definitions = append(definitions, contribution.Definitions...)
		for _, reservation := range contribution.Reservations {
			if reservation.Context != "lab-rhel" {
				t.Fatalf("%s reserved for %q", binding.Kind, reservation.Context)
			}
			reservations[reservation.Service] = reservation.Keys
		}
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal("the example produced a block the plan model refuses:", err)
	}
	blocks := []string{}
	for _, block := range plan.Blocks {
		blocks = append(blocks, block.ID)
		if len(block.Consumes) != 0 {
			t.Fatalf("%s consumes authorization %v for an apply that destroys nothing", block.ID, block.Consumes)
		}
	}
	slices.Sort(blocks)
	if !slices.Equal(blocks, []string{
		"artifact-server-lab-artifacts", clients.BlockID, "dns-lab-dns", "ntp-lab-ntp", "proxy-lab-proxy",
	}) {
		t.Fatalf("blocks = %v", blocks)
	}
	for service, want := range map[string][]string{
		"lab-proxy":     {"socket:192.0.2.1:3128"},
		"lab-dns":       {"socket:192.0.2.1:53"},
		"lab-ntp":       {"socket:192.0.2.1:123"},
		"lab-artifacts": {"socket:192.0.2.1:8443", "socket:192.0.2.1:8080"},
	} {
		for _, key := range want {
			if !slices.Contains(reservations[service], key) {
				t.Fatalf("%s reserved %v, missing %q", service, reservations[service], key)
			}
		}
	}
}

// The derived configuration is what the adapter renders, so the frozen request
// must already carry the graph's own clients, records and upstream sources.
func TestLabRHELRequestsCarryTheirDerivedIntent(t *testing.T) {
	state, _ := compileAcceptance(t, labExampleSources(t))
	resolver := buildCapabilities(systemClock{}, localControllerDependencies(nil, processDependencies{}))
	requests := map[string]managedservice.Request{}
	for kind, implementation := range map[string]string{
		"Proxy": "proxy-squid-v1", "DNSServer": "dns-server-dnsmasq-v1", "NTPServer": "ntp-server-chrony-v1",
	} {
		capability, ok := resolver.Resolve(kind, implementation)
		if !ok {
			t.Fatalf("%s/%s does not resolve", kind, implementation)
		}
		contribution, err := capability.Plan(context.Background(), lifecycle.PlanInput{
			Verb: reconciliation.Apply, State: state, Controller: "controller",
			Context: lifecycle.ContextIdentity{Name: "lab-rhel"},
		})
		if err != nil || len(contribution.Definitions) != 1 {
			t.Fatalf("%s contributed %d blocks (%v)", kind, len(contribution.Definitions), err)
		}
		request, err := managedservice.DecodeRequest(contribution.Definitions[0].Request, implementation)
		if err != nil {
			t.Fatalf("%s request: %v", kind, err)
		}
		requests[kind] = request
	}
	// The guest network reaches the managed services, so the graph's own CIDRs
	// and declared addresses are what the frozen access lists carry.
	clients := []string{"127.0.0.1/32", "192.0.2.1/32", "198.51.100.0/24", "198.51.100.11/32", "::1/128"}
	if !slices.Equal(requests["Proxy"].Clients, clients) || !slices.Equal(requests["NTPServer"].Clients, clients) {
		t.Fatalf("clients = %v and %v", requests["Proxy"].Clients, requests["NTPServer"].Clients)
	}
	records := requests["DNSServer"].Records
	if len(records) != 2 || records[0].Name != "controller.lab.example.test" || records[1].Name != "rhel-01.lab.example.test" {
		t.Fatalf("records = %+v", records)
	}
	if !slices.Equal(records[1].Addresses, []string{"198.51.100.11"}) {
		t.Fatalf("the installed guest record = %+v", records[1])
	}
	if !slices.Equal(requests["DNSServer"].Forwarders, []string{"192.0.2.53"}) {
		t.Fatalf("forwarders = %v", requests["DNSServer"].Forwarders)
	}
	if !slices.Equal(requests["NTPServer"].Sources, []string{"192.0.2.123"}) {
		t.Fatalf("sources = %v", requests["NTPServer"].Sources)
	}
	for kind, request := range requests {
		if request.Placement.Connection != "local" || request.Placement.Machine != "controller" {
			t.Fatalf("%s placement = %+v", kind, request.Placement)
		}
		if !strings.Contains(request.Image, "@sha256:") {
			t.Fatalf("%s image is not digest pinned: %q", kind, request.Image)
		}
	}
}

type labControllerPorts struct {
	identity controller.InstalledHostIdentity
	effects  int
}

func (p *labControllerPorts) Platform(context.Context) (prerequisites.Platform, error) {
	return prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}, nil
}

func (p *labControllerPorts) Identity(context.Context) (controller.InstalledHostIdentity, error) {
	return p.identity, nil
}

func (p *labControllerPorts) Runtime(context.Context, prerequisites.RuntimeRequirement) (prerequisites.RuntimeInspection, error) {
	return prerequisites.RuntimeInspection{Present: true, Ready: false}, nil
}

// Setup admits the context-independent closure only. The lab example libvirt
// client is selected by its desired state, so it reaches the controller stage
// rather than this catalog.
func (p *labControllerPorts) Select(_ prerequisites.Platform, requirements prerequisites.NativeRequirements) (prerequisites.Definition, error) {
	if !requirements.ContainerRuntime || requirements.LibvirtClient {
		return prerequisites.Definition{}, errors.New("setup must select the container runtime without the libvirt client")
	}
	return prerequisites.Definition{CatalogDigest: strings.Repeat("a", 64), PythonVersion: "3.13.15", AnsibleVersion: "2.21.4", Runtime: prerequisites.RuntimeRequirement{Version: "5.8.4"}}, nil
}

func (p *labControllerPorts) ValidateEgress(egress prerequisites.SetupEgress) error {
	if egress.HTTPProxy != "" || egress.HTTPSProxy != "" {
		return errors.New("direct controller egress selected a proxy")
	}
	return nil
}

func (p *labControllerPorts) Inspect(context.Context, prerequisites.BundleArea, prerequisites.Definition, bool) (prerequisites.BundleInspection, error) {
	return prerequisites.BundleInspection{Recoverable: true}, nil
}

func (p *labControllerPorts) Prepare(context.Context, prerequisites.BundleArea, prerequisites.Definition, prerequisites.SetupEgress, func(prerequisites.ProgressEvent)) error {
	p.effects++
	return errors.New("unexpected bundle preparation")
}

type labToolCatalog struct{ ports *labControllerPorts }

func (c labToolCatalog) Select(requests []controller.ToolRequest, _ []prerequisites.DependencySource) ([]prerequisites.ToolDefinition, bool, error) {
	return []prerequisites.ToolDefinition{}, len(requests) == 0, nil
}

func (c labToolCatalog) Resolve(context.Context, []controller.ToolRequest, prerequisites.SetupEgress) ([]prerequisites.ToolDefinition, error) {
	c.ports.effects++
	return nil, errors.New("unexpected publisher metadata resolution")
}

// Present answers without an effect: this example selects no target tool, so
// preflight reports only the closure the controller stage still installs.
func (c labToolCatalog) Present(context.Context, prerequisites.BundleArea, []prerequisites.ToolDefinition) (bool, error) {
	return false, nil
}

func labContextServices(t *testing.T) (cli.Services, string, *labControllerPorts) {
	t.Helper()
	parent := t.TempDir()
	input := filepath.Join(parent, "input")
	root := filepath.Join(parent, "state")
	copySources(t, labExampleSources(t), input)
	repository := testRepository(root)
	ports := &labControllerPorts{}
	var err error
	ports.identity, err = controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, strings.Repeat("1", 32), "11111111-2222-4333-8444-555555555555", "22222222-3333-4444-8555-666666666666")
	if err != nil {
		t.Fatal(err)
	}
	options := testContextWiring(t, root)
	options.Repository, options.Workspace = repository, repository
	options.Controller = controllerDependencies{Storage: repository, Host: ports, Catalog: ports, Bundle: ports, Tools: labToolCatalog{ports}}
	return assembleServices(options), input, ports
}

func TestLabRHELExampleContextAndControllerPreparationJourney(t *testing.T) {
	services, input, ports := labContextServices(t)
	out, _ := contextRun(t, services, 0, "context", "init", "--name", "lab-rhel", "--input-dir", input)
	if !strings.Contains(out, "Files copied     14") || !strings.Contains(out, "Objects decoded  14") {
		t.Fatal("context import did not admit the complete lab example", out)
	}
	if err := os.RemoveAll(input); err != nil {
		t.Fatal(err)
	}
	contextRun(t, services, 0, "validate")
	rendered, _ := contextRun(t, services, 0, "render", "effective", "--output", "json")
	var effective struct {
		Result struct{ EffectiveState []json.RawMessage }
	}
	if err := json.Unmarshal([]byte(rendered), &effective); err != nil || len(effective.Result.EffectiveState) != labExampleFiles {
		t.Fatalf("frozen lab input did not render completely: count=%d err=%v", len(effective.Result.EffectiveState), err)
	}
	contextRun(t, services, 1, "secret", "check")
	contextRun(t, services, 0, "secret", "generate")
	contextRun(t, services, 0, "secret", "check")
	secrets, _ := contextRun(t, services, 0, "secret", "list")
	for _, name := range []string{"artifact-server-tls", "bootwright-machine-key", "lab-bmc-credentials"} {
		if !strings.Contains(secrets, name) {
			t.Fatalf("secret %s is not listed after custody:\n%s", name, secrets)
		}
	}
	// Setup plans the same context-independent prerequisites whatever context
	// is selected, so its dry run names the baseline and neither the example's
	// target tools nor its host binding.
	plan, _ := contextRun(t, services, 0, "setup", "--dry-run")
	for _, expected := range []string{"Scope     baseline", "[UNKNOWN]  Container runtime", "Outcome  planned", "Next     bootwright setup"} {
		if !strings.Contains(plan, expected) {
			t.Fatalf("baseline dry-run lacks %q:\n%s", expected, plan)
		}
	}
	for _, refused := range []string{"Target tools", "Controller binding", "context lab-rhel"} {
		if strings.Contains(plan, refused) {
			t.Fatalf("context-free setup planned %q:\n%s", refused, plan)
		}
	}
	// Preflight keeps the context arm: it reports what the example still needs
	// on this host, including the binding its first apply publishes.
	report, diagnostics := contextRun(t, services, 1, "preflight", "controller", "--context", "lab-rhel")
	for _, expected := range []string{"Scope       context lab-rhel", "Controller  controller", "[FAIL]  Controller binding  required controller", "Outcome  not-ready"} {
		if !strings.Contains(report, expected) {
			t.Fatalf("context preflight lacks %q:\n%s", expected, report)
		}
	}
	// The host itself is unprepared, so the one actionable next command is
	// setup rather than the context's own controller stage.
	if !strings.Contains(diagnostics, "preflight.failed") || !strings.Contains(diagnostics, "run bootwright setup") {
		t.Fatalf("preflight before setup must fail with setup guidance:\nstdout=%s\nstderr=%s", report, diagnostics)
	}
	_, diagnostics = contextRun(t, services, 1, "preflight", "controller", "--context", "missing")
	if !strings.Contains(diagnostics, "context") {
		t.Fatal("unknown explicit context was not refused", diagnostics)
	}
	if ports.effects != 0 {
		t.Fatalf("dry-run and preflight performed %d setup effects", ports.effects)
	}
}

// The controller stage is planned from the example alone: one block, in the
// controller stage, freezing the libvirt requirement its provider declares.
// That request is what an operator's apply would freeze, so it must be
// complete before any effect.
func TestLabRHELExamplePlansOneControllerPrerequisitesBlock(t *testing.T) {
	state, _ := compileAcceptance(t, labExampleSources(t))
	resolver := buildCapabilities(systemClock{}, localControllerDependencies(nil, processDependencies{}))
	capability, ok := resolver.Resolve(clients.Kind, clients.Implementation)
	if !ok {
		t.Fatal("this build offers no controller prerequisites capability")
	}
	contribution, err := capability.Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, State: state, Controller: "controller",
		Context: lifecycle.ContextIdentity{Name: "lab-rhel"},
	})
	if err != nil || len(contribution.Definitions) != 1 {
		t.Fatalf("contributed %d blocks (%v)", len(contribution.Definitions), err)
	}
	block := contribution.Definitions[0]
	if block.ID != clients.BlockID || block.Stage != reconciliation.StageController || block.Object != "lab-rhel" {
		t.Fatalf("block = %+v", block)
	}
	if len(contribution.Reservations) != 0 || len(contribution.Secrets) != 0 {
		t.Fatal("the controller stage claimed a host resource or Secret")
	}
	request, err := clients.DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	direct := request.Egress.HTTPProxy == "" && request.Egress.HTTPSProxy == "" && len(request.Egress.NoProxy) == 0
	if !request.LibvirtClient || request.Machine != "controller" || !direct || len(request.Tools) != 0 {
		t.Fatalf("request = %+v", request)
	}
}

// A context-free journey must still refuse before touching the store, because
// an unavailable target is not a reason to read privileged state.
func TestLifecycleCommandsRequireAResolvedContext(t *testing.T) {
	services := isolatedServices(t)
	for _, args := range [][]string{
		{"plan", "--context", "missing"},
		{"status", "--context", "missing"},
		{"apply", "--context", "missing", "--yes"},
		{"destroy", "--context", "missing", "--yes"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, errOut := contextRun(t, services, 1, args...)
			if errOut == "" {
				t.Fatal("an unresolved context produced no diagnostic")
			}
		})
	}
}

// A lifecycle command must reach the real store and refuse there, rather than
// reporting an unavailable capability for a context that does not exist.
func TestLifecycleReachesTheStoreForAnUnknownContext(t *testing.T) {
	services := isolatedServices(t)
	_, errOut := contextRun(t, services, 1, "plan", "--context", "missing")
	if !strings.Contains(errOut, "context.state") {
		t.Fatalf("plan for an unknown context = %q", errOut)
	}
}
