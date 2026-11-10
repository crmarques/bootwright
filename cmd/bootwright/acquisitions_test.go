package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/acquisition"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
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
	for _, item := range acquired {
		if item.Class == "" || item.Kind == "" || item.Artifact == "" {
			t.Errorf("a row names no kind, artifact or class: %+v", item)
		}
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
