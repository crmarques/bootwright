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
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const labExampleFiles = 14

func labExampleSources(t *testing.T) desiredstate.Sources {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "examples", "lab-ocp"))
	if err != nil {
		t.Fatal(err)
	}
	sources, err := (inputfs.Reader{}).Read(context.Background(), []string{root})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return sources
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

func TestLabExampleSelectsControllerDependenciesFromDesiredState(t *testing.T) {
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
	selection, err := controller.Select(effective)
	if err != nil || selection.MachineName() != "controller" || !selection.ContainerRuntime() || !selection.LibvirtClient() || !selection.Route().Direct() || selection.Versions() != controller.DefaultDependencyVersions() {
		t.Fatalf("controller selection: %#v err=%v", selection, err)
	}
	tools, err := controller.SelectTools(effective)
	want := []controller.ToolRequest{
		{Kind: "helm", Version: "latest"},
		{Kind: "openshift-clients", Version: "4.21.33", Compatibility: "openshift"},
		{Kind: "openshift-install", Version: "4.21.33", Compatibility: "openshift"},
	}
	if err != nil || !slices.Equal(tools, want) {
		t.Fatalf("target tools: %#v err=%v", tools, err)
	}
	cluster := requireObject(t, effective, api.ContainerCluster, "sno")
	install := cluster.Spec().Get("install")
	if install.Get("platform", "type").Text() != "none" || install.Get("endpoints", "api", "address").Text() != "192.0.2.11" || install.Get("endpoints", "api-int", "address").Text() != "192.0.2.11" || install.Get("endpoints", "ingress", "address").Text() != "192.0.2.11" {
		t.Fatal("single-node endpoints were not resolved from the node install address", install)
	}
	if install.Get("proxy", "proxyRef").Text() != "lab-proxy" || install.Get("proxy", "endpointRef").Text() != "ip" || install.Get("ntp").Len() != 1 {
		t.Fatal("cluster installation lost its managed proxy or NTP selection", install)
	}
	node := requireObject(t, effective, api.Machine, "sno-master-01")
	if node.Spec().Get("network", "attachmentRef").Text() != "lab-network" || node.Spec().Has("hardware") || node.Spec().Has("access") {
		t.Fatal("libvirt node acquired hardware or access it must not author", node.Spec())
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
	provider := requireObject(t, effective, api.InfraProvider, "lab-libvirt")
	if provider.Spec().Get("libvirt", "bmcEmulationDefaults", "port").Text() != "8000" || provider.Spec().Get("libvirt", "machineRef").Text() != "controller" || provider.Spec().Get("networkAttachments").Items()[0].Get("libvirt", "management").Text() != "external" {
		t.Fatal("libvirt provider lost its emulated BMC defaults", provider.Spec())
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

// Present answers without an effect: the lab example's tools are installed by
// the controller stage, so preflight only reports that they are absent.
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

func TestLabExampleContextAndControllerPreparationJourney(t *testing.T) {
	services, input, ports := labContextServices(t)
	out, _ := contextRun(t, services, 0, "context", "init", "--name", "lab-ocp", "--input-dir", input)
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
	pullSecret := filepath.Join(t.TempDir(), "pull-secret.json")
	if err := os.WriteFile(pullSecret, []byte(`{"auths":{"registry.example.test":{"auth":"dXNlcjpwYXNz"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	contextRun(t, services, 0, "secret", "set", "--name", "openshift-pull-secret", "--value-file", pullSecret)
	contextRun(t, services, 0, "secret", "generate")
	contextRun(t, services, 0, "secret", "check")
	secrets, _ := contextRun(t, services, 0, "secret", "list")
	for _, name := range []string{"openshift-pull-secret", "sno-cluster-admin-ssh-key", "lab-bmc-credentials", "artifact-server-tls"} {
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
	for _, refused := range []string{"Target tools", "Controller binding", "context lab-ocp"} {
		if strings.Contains(plan, refused) {
			t.Fatalf("context-free setup planned %q:\n%s", refused, plan)
		}
	}
	// Preflight keeps the context arm: it reports what the example still needs
	// on this host, including the binding its first apply publishes.
	report, diagnostics := contextRun(t, services, 1, "preflight", "controller", "--context", "lab-ocp")
	for _, expected := range []string{"Scope       context lab-ocp", "Controller  controller", "[FAIL]  Controller binding  required controller", "[FAIL]  Target tools", "Outcome  not-ready"} {
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
// controller stage, freezing exactly the clients the graph selects and the
// libvirt requirement its provider declares. That request is what an
// operator's apply would freeze, so it must be complete before any effect.
func TestLabExamplePlansOneControllerPrerequisitesBlock(t *testing.T) {
	state, _ := compileAcceptance(t, labExampleSources(t))
	resolver := buildCapabilities(systemClock{}, localControllerDependencies(nil, processDependencies{}))
	capability, ok := resolver.Resolve(clients.Kind, clients.Implementation)
	if !ok {
		t.Fatal("this build offers no controller prerequisites capability")
	}
	contribution, err := capability.Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, State: state, Controller: "controller",
		Context: lifecycle.ContextIdentity{Name: "lab-ocp"},
	})
	if err != nil || len(contribution.Definitions) != 1 {
		t.Fatalf("contributed %d blocks (%v)", len(contribution.Definitions), err)
	}
	block := contribution.Definitions[0]
	if block.ID != clients.BlockID || block.Stage != reconciliation.StageController || block.Object != "lab-ocp" {
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
	if !request.LibvirtClient || request.Machine != "controller" || !direct {
		t.Fatalf("request = %+v", request)
	}
	kinds := make([]string, 0, len(request.Tools))
	for _, tool := range request.Tools {
		kinds = append(kinds, tool.Kind)
	}
	if !slices.Equal(kinds, []string{"helm", "openshift-clients", "openshift-install"}) {
		t.Fatalf("frozen clients = %v", kinds)
	}
}
