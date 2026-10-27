package agentinstall

import (
	"context"
	"fmt"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/substrate"
)

func pushedCatalog(masters, workers int, baremetal bool) api.Catalog {
	objects := topologyCatalog(masters, workers, 0, false).Objects()
	for index, object := range objects {
		spec := object.Spec()
		if object.Kind() == api.InfraProvider {
			spec = api.MapValue(field("kubevirt", api.MapValue(text("kubeconfigRef", "host-access"), text("namespace", "guests"))))
		}
		if object.Kind() == api.OpenShiftCluster {
			spec = spec.WithPath(spec.Get("install", "agent").Without("redfishVirtualMedia"), "install", "agent")
			if !baremetal {
				spec = spec.WithPath(api.MapValue(field("none", api.MapValue())), "install", "platform")
				for _, slot := range []string{"api", "api-int", "ingress"} {
					spec = spec.WithPath(api.StringValue("external"), "install", "endpoints", slot, "source", "type")
				}
			}
		}
		objects[index] = object.WithSpec(spec)
	}
	return api.NewCatalog(objects)
}

func TestPushedInstallerProjectionGolden(t *testing.T) {
	for _, shape := range []struct {
		masters, workers int
		baremetal        bool
	}{{3, 0, false}, {3, 0, true}, {3, 2, false}, {1, 0, false}} {
		name := fmt.Sprintf("pushed-kubevirt-%d-%d-baremetal-%t", shape.masters, shape.workers, shape.baremetal)
		t.Run(name, func(t *testing.T) {
			catalog := pushedCatalog(shape.masters, shape.workers, shape.baremetal)
			cluster := catalog.OfKind(api.OpenShiftCluster)[0]
			var nodes []nodeProjection
			for index, declared := range cluster.Spec().Get("nodes").Items() {
				bound, _ := catalog.Find(api.Machine, declared.Get("machineRef").Text())
				address, err := installAddress(catalog, bound)
				if err != nil {
					t.Fatal(err)
				}
				// Explicit pinned NIC facts exercise installer projection, not B87's unqualified MAC derivation.
				interfaces := []substrate.Interface{{Name: "enp1s0", MACAddress: fmt.Sprintf("02:00:00:00:01:%02x", index)}}
				network, err := networkConfig(catalog, bound, interfaces, nil)
				if err != nil {
					t.Fatal(err)
				}
				nodes = append(nodes, nodeProjection{address: address, interfaces: interfaces, machine: bound, name: declared.Get("name").Text(), role: declared.Get("role").Text(), network: network})
			}
			install, err := installConfig(catalog, cluster, nodes)
			if err != nil {
				t.Fatal(err)
			}
			agent, err := agentConfig(cluster, nodes, nil)
			if err != nil {
				t.Fatal(err)
			}
			assertPushedInterfaceWiring(t, agent)
			matchesGolden(t, name, []byte(encoded(t, map[string]any{"agentConfig": agent, "installConfig": install})))
			if _, reason, _ := refusedCluster(catalog, cluster); reason == "" {
				t.Fatal("projection qualified an unrealized provider")
			}
		})
	}
}

func assertPushedInterfaceWiring(t *testing.T, agent map[string]any) {
	t.Helper()
	for _, raw := range agent["hosts"].([]any) {
		host := raw.(map[string]any)
		native := host["networkConfig"].(map[string]any)
		macs := map[string]string{}
		for _, entry := range native["interfaces"].([]any) {
			iface := entry.(map[string]any)
			if iface["type"] == "ethernet" {
				macs[iface["name"].(string)], _ = iface["mac-address"].(string)
			}
		}
		for _, entry := range host["interfaces"].([]any) {
			iface := entry.(map[string]any)
			if macs[iface["name"].(string)] != iface["macAddress"] {
				t.Fatalf("host %s has no matching native interface and MAC for %+v", host["hostname"], iface)
			}
		}
	}
}

func TestPushedMediaHasNoPublicationOrArtifactDependency(t *testing.T) {
	catalog := pushedCatalog(3, 0, false)
	needs := Requirements{}
	cluster := catalog.OfKind(api.OpenShiftCluster)[0]
	delivery, image, certificate, placement, err := mediaLocation(catalog, cluster, "controller", testContext, &needs)
	if err != nil {
		t.Fatal(err)
	}
	if delivery != "push" || image.URL != "" || image.Path != WorkRoot(testContext, cluster.Name())+"/agent.iso" || certificate != "" || !placement.Local() || len(needs.ArtifactServers) != 0 {
		t.Fatalf("%s %+v %q %+v %+v", delivery, image, certificate, placement, needs)
	}
	execution, request := mediaExecution(t, testDigest)
	request.DeliveryKind, request.Image, request.TLSCertificateRef = delivery, image, certificate
	request.WorkRoot, request.Placement = WorkRoot(testContext, cluster.Name()), placement
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeMediaRequest(canonical); err != nil {
		t.Fatal(err)
	}
	if len(mediaRequires(needs)) != 0 || len(request.ReservationKeys()) != 1 {
		t.Fatal(needs, request.ReservationKeys())
	}
	for _, impact := range mediaImpacts(reconciliation.Apply, request) {
		if strings.HasPrefix(impact, "publish-content") {
			t.Fatal(impact)
		}
	}
	definition, err := mediaDefinition(request, needs, reconciliation.Apply)
	if err != nil {
		t.Fatal(err)
	}
	wantIdentity, err := reconciliation.RequestDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	install := InstallRequest{DeliveryKind: delivery, Image: image, PublicationRoot: image}
	if err := bindPushedMedia(request, needs, &install); err != nil {
		t.Fatal(err)
	}
	if install.ImageIdentity != wantIdentity || install.PublicationRoot != (Publication{}) || install.Image.Path != image.Path {
		t.Fatal(install)
	}
	if canonical, err := install.Canonical(); err != nil || strings.Contains(string(canonical), "host-access") {
		t.Fatal(string(canonical), err)
	}
	groups := mediaGroups(reconciliation.Apply, request)
	if len(groups) != 1 || groups[0].ID != "build-image" {
		t.Fatal(groups)
	}
	runner := &fakeRunner{}
	capability := NewMedia(runner, material.AuthorizedKey)
	if _, err := capability.run(context.Background(), execution, "apply", request); err != nil {
		t.Fatal(err)
	}
	for _, file := range runner.requests[0].Materials {
		if file.Variable == "artifactCertificate" || file.Secret == "" {
			t.Fatal(file)
		}
	}
}
