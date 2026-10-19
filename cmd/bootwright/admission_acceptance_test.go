package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	effectiveencoding "github.com/crmarques/bootwright/internal/desiredstate/encoding"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func exampleSources(t *testing.T) desiredstate.Sources {
	t.Helper()
	return exampleDirectory(t, "multidc-platform")
}

func exampleDirectory(t *testing.T, name string) desiredstate.Sources {
	t.Helper()
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("filesystem admission is qualified for linux/amd64")
	}
	root, err := filepath.Abs(examplePath(t, name))
	if err != nil {
		t.Fatal(err)
	}
	sources, err := (inputfs.Reader{}).Read(context.Background(), []string{root})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return sources
}

func examplePath(t *testing.T, name string) string {
	t.Helper()
	path, ok := exampleDirectories(t)[name]
	if !ok {
		t.Fatalf("no categorized example named %q", name)
	}
	return path
}

// Discover tracked scenarios without entering ignored or untracked local input.
func exampleDirectories(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..")
	command := exec.Command("git", "ls-files", "-z", "--", "examples")
	command.Dir = root
	command.Env = slices.DeleteFunc(os.Environ(), func(value string) bool { return strings.HasPrefix(value, "GIT_") })
	output, err := command.Output()
	if err != nil {
		t.Fatalf("list tracked examples: %v", err)
	}
	examples := map[string]string{}
	tracked := map[string]bool{}
	for name := range strings.SplitSeq(string(output), "\x00") {
		tracked[name] = true
		parts := strings.Split(name, "/")
		if len(parts) < 4 || parts[1] == "wip" {
			continue
		}
		directory := filepath.Join(root, filepath.FromSlash(strings.Join(parts[:3], "/")))
		if previous, exists := examples[parts[2]]; exists && previous != directory {
			t.Fatalf("duplicate example name %q in %s and %s", parts[2], previous, directory)
		}
		examples[parts[2]] = directory
	}
	for _, directory := range examples {
		relative, err := filepath.Rel(root, directory)
		if err != nil || !tracked[filepath.ToSlash(filepath.Join(relative, "environment.yaml"))] {
			t.Fatalf("example %s needs a tracked environment.yaml file", directory)
		}
	}
	if len(examples) == 0 {
		t.Fatal("no categorized example was found under examples/")
	}
	return examples
}

func readAcceptanceFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "admission", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func expandedExampleSources(t *testing.T) desiredstate.Sources {
	t.Helper()
	sources := exampleSources(t)
	sources.Files = append(sources.Files, desiredstate.NewSourceFile(filepath.Join(sources.Roots[0], "acceptance-extras.yaml"), readAcceptanceFixture(t, "extras.yaml")))
	return sources
}

// refusedKinds are the kinds the reference and the extras declare that
// admission refuses until their later slices (storage family refusals): the
// admissible projection cannot carry one, and no other kind may be missing
// from it.
var refusedKinds = []api.Kind{api.StorageExport}

const (
	fusionAddOn           = "fusion-data-foundation"
	admissibleCephSources = "acceptance-ceph.yaml"
)

// mustReplaceOnce rewrites body and fails the test when the text it edits is
// gone, so a changed reference cannot silently stop being projected.
func mustReplaceOnce(t *testing.T, path, body, old, replacement string) string {
	t.Helper()
	if strings.Count(body, old) != 1 {
		t.Fatalf("%s: the projection expects %q exactly once", path, old)
	}
	return strings.Replace(body, old, replacement, 1)
}

// admissibleExampleSources is the reference as admission accepts it today:
// the Ceph sources and the add-on that can only consume a refused export
// dropped (the hub profile keeps the add-on every cluster profile shares), a
// non-stretch StorageCluster with its policy and pool substituted from
// testdata, and the extras without the refused families. The public example is
// untouched; its own refusals are pinned by referenceRefusals.
func admissibleExampleSources(t *testing.T, withExtras bool) desiredstate.Sources {
	t.Helper()
	sources := exampleSources(t)
	root := sources.Roots[0]
	ceph := filepath.Join(root, "clusters", "ceph") + string(filepath.Separator)
	addOn := filepath.Join(root, "add-ons", fusionAddOn) + string(filepath.Separator)
	var files []desiredstate.SourceFile
	dropped := 0
	for _, file := range sources.Files {
		body := string(file.Bytes())
		switch {
		case strings.HasPrefix(file.Path(), ceph), strings.HasPrefix(file.Path(), addOn):
			dropped++
			continue
		case strings.Contains(body, "kind: ClusterAddonProfile\n"):
			replacement := ""
			if strings.Contains(body, "  name: hub-cluster-addons\n") {
				replacement = "    - nmstate\n"
			}
			body = mustReplaceOnce(t, file.Path(), body, "    - "+fusionAddOn+"\n", replacement)
		case strings.Contains(body, "kind: ClusterAddonBinding\n"):
			head, _, found := strings.Cut(body, "  addonConfigs:\n")
			if !found || !strings.Contains(body, "addonRef: "+fusionAddOn+"\n") {
				t.Fatalf("%s: the projection expects the %s input block", file.Path(), fusionAddOn)
			}
			body = head
		default:
			files = append(files, file)
			continue
		}
		files = append(files, desiredstate.NewSourceFile(file.Path(), []byte(body)))
	}
	if dropped != 15+1 {
		t.Fatalf("the projection dropped %d sources, want the 15 Ceph sources and the add-on", dropped)
	}
	files = append(files, desiredstate.NewSourceFile(filepath.Join(root, admissibleCephSources), readAcceptanceFixture(t, "ceph-admissible.yaml")))
	if withExtras {
		files = append(files, desiredstate.NewSourceFile(filepath.Join(root, "acceptance-extras.yaml"), admissibleExtras(t)))
	}
	sources.Files = files
	return sources
}

// admissibleExtras is extras.yaml without its documents of refused kinds.
func admissibleExtras(t *testing.T) []byte {
	t.Helper()
	var kept []string
	removed := 0
	for _, document := range strings.Split(string(readAcceptanceFixture(t, "extras.yaml")), "\n---\n") {
		refused := false
		for _, kind := range refusedKinds {
			refused = refused || strings.Contains(document, "kind: "+string(kind)+"\n")
		}
		if refused {
			removed++
			continue
		}
		kept = append(kept, document)
	}
	if removed != 1 {
		t.Fatalf("the extras hold %d refused documents, want the external export", removed)
	}
	return []byte(strings.Join(kept, "\n---\n"))
}

func compileAcceptance(t *testing.T, sources desiredstate.Sources) (*compilation.State, *compilation.Report) {
	t.Helper()
	state, report, err := wireCompiler().Compile(context.Background(), sources)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return state, report
}

func requireObject(t *testing.T, catalog api.Catalog, kind api.Kind, name string) api.Object {
	t.Helper()
	object, ok := catalog.Find(kind, name)
	if !ok {
		t.Fatalf("missing %s/%s", kind, name)
	}
	return object
}

// The reference includes later service families and stretch intent. Admission
// refuses those declarations explicitly rather than dropping them from input.
func TestActualCLIRefusesFutureReferenceFeatures(t *testing.T) {
	sources := exampleSources(t)
	if len(sources.Files) != 106 {
		t.Fatalf("reference discovery: got %d files, want 106", len(sources.Files))
	}
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"validate", "-f", sources.Roots[0], "--output", "json"}, &out, &errOut)
	var result struct {
		OK          bool
		Diagnostics []diagnostics.Diagnostic
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err, out.String())
	}
	if code == 0 || errOut.Len() != 0 || result.OK {
		t.Fatalf("reference admission: code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	requireReferenceRefusals(t, result.Diagnostics, false)
}

// diagnosticTuple names one diagnostic by what an author reads: its code, the
// object it is about and the field it points at.
type diagnosticTuple struct{ code, kind, name, field string }

func (d diagnosticTuple) String() string {
	return strings.Join([]string{d.code, d.kind, d.name, d.field}, " | ")
}

const (
	laterServiceRefusal = "$.spec"
	storageClusterKind  = string(api.StorageCluster)
	multidcCluster      = "ceph-multidc-01"
)

// referenceRefusals is the complete set of diagnostics the unmodified
// reference raises: the managed Ceph cluster's per-field qualification
// refusals and its stretch intent, and each storage family refused until its
// later slice. A diagnostic outside the set, or a missing one, fails.
func referenceRefusals() []diagnosticTuple {
	cluster := func(field string) diagnosticTuple {
		return diagnosticTuple{"api.invariant", storageClusterKind, multidcCluster, field}
	}
	family := func(kind, name string) diagnosticTuple {
		return diagnosticTuple{"api.invariant", kind, name, laterServiceRefusal}
	}
	return []diagnosticTuple{
		cluster("$.spec.ceph.cephadm.workarounds"),
		cluster("$.spec.ceph.config.global.mon_osd_down_out_subtree_limit"),
		cluster("$.spec.ceph.config.global.osd_pool_default_pg_autoscale_mode"),
		{"api.required", storageClusterKind, multidcCluster, "$.spec.ceph.config.global.osd_pool_default_min_size"},
		{"api.required", storageClusterKind, multidcCluster, "$.spec.ceph.config.global.osd_pool_default_size"},
		cluster("$.spec.ceph.mgrModules"),
		cluster("$.spec.ceph.topology.stretch"),
		cluster("$.spec.ceph.services[3].service_type"),
		cluster("$.spec.ceph.services[4].service_type"),
		cluster("$.spec.ceph.services[5].service_type"),
		cluster("$.spec.ceph.serviceBindings[0].secrets[0].path"),
		family("StorageExport", "ceph-multidc-01-odf-hub-multidc-01"),
		family("StorageExport", "ceph-multidc-01-odf-hub-multidc-02"),
		family("StorageExport", "ceph-multidc-01-odf-ocp-multidc-01"),
		family("StorageExport", "ceph-multidc-01-odf-ocp-multidc-02")}
}

// expandedRefusals adds what the acceptance extras contribute to the
// reference's: one more refused storage family, and the warning every
// CustomPlaybook raises because it cannot execute.
func expandedRefusals() []diagnosticTuple {
	return append(referenceRefusals(),
		diagnosticTuple{"api.invariant", "StorageExport", "acceptance-external-export", laterServiceRefusal},
		diagnosticTuple{"api.deferred", "CustomPlaybook", "acceptance-playbook", laterServiceRefusal},
	)
}

func requireReferenceRefusals(t *testing.T, sink []diagnostics.Diagnostic, expanded bool) {
	t.Helper()
	want := referenceRefusals()
	if expanded {
		want = expandedRefusals()
	}
	missing := map[diagnosticTuple]int{}
	for _, tuple := range want {
		missing[tuple]++
	}
	for _, d := range sink {
		tuple := diagnosticTuple{code: d.Code, field: d.Field}
		if d.Object != nil {
			tuple.kind, tuple.name = d.Object.Kind, d.Object.Name
		}
		if missing[tuple] == 0 {
			t.Errorf("unexpected diagnostic outside the reference's refusal set: %s: %s", tuple, d.Message)
			continue
		}
		missing[tuple]--
	}
	for tuple, count := range missing {
		if count != 0 {
			t.Errorf("expected refusal is missing: %s", tuple)
		}
	}
}

// Target tools are selected by the graph alone: the clusters and providers this
// supported example declares are what pull the OpenShift clients in, at the
// versions it declares, and nothing else contributes one.
func TestActualExampleSelectsTargetToolsFromItsClusters(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "lab-sno"))
	tools, err := controller.SelectTools(state.Effective())
	want := []controller.ToolRequest{
		{Kind: "helm", Version: "latest"},
		{Kind: "openshift-clients", Version: "4.21.15", Compatibility: "openshift"},
		{Kind: "openshift-install", Version: "4.21.15", Compatibility: "openshift"},
	}
	if err != nil || !slices.Equal(tools, want) {
		t.Fatalf("target tools: %#v err=%v", tools, err)
	}
}

func TestActualCompilerRefusesFutureKindsAndAdditionalVariants(t *testing.T) {
	sources := expandedExampleSources(t)
	state, report, err := wireCompiler().Compile(context.Background(), sources)
	if err == nil || state != nil || report != nil {
		t.Fatal("future reference unexpectedly compiled")
	}
	requireReferenceRefusals(t, diagnostics.Of(err), true)
	if len(api.Kinds()) != 29 {
		t.Fatalf("kind catalog changed: %d", len(api.Kinds()))
	}
}

// The projection is the full-wiring positive coverage the reference cannot
// give while it declares refused families: every kind but the refused ones
// reaches the effective graph from one admitted stream. A kind added to the
// catalog without coverage, or a family admitted without leaving the
// allow-list, fails here.
func TestActualAdmissibleProjectionAdmitsEveryKindButTheRefusedFamilies(t *testing.T) {
	state, report := compileAcceptance(t, admissibleExampleSources(t, true))
	kinds := api.Kinds()
	if len(kinds) != 29 {
		t.Fatalf("kind catalog changed: %d; cover the new kind in the reference or the extras", len(kinds))
	}
	for _, kind := range kinds {
		covered := len(state.Effective().OfKind(kind)) != 0
		switch refused := slices.Contains(refusedKinds, kind); {
		case refused && covered:
			t.Errorf("kind %s is admitted now: remove it from refusedKinds and cover it", kind)
		case !refused && !covered:
			t.Errorf("kind %s has no full-wiring positive admission coverage", kind)
		}
	}
	if report.Counts != (compilation.Counts{FilesSeen: 92, ObjectsDecoded: 121}) {
		t.Errorf("projection counts: %#v", report.Counts)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "api.deferred" || report.Diagnostics[0].Object == nil || report.Diagnostics[0].Object.Name != "acceptance-playbook" {
		t.Errorf("the projection raised a diagnostic besides the reserved playbook's: %+v", report.Diagnostics)
	}
	installed := requireObject(t, state.Effective(), api.Machine, "storage-a-01")
	if installed.Spec().Get("access", "ssh", "user").Text() != "bootwright" || installed.Spec().Get("access", "ssh", "auth", "privateKeyRef").Text() != "bootwright-machine-key" {
		t.Fatal("installed Machine access was not derived")
	}
	if requireObject(t, state.Authored(), api.Machine, "storage-a-01").Spec().Has("access") {
		t.Fatal("authored Machine acquired effective access")
	}
}

func TestActualAdmissibleProjectionNormalizesAndDefaultsItsExtras(t *testing.T) {
	state, _ := compileAcceptance(t, admissibleExampleSources(t, true))
	playbook := requireObject(t, state.Effective(), api.CustomPlaybook, "acceptance-playbook")
	if playbook.Spec().Get("enabled").Bool() || playbook.Spec().Get("extraVars", "literal").Type() != api.String || playbook.Spec().Get("extraVars", "fraction").Type() != api.Number {
		t.Fatal("reserved playbook declaration changed")
	}
	libvirt := requireObject(t, state.Effective(), api.InfraProvider, "acceptance-libvirt")
	if libvirt.Spec().Get("libvirt", "bmcEmulationDefaults", "port").Text() != "8000" || libvirt.Spec().Get("libvirt", "bmcEmulationDefaults", "emulator").Text() != "sushy-tools" {
		t.Fatal("libvirt emulated BMC defaults missing")
	}
	if libvirt.Spec().Get("libvirt", "bmcEmulationDefaults", "bindAddress").Text() != "192.0.2.60" {
		t.Fatal("the emulated BMC's authored listener address changed")
	}
	vsphere := requireObject(t, state.Effective(), api.InfraProvider, "acceptance-vsphere")
	if vsphere.Spec().Get("vsphere", "machineProfiles").Items()[0].Get("failureDomainRef").Text() != "example-zone" {
		t.Fatal("vSphere domain default missing")
	}
	clone := requireObject(t, state.Effective(), api.MachineInstallProfile, "acceptance-clone")
	if !clone.Spec().Get("installer", "templateClone", "seed", "cloudInit", "growRootFilesystem").Bool() {
		t.Fatal("clone seed default missing")
	}
	for _, service := range []struct {
		kind       api.Kind
		name, port string
	}{{api.Proxy, "acceptance-proxy", "3128"}, {api.DNSServer, "acceptance-dns", "53"}, {api.NTPServer, "acceptance-ntp", "123"}, {api.Registry, "acceptance-registry", "5000"}} {
		component := requireObject(t, state.Effective(), service.kind, service.name)
		if component.Spec().Get("port").Text() != service.port {
			t.Errorf("service %s missing default port %s", service.name, service.port)
		}
	}
	if requireObject(t, state.Effective(), api.Entitlement, "acceptance-redhat-ceph").Spec().Get("rhsm").Has("connectToInsights") {
		t.Fatal("external RHSM acquired managed intent")
	}
	if requireObject(t, state.Effective(), api.StorageCluster, "acceptance-external-storage").Spec().Has("ceph") {
		t.Fatal("external storage acquired managed configuration")
	}
	sno := requireObject(t, state.Effective(), api.ContainerCluster, "acceptance-sno")
	if sno.Spec().Get("install", "endpoints", "api", "address").Text() != "192.0.2.220" || sno.Spec().Get("install", "platform", "type").Text() != "none" {
		t.Fatal("single-node endpoint/platform derivation missing")
	}
}

// The graph alone selects the target tools of the reference's clusters and
// providers, KubeVirt's client included. The extras are left out: their
// single-node cluster pins a release image, which tool selection refuses.
func TestActualAdmissibleProjectionSelectsKubeVirtTools(t *testing.T) {
	state, _ := compileAcceptance(t, admissibleExampleSources(t, false))
	tools, err := controller.SelectTools(state.Effective())
	want := []controller.ToolRequest{
		{Kind: "helm", Version: "latest"},
		{Kind: "openshift-clients", Version: "4.21.15", Compatibility: "openshift"},
		{Kind: "openshift-install", Version: "4.21.15", Compatibility: "openshift"},
		{Kind: "virtctl", Version: "latest", Compatibility: "kubevirt"},
	}
	if err != nil || !slices.Equal(tools, want) {
		t.Fatalf("target tools: %#v err=%v", tools, diagnostics.Of(err))
	}
}

func TestActualCompilerSourceOrderIndependence(t *testing.T) {
	sources := exampleDirectory(t, "lab-rhel")
	before := make([][]byte, len(sources.Files))
	for i, file := range sources.Files {
		before[i] = file.Bytes()
	}
	state, report := compileAcceptance(t, sources)
	canonical, err := effectiveencoding.JSON(context.Background(), state.Effective())
	if err != nil {
		t.Fatal(err)
	}
	reversed := sources
	reversed.Files = slices.Clone(sources.Files)
	slices.Reverse(reversed.Files)
	again, againReport := compileAcceptance(t, reversed)
	other, err := effectiveencoding.JSON(context.Background(), again.Effective())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, other) || report.Counts != againReport.Counts {
		t.Fatal("source ordering affected effective graph or counts")
	}
	for i, file := range sources.Files {
		if !bytes.Equal(before[i], file.Bytes()) {
			t.Fatal("compiler changed acquired source bytes")
		}
	}
	for _, object := range state.Effective().Objects() {
		peer, ok := again.Effective().Find(object.Kind(), object.Name())
		if !ok || !object.Value().Equal(peer.Value()) {
			t.Fatal("source order changed object", object.Identity())
		}
	}
}

func TestFutureReferenceRefusalsAreSourceOrderIndependent(t *testing.T) {
	sources := exampleSources(t)
	before := admissionFingerprint(t, sources)
	reversed := sources
	reversed.Files = slices.Clone(sources.Files)
	slices.Reverse(reversed.Files)
	if !bytes.Equal(before, admissionFingerprint(t, reversed)) {
		t.Fatal("source order changed future-reference refusals")
	}
}

func TestActualCompilerDefaultPrecedencePreservesExplicitValues(t *testing.T) {
	data := readAcceptanceFixture(t, "defaults.yaml")
	sources := desiredstate.Sources{Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/environment.yaml", data)}, Roots: []string{"/synthetic"}}
	state, _ := compileAcceptance(t, sources)
	inherited := requireObject(t, state.Effective(), api.Machine, "inherited")
	explicit := requireObject(t, state.Effective(), api.Machine, "explicit")
	local := requireObject(t, state.Effective(), api.Machine, "local")
	if inherited.Spec().Get("access", "ssh", "port").Text() != "2200" || inherited.Spec().Get("access", "ssh", "addressRef").Text() != "ssh" {
		t.Fatal("Environment values did not precede conventional defaults")
	}
	if explicit.Spec().Get("access", "ssh", "port").Text() != "2222" || explicit.Spec().Get("access", "ssh", "addressRef").Text() != "fqdn" || explicit.Spec().Get("network", "addresses").Len() != 1 {
		t.Fatal("explicit scalar or empty list lost precedence")
	}
	if !local.Spec().Get("access", "local").Bool() || local.Spec().Has("access", "ssh") {
		t.Fatal("inactive access defaults crossed authored union choice")
	}
	playbook := requireObject(t, state.Effective(), api.CustomPlaybook, "disabled")
	if playbook.Spec().Get("enabled").Bool() || playbook.Spec().Get("order").Text() != "0" || playbook.Spec().Get("tags").Len() != 0 {
		t.Fatal("explicit false, zero, or empty collection changed")
	}
	if len(state.Effective().OfKind(api.Secret)) != 0 || len(state.Effective().OfKind(api.ContainerCluster)) != 0 {
		t.Fatal("unused defaults created objects or retention edges")
	}
}

func TestActualCompilerCanonicalEffectiveOutputIsInspectionOnly(t *testing.T) {
	sources := exampleDirectory(t, "lab-rhel")
	state, _ := compileAcceptance(t, sources)
	data, err := effectiveencoding.YAML(context.Background(), state.Effective())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("trustedMode")) || bytes.Contains(data, []byte("effectiveMode")) {
		t.Fatal("effective output contains a validation bypass marker")
	}
	copy := desiredstate.Sources{Files: []desiredstate.SourceFile{desiredstate.NewSourceFile(filepath.Join(sources.Roots[0], "effective.yaml"), data)}, Roots: slices.Clone(sources.Roots)}
	if state, _, err := wireCompiler().Compile(context.Background(), copy); err == nil || state != nil {
		t.Fatal("effective access incorrectly became ordinary authored input")
	} else {
		found := false
		for _, d := range diagnostics.Of(err) {
			found = found || d.Object != nil && d.Object.Kind == string(api.Machine) && d.Field == "$.spec.access"
		}
		if !found {
			t.Fatal("effective reload did not reject authored Machine access", diagnostics.Of(err))
		}
	}
	// Machine network declarations remain present; composed native addresses
	// belong to internal evidence rather than canonical template replacement.
	if !bytes.Contains(data, []byte("attachmentRef: lab-guests")) || !bytes.Contains(data, []byte("configRef: lab-guests")) {
		t.Fatal("canonical output lost Machine declaration structure")
	}
}

func FuzzAdmissionCompiler(f *testing.F) {
	const environment = `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: fuzz

spec:
  controller: {machineRef: service-host}

  domains:
    base: example.test
`
	for _, seed := range []string{
		"",
		"apiVersion: bootwright.io/v1alpha1\nkind: Proxy\nmetadata: {name: egress}\nspec: {management: external, connection: {httpProxy: 'http://proxy.example.test:3128'}}\n",
		"apiVersion: bootwright.io/v1alpha1\nkind: DNSServer\nmetadata: {name: resolver}\nspec: {management: external, address: '2001:0db8::53'}\n",
		"apiVersion: bootwright.io/v1alpha1\nkind: NTPServer\nmetadata: {name: clock}\nspec: {management: external, address: time.example.test}\n",
		"apiVersion: bootwright.io/v1alpha1\nkind: ArtifactServer\nmetadata: {name: artifacts}\nspec: {management: external, endpoints: [{name: media, url: 'https://artifacts.example.test'}]}\n",
		"apiVersion: bootwright.io/v1alpha1\nkind: Registry\nmetadata: {name: mirror}\nspec: {management: external, url: registry.example.test}\n",
		"apiVersion: bootwright.io/v1alpha1\nkind: LoadBalancer\nmetadata: {name: ingress}\nspec: {management: external, bindAddresses: [{address: '192.0.2.10'}]}\n",
		`apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: host

spec:
  os:
    provided: true
`,
		`apiVersion: bootwright.io/v1alpha1
kind: NetworkConfig
metadata:
  name: net

spec:
  machineNetwork:
    - cidr: 192.0.2.0/24

  nmstate:
    interfaces:
      - name: eth0
        type: ethernet
        ipv4:
          address:
            - ip: 192.0.2.1
`,
		`apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: host

spec:
  baremetal: {}

  libvirt:
    machineRef: missing
`,
		`apiVersion: bootwright.io/v1alpha1
kind: StorageExport
metadata:
  name: export

spec:
  type: dataFoundation
  clusterRef: missing

  externalDetails:
    fromSecretRef: missing
`,
	} {
		f.Add([]byte(seed), false)
	}
	f.Add([]byte(environment+`
  defaults:
    ContainerCluster:
      install:
        platform:
          type: none
          baremetal: {}
`), true)
	f.Fuzz(func(t *testing.T, body []byte, completeStream bool) {
		if len(body) > 32<<10 {
			t.Skip()
		}
		content := slices.Clone(body)
		if !completeStream {
			content = append([]byte(environment+"\n---\n"+serviceHost+"\n---\n"), content...)
		}
		sources := desiredstate.Sources{Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/environment.yaml", content)}, Roots: []string{"/synthetic"}}
		before := sources.Files[0].Bytes()
		first := admissionFingerprint(t, sources)
		second := admissionFingerprint(t, sources)
		if !bytes.Equal(first, second) {
			t.Fatal("admission outcome depends on prior calls")
		}
		if !bytes.Equal(before, sources.Files[0].Bytes()) || !bytes.Equal(content, before) {
			t.Fatal("admission mutated its input")
		}
	})
}

func admissionFingerprint(t *testing.T, sources desiredstate.Sources) []byte {
	t.Helper()
	state, report, err := wireCompiler().Compile(context.Background(), sources)
	if err != nil {
		if state != nil || report != nil {
			t.Fatal("failed compilation returned partial successful state")
		}
		data, jsonErr := json.Marshal(diagnostics.Of(err))
		if jsonErr != nil {
			t.Fatal(jsonErr)
		}
		return append([]byte("failure:"), data...)
	}
	if state == nil || report == nil {
		t.Fatal("successful compilation omitted state or report")
	}
	data, encodeErr := effectiveencoding.JSON(context.Background(), state.Effective())
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	counts, countErr := json.Marshal(report)
	if countErr != nil {
		t.Fatal(countErr)
	}
	return []byte(strings.Join([]string{"success", string(counts), string(data)}, "\n"))
}
