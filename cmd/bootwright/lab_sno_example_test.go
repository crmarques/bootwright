//go:build linux && amd64

package main

import (
	"encoding/json"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
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
