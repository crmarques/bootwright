package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	effectiveencoding "github.com/crmarques/bootwright/internal/desiredstate/encoding"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
)

func exampleSources(t *testing.T) desiredstate.Sources {
	t.Helper()
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("filesystem admission is qualified for linux/amd64")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "examples", "multidc-platform"))
	if err != nil {
		t.Fatal(err)
	}
	sources, err := (inputfs.Reader{}).Read(context.Background(), []string{root})
	if err != nil {
		t.Fatal(desiredstate.DiagnosticsOf(err))
	}
	return sources
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

func compileAcceptance(t *testing.T, sources desiredstate.Sources) (*compilation.State, *compilation.Report) {
	t.Helper()
	state, report, err := wireCompiler().Compile(context.Background(), sources)
	if err != nil {
		t.Fatal(desiredstate.DiagnosticsOf(err))
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

func TestActualCLIValidatesCompleteExample(t *testing.T) {
	sources := exampleSources(t)
	if len(sources.Files) != 100 {
		t.Fatalf("example discovery: got %d files, want 100", len(sources.Files))
	}
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"validate", "-f", sources.Roots[0], "--output", "json"}, &out, &errOut)
	var result struct {
		OK     bool
		Result struct {
			Counts                    compilation.Counts
			Advisories                []any
			ExcludedResourceFiles     []string
			ExcludedContainerClusters []string
			ExcludedStorageClusters   []string
		}
		Diagnostics []any
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err, out.String())
	}
	if code != 0 || errOut.Len() != 0 || !result.OK || result.Result.Counts != (compilation.Counts{FilesSeen: 100, ObjectsDecoded: 100}) || len(result.Diagnostics) != 0 || len(result.Result.Advisories) != 0 {
		t.Fatalf("actual CLI admission: code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	if len(result.Result.ExcludedResourceFiles)+len(result.Result.ExcludedContainerClusters)+len(result.Result.ExcludedStorageClusters) != 0 {
		t.Fatal("unexpected example exclusions")
	}
	state, _ := compileAcceptance(t, sources)
	if len(state.Effective().Objects()) != 100 {
		t.Fatal("example objects lost")
	}
	installed := requireObject(t, state.Effective(), api.Machine, "storage-a-01")
	if installed.Spec().Get("access", "ssh", "user").Text() != "bootwright" || installed.Spec().Get("access", "ssh", "auth", "privateKeyRef").Text() != "bootwright-machine-key" {
		t.Fatal("installed Machine access was not derived")
	}
	if requireObject(t, state.Authored(), api.Machine, "storage-a-01").Spec().Has("access") {
		t.Fatal("authored Machine acquired effective access")
	}
}

func TestActualCompilerAdmitsAllKindsAndAdditionalVariants(t *testing.T) {
	sources := expandedExampleSources(t)
	state, report := compileAcceptance(t, sources)
	if report.Counts != (compilation.Counts{FilesSeen: 101, ObjectsDecoded: 120}) {
		t.Fatalf("expanded graph counts: %#v", report.Counts)
	}
	for _, kind := range api.Kinds() {
		if len(state.Effective().OfKind(kind)) == 0 {
			t.Errorf("kind %s has no full-wiring positive admission coverage", kind)
		}
	}
	if len(api.Kinds()) != 26 {
		t.Fatalf("kind catalog changed: %d", len(api.Kinds()))
	}
	nfs := requireObject(t, state.Effective(), api.StorageNFSExport, "acceptance-nfs")
	if nfs.Spec().Get("port").Text() != "2049" || nfs.Spec().Get("exports").Items()[0].Get("clients").Items()[0].Text() != "192.0.2.0/24" {
		t.Fatal("NFS normalization missing")
	}
	playbook := requireObject(t, state.Effective(), api.CustomPlaybook, "acceptance-playbook")
	if playbook.Spec().Get("enabled").Bool() || playbook.Spec().Get("extraVars", "literal").Type() != api.String || playbook.Spec().Get("extraVars", "fraction").Type() != api.Number {
		t.Fatal("reserved playbook declaration changed")
	}
	libvirt := requireObject(t, state.Effective(), api.InfraProvider, "acceptance-libvirt")
	if libvirt.Spec().Get("libvirt", "bmcEmulationDefaults", "vMediaPort").Text() != "8001" {
		t.Fatal("libvirt dependent default missing")
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

func TestActualCompilerSourceOrderIndependence(t *testing.T) {
	sources := exampleSources(t)
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
	sources := exampleSources(t)
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
		for _, d := range desiredstate.DiagnosticsOf(err) {
			found = found || d.Object != nil && d.Object.Kind == string(api.Machine) && d.Field == "$.spec.access"
		}
		if !found {
			t.Fatal("effective reload did not reject authored Machine access", desiredstate.DiagnosticsOf(err))
		}
	}
	// Machine network declarations remain present; composed native addresses
	// belong to internal evidence rather than canonical template replacement.
	if !bytes.Contains(data, []byte("interfaceBinding:")) || !bytes.Contains(data, []byte("configRef: ceph-network")) {
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
		data, jsonErr := json.Marshal(desiredstate.DiagnosticsOf(err))
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
