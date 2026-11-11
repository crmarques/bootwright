//go:build linux && amd64

package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/mirrorfill"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

const mirrorRegistryFile = `apiVersion: bootwright.io/v1alpha1
kind: Registry
metadata:
  name: mirror-registry

spec:
  management: managed
  machineRef: %s
  implementation: distribution
  port: 5000
  credentialsRef: mirror-registry-credentials

  tls:
    secretRef: mirror-tls

  endpoints:
    - name: ip
      addressRef: ip
`

const mirrorRegistryCredentialsFile = `apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: mirror-registry-credentials

spec:
  type: usernamePassword

  source:
    generated:
      username: mirror
`

// sourceEdits rewrites an example's sources: each edit replaces its first text
// in the file ending with its suffix, each dropped file is removed and each
// added file joins the example's root.
type sourceEdits struct {
	replace map[string][2]string
	drop    []string
	add     map[string]string
}

func editedSources(t *testing.T, sources desiredstate.Sources, edits sourceEdits) desiredstate.Sources {
	t.Helper()
	pending := map[string][2]string{}
	for suffix, edit := range edits.replace {
		pending[suffix] = edit
	}
	files := []desiredstate.SourceFile{}
	for _, file := range sources.Files {
		if slices.ContainsFunc(edits.drop, func(suffix string) bool { return strings.HasSuffix(file.Path(), "/"+suffix) }) {
			continue
		}
		for suffix, edit := range pending {
			if !strings.HasSuffix(file.Path(), "/"+suffix) {
				continue
			}
			data := string(file.Bytes())
			if !strings.Contains(data, edit[0]) {
				t.Fatalf("%s holds no %q", suffix, edit[0])
			}
			file = desiredstate.NewSourceFile(file.Path(), []byte(strings.Replace(data, edit[0], edit[1], 1)))
			delete(pending, suffix)
		}
		files = append(files, file)
	}
	if len(pending) != 0 || len(sources.Roots) != 1 {
		t.Fatalf("the example lacks %v or has roots %v", pending, sources.Roots)
	}
	for path, data := range edits.add {
		files = append(files, desiredstate.NewSourceFile(filepath.Join(sources.Roots[0], path), []byte(data)))
	}
	slices.SortFunc(files, func(x, y desiredstate.SourceFile) int { return strings.Compare(x.Path(), y.Path()) })
	sources.Files = files
	return sources
}

// controllerPlacedMirrors is the managed image mirror example with its
// Registry and a managed artifact mirror both on the controller Machine,
// served at the controller's own address, and its resolver moved to the
// former mirror host, an SSH Machine whose pulls now read the mirror.
func controllerPlacedMirrors(t *testing.T) desiredstate.Sources {
	t.Helper()
	return editedSources(t, exampleDirectory(t, "lab-rhel-mirror-host"), sourceEdits{
		replace: map[string][2]string{
			"infra/components/mirror-registry.yaml": {"machineRef: mirror", "machineRef: controller"},
			"infra/components/dns.yaml":             {"machineRef: controller", "machineRef: mirror"},
			"secret-descriptors/mirror-tls.yaml":    {"- 203.0.113.20", "- 192.0.2.1"},
			"environment.yaml":                      {"      endpointRef: ip\n", "      endpointRef: ip\n\n    artifacts:\n      serverRef: mirror-artifacts\n      endpointRef: ip\n"},
		},
		add: map[string]string{"infra/components/mirror-artifacts.yaml": `apiVersion: bootwright.io/v1alpha1
kind: ArtifactServer
metadata:
  name: mirror-artifacts

spec:
  management: managed
  machineRef: controller
  retention: persistent

  tls:
    secretRef: mirror-tls

  listeners:
    - name: https
      protocol: https
      port: 443

  endpoints:
    - name: ip
      listenerRef: https
      addressRef: ip
`},
	})
}

// edgeFiles are an OS-ready Machine reached over SSH and a managed resolver on
// it, which nothing the mirror host's installation waits for runs beside.
var edgeFiles = map[string]string{
	"infra/machines/edge.yaml": `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: edge

spec:
  os:
    provided: true

  proxy:
    direct: {}

  network:
    addresses:
      - name: ip
        address: 203.0.113.30

  access:
    ssh:
      addressRef: ip
      knownHostsRef: edge-known-hosts

      auth:
        privateKeyRef: edge-access-key
`,
	"infra/components/edge-dns.yaml": `apiVersion: bootwright.io/v1alpha1
kind: DNSServer
metadata:
  name: edge-dns

spec:
  management: managed
  machineRef: edge
  implementation: dnsmasq
  port: 53

  endpoints:
    - name: ip
      addressRef: ip

  forwarders:
    - 192.0.2.53
`,
	"secret-descriptors/edge-access-key.yaml":  "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: edge-access-key\n\nspec:\n  type: sshKeyPair\n",
	"secret-descriptors/edge-known-hosts.yaml": "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: edge-known-hosts\n\nspec:\n  type: opaque\n",
}

// installedImageMirror is the installed mirror host's example with a managed
// image mirror beside its artifact mirror, whose install profile adds skopeo,
// a resolver on an edge Machine that pulls through it, and without its
// cluster, which a managed image mirror still refuses until its release is
// mirrored.
func installedImageMirror(t *testing.T) desiredstate.Sources {
	t.Helper()
	return installedImageMirrorWith(t, []string{"clusters/sno/cluster.yaml", "infra/machines/sno-01.yaml", "secret-descriptors/openshift-pull-secret.yaml", "secret-descriptors/sno-cluster-admin-ssh-key.yaml"})
}

func installedImageMirrorWith(t *testing.T, drop []string) desiredstate.Sources {
	t.Helper()
	added := map[string]string{
		"infra/components/mirror-registry.yaml":               strings.Replace(mirrorRegistryFile, "%s", "mirror", 1),
		"secret-descriptors/mirror-registry-credentials.yaml": mirrorRegistryCredentialsFile,
	}
	for path, data := range edgeFiles {
		added[path] = data
	}
	return editedSources(t, exampleDirectory(t, "lab-sno-installed-mirror-host"), sourceEdits{
		replace: map[string][2]string{
			"environment.yaml":              {"  mirror:\n", "  mirror:\n    registry:\n      registryRef: mirror-registry\n      endpointRef: ip\n\n"},
			"infra/os/rhel-9-8-mirror.yaml": {"        - podman\n", "        - podman\n        - skopeo\n"},
		},
		drop: drop,
		add:  added,
	})
}

// A managed image mirror on the controller Machine, beside the artifact
// mirror, runs its Registry and fill locally after the controller stage, whose
// host packages now install skopeo, and before every consumer off the
// controller; the controller's own services and emulated BMC pull their
// origins, and nothing waits in a cycle.
func TestAManagedImageMirrorOnTheControllerIsFilledLocallyAfterTheStage(t *testing.T) {
	const name = "lab-rhel-mirror-host"
	sources := controllerPlacedMirrors(t)
	order, waits := mirrorOrder(t, name, sources)
	const stage, registry, fill = clients.BlockID, "registry-mirror-registry", "mirror-images-" + name
	if order[0] != stage || len(waits[stage]) != 0 || slices.Contains(order, clients.MirroredClientsBlockID) {
		t.Fatalf("plan order = %v", order)
	}
	if !slices.Equal(waits[registry], []string{stage}) || !slices.Equal(waits[fill], []string{stage, registry}) {
		t.Fatalf("the Registry waits %v and its fill %v", waits[registry], waits[fill])
	}
	if !slices.Contains(waits["dns-lab-dns"], fill) {
		t.Errorf("the resolver off the controller does not wait for the fill: %v", waits["dns-lab-dns"])
	}
	for _, id := range []string{"ntp-lab-ntp", "proxy-lab-proxy", "machine-rhel-01", "artifact-server-mirror-artifacts"} {
		if slices.Contains(waits[id], fill) {
			t.Errorf("%s, on the mirror host, waits for its fill: %v", id, waits[id])
		}
	}
	state, _ := compileAcceptance(t, sources)
	for _, definition := range productionDefinitions(t, state, name) {
		switch definition.ID {
		case fill:
			request, err := mirrorfill.DecodeImagesRequest(definition.Request)
			if err != nil || request.Placement.Connection != machine.ConnectionLocal || request.Mirror.Host != "192.0.2.1:5000" || len(request.Images) != 1 {
				t.Fatalf("the fill is %+v (%v)", request, err)
			}
		case stage:
			request, err := clients.DecodeRequest(definition.Request)
			if err != nil || !request.ImageMirror || !slices.Contains(request.Packages, "skopeo") {
				t.Fatalf("the stage's host packages are %v (%v)", request.Packages, err)
			}
		}
	}
	services, _ := productionSourcesContext(t, name, name, sources)
	out, _ := contextRun(t, services, 0, "plan", "--context", name, "--output", "json")
	at := []int{}
	for _, id := range []string{stage, registry, fill, "dns-lab-dns"} {
		at = append(at, strings.Index(out, `"`+id+`"`))
	}
	if slices.Contains(at, -1) || !slices.IsSorted(at) {
		t.Fatalf("the plan does not preview the stage, the Registry, its fill and the resolver in that order:\n%s", out)
	}
}

// A managed image mirror on a Machine Bootwright installs runs its Registry
// and fill there, over the installed arm, after that installation; every
// service and emulated BMC the installation waits for pulls its origin, so
// the mirror never waits for itself, while the proxy beside them on the
// controller, which nothing there waits for, pulls through the mirror.
func TestAManagedImageMirrorOnAnInstalledMachinePlansAfterItsInstallation(t *testing.T) {
	const name = "lab-sno-installed-mirror-host"
	sources := installedImageMirror(t)
	order, waits := mirrorOrder(t, name, sources)
	const registry, fill = "registry-mirror-registry", "mirror-images-" + name
	if !slices.Contains(order, fill) || !slices.Contains(waits[registry], "os-install-mirror") || !slices.Contains(waits[fill], "os-install-mirror") {
		t.Fatalf("plan order = %v with the Registry waiting %v and its fill %v", order, waits[registry], waits[fill])
	}
	if !slices.Contains(waits["dns-edge-dns"], fill) {
		t.Errorf("the edge resolver does not wait for the fill: %v", waits["dns-edge-dns"])
	}
	if !slices.Contains(waits["proxy-lab-proxy"], fill) || !slices.Contains(waits["proxy-lab-proxy"], registry) {
		t.Errorf("the proxy on the controller, which the installation does not wait for, does not wait for the mirror: %v", waits["proxy-lab-proxy"])
	}
	for _, id := range []string{"os-install-mirror", "machine-mirror", "dns-lab-dns", "ntp-lab-ntp", "artifact-server-lab-artifacts"} {
		if slices.Contains(waits[id], registry) || slices.Contains(waits[id], fill) {
			t.Errorf("%s, which the mirror host's installation waits for, waits for the mirror: %v", id, waits[id])
		}
	}
	state, _ := compileAcceptance(t, sources)
	pulled := map[string]string{}
	for _, definition := range productionDefinitions(t, state, name) {
		switch definition.ID {
		case fill:
			request, err := mirrorfill.DecodeImagesRequest(definition.Request)
			if err != nil || !request.Placement.Installed() {
				t.Fatalf("the fill is %+v (%v)", request, err)
			}
		case "proxy-lab-proxy", "dns-lab-dns":
			request, err := managedservice.DecodeRequest(definition.Request, managedservice.RequestVersion)
			if err != nil {
				t.Fatal(err)
			}
			pulled[definition.ID] = request.Image
		}
	}
	if pulled["dns-lab-dns"] != dnsserver.Definition(nil).Image || pulled["proxy-lab-proxy"] == proxy.Definition().Image || !strings.HasSuffix(pulled["proxy-lab-proxy"], "/"+proxy.Definition().Image) {
		t.Fatalf("the controller's resolver and proxy pull %v", pulled)
	}
	services, _ := productionSourcesContext(t, name, name, sources)
	out, _ := contextRun(t, services, 0, "plan", "--context", name, "--output", "json")
	if install, at := strings.Index(out, `"os-install-mirror"`), strings.Index(out, `"`+fill+`"`); install < 0 || at < install {
		t.Fatalf("the plan does not preview the installation before the fill:\n%s", out)
	}
}

// With its cluster kept, the node's emulated BMC on the mirror host's own
// provider host waits for the mirror and pulls through it, while the mirror
// host's emulated BMC there still reads its origin.
func TestAnotherGuestsEmulatedBMCBesideAnInstalledMirrorHostPullsThroughTheMirror(t *testing.T) {
	const name = "lab-sno-installed-mirror-host"
	sources := installedImageMirrorWith(t, nil)
	_, waits := mirrorOrder(t, name, sources)
	const registry, fill = "registry-mirror-registry", "mirror-images-" + name
	if !slices.Contains(waits["machine-sno-01"], fill) || !slices.Contains(waits["machine-sno-01"], registry) {
		t.Errorf("the node's emulated BMC does not wait for the mirror: %v", waits["machine-sno-01"])
	}
	if slices.Contains(waits["machine-mirror"], fill) || slices.Contains(waits["machine-mirror"], registry) {
		t.Errorf("the mirror host's emulated BMC waits for the mirror: %v", waits["machine-mirror"])
	}
	state, _ := compileAcceptance(t, sources)
	images := map[string]string{}
	for _, definition := range productionDefinitions(t, state, name) {
		if definition.ID == "machine-sno-01" || definition.ID == "machine-mirror" {
			request, err := libvirt.DecodeMachineRequest(definition.Request)
			if err != nil {
				t.Fatal(err)
			}
			images[definition.ID] = request.Controller.Image
		}
	}
	if images["machine-mirror"] != libvirt.EmulatorImage() || !strings.HasSuffix(images["machine-sno-01"], "/"+libvirt.EmulatorImage()) || images["machine-sno-01"] == libvirt.EmulatorImage() {
		t.Fatalf("the emulated BMCs pull %v", images)
	}
}

// The acquisition list reads the same per-consumer derivation: the provider's
// emulated BMC image once from its origin, for the mirror host's own, and once
// through the mirror, for the node's, and the controller's proxy through the
// mirror beside its resolver at its origin.
func TestTheAcquisitionListNamesEachEmulatedBMCAndServicePullBesideAnInstalledMirrorHost(t *testing.T) {
	state, _ := compileAcceptance(t, installedImageMirrorWith(t, nil))
	acquired, err := listAcquisitions(state.Effective())
	if err != nil {
		t.Fatal(err)
	}
	rows := func(kind, consumer string) []string {
		var artifacts []string
		for _, item := range acquired {
			if item.Kind == kind && item.Consumer == consumer && slices.Equal(item.Machines, []string{"controller"}) {
				artifacts = append(artifacts, item.Artifact)
			}
		}
		slices.Sort(artifacts)
		return artifacts
	}
	sushy, _, _ := strings.Cut(libvirt.EmulatorImage(), "@")
	if got := rows("emulated BMC image", "InfraProvider/lab-libvirt"); len(got) != 2 || got[1] != sushy || !strings.HasSuffix(got[0], "/"+sushy) {
		t.Errorf("the provider's emulated BMC images are %v", got)
	}
	if got := rows("service image", "Proxy/lab-proxy"); len(got) != 1 || !strings.HasSuffix(got[0], "/docker.io/ubuntu/squid") {
		t.Errorf("the controller's proxy reads %v", got)
	}
	if got := rows("service image", "DNSServer/lab-dns"); len(got) != 1 || got[0] != "docker.io/4km3/dnsmasq" {
		t.Errorf("the installation's resolver reads %v", got)
	}
	images, err := mirrorImageArtifacts(state.Effective())
	if err != nil || !slices.ContainsFunc(images, func(image mirrorfill.Image) bool { return image.Repository == sushy }) {
		t.Fatalf("the fill copies %+v (%v), not the node's emulated BMC image", images, err)
	}
}

// With its cluster kept, the installed mirror host's example copies the
// cluster's release into the image mirror there, over the installed arm after
// that installation, before the cluster's image build.
func TestAReleaseCopyOnAnInstalledMirrorHostPlansAfterItsInstallation(t *testing.T) {
	const name, release = "lab-sno-installed-mirror-host", "mirror-release-sno"
	sources := installedImageMirrorWith(t, nil)
	order, waits := mirrorOrder(t, name, sources)
	if !slices.Contains(waits[release], "os-install-mirror") || !slices.Contains(waits[release], "registry-mirror-registry") ||
		!slices.Contains(waits["cluster-media-sno"], release) {
		t.Fatalf("plan order = %v with the release copy waiting %v and the media %v", order, waits[release], waits["cluster-media-sno"])
	}
	state, _ := compileAcceptance(t, sources)
	for _, definition := range productionDefinitions(t, state, name) {
		if definition.ID == release {
			request, err := mirrorfill.DecodeReleaseRequest(definition.Request)
			if err != nil || !request.Placement.Installed() {
				t.Fatalf("the release copy is %+v (%v)", request, err)
			}
		}
	}
	services, _ := productionSourcesContext(t, name, name, sources)
	out, _ := contextRun(t, services, 0, "plan", "--context", name, "--output", "json")
	if install, at := strings.Index(out, `"os-install-mirror"`), strings.Index(out, `"`+release+`"`); install < 0 || at < install {
		t.Fatalf("the plan does not preview the installation before the release copy:\n%s", out)
	}
}
