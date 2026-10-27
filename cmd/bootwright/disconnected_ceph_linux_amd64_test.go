//go:build linux && amd64

package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

func TestDisconnectedProfileFreezesItsEntireMirrorClosure(t *testing.T) {
	sources := exampleDirectory(t, "libvirt-3-nodes-ceph-disconnected")
	state, _ := compileAcceptance(t, sources)
	if len(sources.Files) != 26 {
		t.Fatalf("files: %d", len(sources.Files))
	}
	if refused := productionUnsupported(t, state); len(refused) != 0 {
		t.Fatalf("unsupported: %+v", refused)
	}
	plan := planIngressFixture(t, state, "disconnected")
	found := false
	hosts := 0
	for _, block := range plan.Blocks {
		if strings.HasPrefix(block.ID, "ceph-host-") {
			request, err := ceph.DecodeHostRequest(block.Request)
			if err != nil {
				t.Fatal(err)
			}
			if request.Cluster.PackageRepositoryKey != "https://mirror.lab.example.test/keys/RPM-GPG-KEY-IBM-CEPH" || request.Cluster.Packages == nil || request.Cluster.Packages.PackageRepositoryTrustBundleRef != "mirror-ca" {
				t.Fatalf("host mirror: %+v", request)
			}
			hosts++
		}
		if block.ID != "ceph-cluster-ceph-lab" {
			continue
		}
		found = true
		request, err := ceph.DecodeRequest(block.Request)
		if err != nil {
			t.Fatal(err)
		}
		if request.PackageRepository != "https://mirror.lab.example.test/packages/ceph/9/rhel9/x86_64" || request.PackageRepositoryKey != "https://mirror.lab.example.test/keys/RPM-GPG-KEY-IBM-CEPH" || request.RegistryTrustBundleRef != "mirror-ca" {
			t.Fatalf("mirror identity: %+v", request)
		}
		if !strings.HasPrefix(request.Image, "mirror.lab.example.test/ceph/core@sha256:") {
			t.Fatal(request.Image)
		}
		var names []string
		for _, image := range request.Auxiliary {
			names = append(names, image.Name)
			if !strings.HasPrefix(image.Image, "mirror.lab.example.test/ceph/") || !strings.Contains(image.Image, "@sha256:") {
				t.Fatal(image)
			}
		}
		if !slices.Equal(names, []string{"alertmanager", "grafana", "haproxy", "keepalived", "node_exporter", "prometheus"}) {
			t.Fatal(names)
		}
	}
	if hosts != 3 {
		t.Fatalf("hosts: %d", hosts)
	}
	if !found {
		t.Fatal("no cluster block")
	}
}

func TestDisconnectedProfileMissingAuxiliaryClosureRefusesBeforePlanning(t *testing.T) {
	sources := exampleDirectory(t, "libvirt-3-nodes-ceph-disconnected")
	for i, file := range sources.Files {
		body := string(file.Bytes())
		if !strings.Contains(body, "\n  images:\n") {
			continue
		}
		start, end := strings.Index(body, "\n  images:\n"), strings.Index(body, "\n  entitlementRef:")
		sources.Files[i] = desiredstate.NewSourceFile(file.Path(), []byte(body[:start]+body[end:]))
	}
	state, _, err := wireCompiler().Compile(context.Background(), sources)
	if err != nil {
		t.Fatal(err)
	}
	refused := productionUnsupported(t, state)
	if len(refused) == 0 {
		t.Fatal("missing image closure admitted")
	}
	cluster, ok := state.Effective().Find(api.CephCluster, "ceph-lab")
	if !ok || cluster.Spec().Has("images") {
		t.Fatal("fixture did not remove the closure")
	}
}
