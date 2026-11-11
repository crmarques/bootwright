package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/acquisition"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestTheAcquisitionListNamesWhatEachOwnerAcquiresWithItsOrigin(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "lab-compact-df"))
	acquired, err := listAcquisitions(state.Effective())
	if err != nil {
		t.Fatal(err)
	}
	find := func(kind, artifact string) cli.Acquisition {
		t.Helper()
		index := slices.IndexFunc(acquired, func(item cli.Acquisition) bool { return item.Kind == kind && item.Artifact == artifact })
		if index < 0 {
			t.Fatalf("the list names no %s %s", kind, artifact)
		}
		return acquired[index]
	}
	helm, _, err := controller.PinOf("helm")
	if err != nil {
		t.Fatal(err)
	}
	if row := find("client", "helm"); row.SHA256 != helm.Default.SHA256 || row.Class != acquisition.ClassPublic || !slices.Equal(row.Machines, []string{"controller"}) {
		t.Errorf("helm: %+v", row)
	}
	if row := find("client", "openshift-install"); row.Class != acquisition.ClassVendor || row.SHA256 != "" {
		t.Errorf("the installer, read from its vendor by the publisher's checksum, is %+v", row)
	}
	if row := find("client release metadata", "openshift-install"); row.Origin != "mirror.openshift.com" || row.Class != acquisition.ClassVendor {
		t.Errorf("the installer's checksum read from its publisher is %+v", row)
	}
	if slices.ContainsFunc(acquired, func(item cli.Acquisition) bool { return item.Artifact == "helm" && item.Kind != "client" }) {
		t.Error("the pinned helm names a publisher contact it never makes")
	}
	if row := find("host package", "lorax"); row.Class != "host repositories" {
		t.Errorf("lorax: %+v", row)
	}
	if row := find("service image", "docker.io/ubuntu/squid"); row.Class != acquisition.ClassPublic || len(row.SHA256) != 64 || row.Consumer != "Proxy/lab-proxy" {
		t.Errorf("the proxy image: %+v", row)
	}
	if row := find("emulated BMC image", "quay.io/metal3-io/sushy-tools"); row.Class != acquisition.ClassPublic {
		t.Errorf("the emulated BMC image: %+v", row)
	}
	for _, repository := range openShiftReleaseRepositories {
		if row := find("release", repository); row.Class != acquisition.ClassPublic || !slices.Contains(row.Machines, "controller") || len(row.Machines) < 2 {
			t.Errorf("the release repository %s: %+v", repository, row)
		}
	}
	if row := find("storage image", "cp.icr.io/cp/ibm-ceph/ceph-9-rhel10"); row.Class != acquisition.ClassVendor || !slices.Contains(row.Machines, "rhel-01") {
		t.Errorf("the Ceph image: %+v", row)
	}
	if row := find("storage packages", "cephadm and its packages"); row.Class != acquisition.ClassVendor || !strings.HasPrefix(row.Origin, "https://public.dhe.ibm.com/") {
		t.Errorf("the Ceph packages: %+v", row)
	}
	catalogImage := "icr.io/cpopen/isf-data-foundation-catalog"
	if row := find("add-on catalog image", catalogImage); row.Class != acquisition.ClassVendor || row.Consumer != "ClusterAddonBinding/compact-data-foundation" || row.Version != "sha256:"+strings.Repeat("2", 64) || !slices.Equal(row.Machines, []string{"compact-01", "compact-02", "compact-03"}) {
		t.Errorf("the add-on catalog image: %+v", row)
	}
	for _, name := range addons.DefaultCatalogs() {
		repository := defaultCatalogRepositories[name]
		if repository == "" {
			t.Fatalf("the default catalog %s names no index repository", name)
		}
		if row := find("default catalog image", repository); row.Class != acquisition.ClassVendor || row.Version != "v4.21" || row.Consumer != "OpenShiftCluster/compact" || slices.Contains(row.Machines, "controller") {
			t.Errorf("the default catalog %s: %+v", repository, row)
		}
	}
	if row := find("update service", "the cluster's update graph"); row.Origin != updateServiceURL || row.Class != acquisition.ClassVendor || row.Version != "stable-4.21" {
		t.Errorf("the update service: %+v", row)
	}
	if row := find("machine repository", "ibm-storage-ceph-9"); row.Class != acquisition.ClassVendor || row.Consumer != "MachineInstallProfile/rhel-9-8" || !slices.Equal(row.Machines, []string{"rhel-01", "rhel-02", "rhel-03"}) {
		t.Errorf("the Machines' configured repository: %+v", row)
	}
	if row := find("registry token service", "auth.docker.io"); row.Class != acquisition.ClassPublic || !slices.Contains(row.Machines, "controller") || !strings.Contains(row.Consumer, "Proxy/lab-proxy") {
		t.Errorf("the Docker Hub token service: %+v", row)
	}
	if row := find("registry blob host", "cdn01.quay.io"); row.Class != acquisition.ClassPublic || !strings.Contains(row.Consumer, "OpenShiftCluster/compact") || !slices.Contains(row.Machines, "compact-01") {
		t.Errorf("a quay.io blob host: %+v", row)
	}
	if row := find("registry blob host", "dd2.icr.io"); row.Class != acquisition.ClassVendor || row.Consumer != "ClusterAddonBinding/compact-data-foundation" {
		t.Errorf("an icr.io blob host: %+v", row)
	}
	for _, item := range acquired {
		if item.Class == "" || item.Kind == "" || item.Artifact == "" {
			t.Errorf("a row names no kind, artifact or class: %+v", item)
		}
	}
}

func TestAManagedArtifactMirrorIsFilledFromThePublisherOnItsHostAndReadByTheController(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "lab-sno-mirror-host"))
	acquired, err := listAcquisitions(state.Effective())
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"helm"} {
		pin, _, err := controller.PinOf(kind)
		if err != nil {
			t.Fatal(err)
		}
		client := slices.IndexFunc(acquired, func(item cli.Acquisition) bool { return item.Kind == "client" && item.Artifact == kind })
		fill := slices.IndexFunc(acquired, func(item cli.Acquisition) bool { return item.Kind == "mirror file" && item.Artifact == kind })
		if client < 0 || fill < 0 {
			t.Fatalf("%s: the list names no client or mirror file row: %+v", kind, acquired)
		}
		read, filled := acquired[client], acquired[fill]
		if !strings.HasPrefix(read.Origin, "https://203.0.113.20/mirror/lab-sno-mirror-host/"+kind+"/") || !slices.Equal(read.Machines, []string{"controller"}) || read.SHA256 != pin.Default.SHA256 {
			t.Errorf("the controller reads %s from %+v, not the managed mirror", kind, read)
		}
		if filled.Class != acquisition.ClassPublic || strings.Contains(filled.Origin, "203.0.113.20") || !slices.Equal(filled.Machines, []string{"mirror"}) ||
			filled.Consumer != "ArtifactServer/mirror-artifacts" || filled.SHA256 != pin.Default.SHA256 || filled.Bytes != pin.Default.Bytes {
			t.Errorf("the mirror host fills %s from %+v, not its publisher", kind, filled)
		}
	}
	if slices.ContainsFunc(acquired, func(item cli.Acquisition) bool {
		return item.Kind == "mirror file" && strings.HasPrefix(item.Artifact, "openshift")
	}) {
		t.Error("a vendor client is filled into the mirror, which serves public origins alone")
	}
}

func TestAShortImageNameIsClassedByTheRegistryItResolvesTo(t *testing.T) {
	row := imageAcquisition("service image", "ubuntu/squid@sha256:"+strings.Repeat("a", 64), []string{"controller"}, "Proxy/egress")
	if row.Artifact != "docker.io/ubuntu/squid" || row.Class != acquisition.ClassPublic {
		t.Fatalf("a short name: %+v", row)
	}
	if row := imageAcquisition("service image", "registry.example.test:5000/squid@sha256:"+strings.Repeat("a", 64), nil, ""); row.Class != acquisition.ClassDeclared {
		t.Fatalf("an enterprise registry: %+v", row)
	}
}

func TestAnInstalledMachineNamesItsHostedSubscriptionAndItsConfiguredRepositories(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "libvirt-3-nodes-ceph-host-variants"))
	acquired, err := listAcquisitions(state.Effective())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"machine registration": "https://subscription.rhsm.redhat.com", "machine repositories": "https://cdn.redhat.com"}
	for kind, origin := range want {
		index := slices.IndexFunc(acquired, func(item cli.Acquisition) bool { return item.Kind == kind })
		if index < 0 {
			t.Fatalf("the list names no %s", kind)
		}
		if row := acquired[index]; row.Origin != origin || row.Class != acquisition.ClassVendor || row.Consumer != "MachineInstallProfile/rhel-9-8" || !slices.Equal(row.Machines, []string{"rhel-01", "rhel-02", "rhel-03"}) {
			t.Errorf("%s: %+v", kind, row)
		}
	}
	if !slices.ContainsFunc(acquired, func(item cli.Acquisition) bool {
		return item.Kind == "machine repository key" && item.Artifact == "ibm-storage-ceph-9" && strings.HasPrefix(item.Origin, "https://public.dhe.ibm.com/")
	}) {
		t.Error("the configured repository's key URL is not listed")
	}
}

func TestAMachineRegisteringWithASatelliteNamesTheSatelliteAndItsContentRoot(t *testing.T) {
	sources := exampleDirectory(t, "libvirt-3-nodes-ceph-host-variants")
	satellite := "    connectToInsights: false\n\n    satellite:\n      hostname: satellite.example.test\n      trustBundleRef: satellite-ca\n"
	bundle := "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: satellite-ca\n\nspec:\n  type: caBundle\n\n  source:\n    contextStore: {}\n"
	for index, file := range sources.Files {
		if strings.Contains(string(file.Bytes()), "name: rhel-rhsm\n") {
			sources.Files[index] = desiredstate.NewSourceFile(file.Path(), []byte(mustReplaceOnce(t, file.Path(), string(file.Bytes()), "    connectToInsights: false\n", satellite)))
		}
	}
	sources.Files = append(sources.Files, desiredstate.NewSourceFile(filepath.Join(sources.Roots[0], "satellite-ca.yaml"), []byte(bundle)))
	state, _ := compileAcceptance(t, sources)
	acquired, err := listAcquisitions(state.Effective())
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	want := map[string]string{"machine registration": "https://satellite.example.test", "machine repositories": "https://satellite.example.test/pulp/content"}
	for kind, origin := range want {
		index := slices.IndexFunc(acquired, func(item cli.Acquisition) bool { return item.Kind == kind })
		if index < 0 {
			t.Fatalf("the list names no %s", kind)
		}
		if row := acquired[index]; row.Origin != origin || row.Class != acquisition.ClassDeclared || !slices.Equal(row.Machines, []string{"rhel-01", "rhel-02", "rhel-03"}) {
			t.Errorf("%s: %+v", kind, row)
		}
	}
	if slices.ContainsFunc(acquired, func(item cli.Acquisition) bool {
		return item.Origin == hostedRegistrationHost || item.Origin == hostedContentHost
	}) {
		t.Error("a Machine registering with a Satellite is listed as reaching the hosted service")
	}
}

func TestOnlyARegistryTheListKnowsGetsItsTokenAndDeliveryHosts(t *testing.T) {
	rows := withRegistryHosts([]cli.Acquisition{
		{Kind: "service image", Artifact: "docker.io/ubuntu/squid", Origin: "docker.io/ubuntu/squid", Machines: []string{"b"}, Consumer: "Proxy/two"},
		{Kind: "service image", Artifact: "docker.io/4km3/dnsmasq", Origin: "docker.io/4km3/dnsmasq", Machines: []string{"a"}, Consumer: "DNSServer/one"},
		{Kind: "service image", Artifact: "registry.example.test/squid", Origin: "registry.example.test/squid", Machines: []string{"c"}, Consumer: "Proxy/three"},
		{Kind: "client", Artifact: "helm", Origin: "https://quay.io/helm", Machines: []string{"d"}, Consumer: "Environment/lab"},
	})
	got := map[string]cli.Acquisition{}
	for _, row := range rows[4:] {
		got[row.Artifact] = row
	}
	if len(got) != 3 || len(rows) != 7 {
		t.Fatalf("only the Docker Hub image names companion hosts: %+v", rows[4:])
	}
	if row := got["auth.docker.io"]; row.Kind != "registry token service" || row.Class != acquisition.ClassPublic || row.Consumer != "DNSServer/one, Proxy/two" || !slices.Equal(row.Machines, []string{"b", "a"}) {
		t.Errorf("the token service merges its consumers: %+v", row)
	}
	if got["registry-1.docker.io"].Kind != "registry endpoint" || got["production.cloudfront.docker.com"].Kind != "registry blob host" {
		t.Errorf("the endpoint and blob host: %+v", got)
	}
}

func TestAnImageNamedByTagKeepsTheTagAsItsVersion(t *testing.T) {
	row := imageAcquisition("add-on catalog image", "registry.example.test:5000/team/catalog:v1.2", nil, "")
	if row.Artifact != "registry.example.test:5000/team/catalog" || row.Version != "v1.2" || row.SHA256 != "" {
		t.Fatalf("a tagged reference: %+v", row)
	}
	if row := imageAcquisition("add-on catalog image", "registry.example.test:5000/team/catalog", nil, ""); row.Artifact != "registry.example.test:5000/team/catalog" || row.Version != "" {
		t.Fatalf("a port is not a tag: %+v", row)
	}
}

func TestAnAuthoredAddOnNamesItsCatalogImageAloneOnTheRegistryItsAuthorChose(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "lab-sno-authored"))
	acquired, err := listAcquisitions(state.Effective())
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	index := slices.IndexFunc(acquired, func(item cli.Acquisition) bool { return item.Kind == "add-on catalog image" })
	if index < 0 {
		t.Fatal("the list names no add-on catalog image")
	}
	if row := acquired[index]; row.Artifact != "registry.example.test/example-operator/catalog" || row.Class != acquisition.ClassDeclared || row.Consumer != "ClusterAddonBinding/widgets" || !slices.Equal(row.Machines, []string{"sno-01"}) {
		t.Errorf("the authored catalog: %+v", row)
	}
}

func TestARepositoryKeyURLIsListedWithoutItsCredentialOrQuery(t *testing.T) {
	if got := redactedLocation("https://user:secret@keys.example.test/RPM-GPG-KEY?token=abc#frag"); got != "https://keys.example.test/RPM-GPG-KEY" {
		t.Fatalf("a credentialed key URL is listed as %s", got)
	}
}
