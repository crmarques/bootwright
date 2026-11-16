//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
	"github.com/crmarques/bootwright/internal/managedos/installation"

	"go.yaml.in/yaml/v3"
)

func TestSingleCephControllerDeclaresItsInstallLayout(t *testing.T) {
	directories := renderE2ECase(t, e2eRenderScript(t), "lab-sno-single-ceph-fdf", e2eRender{name: "default", sets: fipsControllerSubscriptions()})
	sources, err := (inputfs.Reader{}).Read(context.Background(), []string{directories[0]})
	if err != nil {
		t.Fatal(err)
	}
	state, _ := compileAcceptance(t, sources)
	catalog := state.Effective()
	provider := requireObject(t, catalog, api.InfraProvider, "lab-libvirt")
	profiles := provider.Spec().Get("libvirt", "machineProfiles").Items()
	if len(profiles) != 1 || profiles[0].Get("diskGiB").Text() != "56" {
		t.Fatal("the controller disk does not cover its media and publication budget")
	}
	profile := requireObject(t, catalog, api.MachineInstallProfile, "rhel")
	if profile.Spec().Get("installer", "anaconda", "partitioning", "home").Text() != "none" {
		t.Fatal("the controller installation leaves /home to automatic partitioning")
	}
	requests, _, err := installation.Requests(catalog, "controller", "synthetic")
	if err != nil || len(requests) != 1 || !strings.Contains(requests[0].Kickstart, "\nautopart --type=lvm --nohome\n") {
		t.Fatalf("the controller's planned installer lost its home policy: %v", err)
	}
}

func TestSingleCephSNOHasFiveNetworksAndOnlyClientStorageAccess(t *testing.T) {
	directories := renderE2ECase(t, e2eRenderScript(t), "lab-sno-single-ceph-fdf", e2eRender{name: "default", sets: fipsControllerSubscriptions()})
	sources, err := (inputfs.Reader{}).Read(context.Background(), []string{directories[1]})
	if err != nil {
		t.Fatal(err)
	}
	state, _ := compileAcceptance(t, sources)
	catalog := state.Effective()
	names := []string{}
	for _, network := range catalog.OfKind(api.Network) {
		names = append(names, network.Name())
		if network.Spec().Get("libvirt", "routedNetworkRefs").Len() != 0 {
			t.Fatal("the case routes between its networks")
		}
		if slices.Contains([]string{"staging", "production"}, network.Name()) && network.Spec().Get("libvirt", "hostAddressing").Text() != "none" {
			t.Fatal("environment host VLAN legs must remain addressless")
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"ceph-client", "ceph-replication", "openshift-infra", "production", "staging"}) {
		t.Fatal(names)
	}
	sno := requireObject(t, catalog, api.Machine, "sno-01")
	segments := []string{}
	for _, nic := range sno.Spec().Get("substrate", "libvirt", "nics").Items() {
		segments = append(segments, nic.Get("segmentRef").Text())
	}
	if !slices.Equal(segments, []string{"openshift-infra", "staging", "ceph-client"}) {
		t.Fatalf("SNO must have infrastructure, environment trunk and client NICs: %v", segments)
	}
	staging := requireObject(t, catalog, api.Network, "staging")
	if !slices.Contains(staging.Spec().Get("libvirt", "taggedNetworkRefs").Strings(), "production") {
		t.Fatal("SNO's environment trunk does not carry production")
	}
	ceph := requireObject(t, catalog, api.CephCluster, "ceph-e2e")
	if !ceph.Spec().Get("cephadm", "bootstrap", "singleHostDefaults").Bool() || ceph.Spec().Get("topology", "nodes").Len() != 1 {
		t.Fatal("the case is not explicitly single-host")
	}
	if ceph.Spec().Get("networks", "publicCIDRs").Equal(ceph.Spec().Get("networks", "clusterCIDRs")) {
		t.Fatal("replication and clients share a network")
	}
	cluster := requireObject(t, catalog, api.OpenShiftCluster, "sno")
	if cluster.Spec().Get("networking", "secondaryNetworks").Len() != 0 || !cluster.Spec().Get("security", "fips", "enabled").Bool() || !cluster.Spec().Has("security", "diskEncryption", "unlock", "tpm2") {
		t.Fatal("SNO must retain FIPS/TPM2 and primary OVN workloads")
	}
	binding := requireObject(t, catalog, api.ClusterAddonBinding, "sno-data-foundation")
	if binding.Spec().Get("packages", "fdf", "multiCloudGateway").Bool() || binding.Spec().Get("packages", "fdf", "subscription", "source").Text() != "isf-data-foundation-catalog" {
		t.Fatal("the constrained case must select IBM FDF with NooBaa disabled")
	}
	consumer := requireObject(t, catalog, api.CephConsumer, "sno-storage")
	for _, field := range []string{"rbdPoolRef", "filesystemRef", "objectGatewayRef"} {
		if consumer.Spec().Get("dataFoundation", field).Text() == "" {
			t.Fatalf("the default case has no %s", field)
		}
	}
}

func TestSingleCephStorageAndIsolationFixtures(t *testing.T) {
	root := filepath.Join("..", "..", "e2e")
	caseRoot := filepath.Join(root, "lab-sno-single-ceph-fdf")
	image := "registry.example.test/python@sha256:" + strings.Repeat("a", 64)
	for _, chart := range []string{filepath.Join(caseRoot, "app"), filepath.Join(caseRoot, "platform"), filepath.Join(root, "lab-sno-fips-controller", "isolation")} {
		out, err := exec.Command("helm", "template", "fixture", chart, "-f", filepath.Join(caseRoot, "values.yaml"), "--set", "isolation.image="+image).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v %s", chart, err, out)
		}
		decoder := yaml.NewDecoder(bytes.NewReader(out))
		found := map[string]bool{}
		for {
			var object map[string]any
			if err := decoder.Decode(&object); err == io.EOF {
				break
			} else if err != nil {
				t.Fatal(err)
			}
			if len(object) == 0 {
				continue
			}
			meta := object["metadata"].(map[string]any)
			found[object["kind"].(string)+"/"+meta["name"].(string)] = true
			if object["kind"] == "NodeNetworkConfigurationPolicy" {
				interfaces := object["spec"].(map[string]any)["desiredState"].(map[string]any)["interfaces"].([]any)
				if len(interfaces) != 1 {
					t.Fatal("client policy touches other interfaces")
				}
				nic := interfaces[0].(map[string]any)
				if nic["name"] != "enp3s0" || nic["ipv4"].(map[string]any)["forwarding"] != true {
					t.Fatal("client replies cannot reach pods")
				}
			}
			if object["kind"] == "ConfigMap" && meta["name"] == "e2e-isolation-config" {
				data := object["data"].(map[string]any)["config.json"].(string)
				probe := exec.Command("python3", filepath.Join(root, "lab-sno-fips-controller", "app", "verify.py"), "--config", "/dev/stdin", "--mode", "config")
				probe.Stdin = strings.NewReader(data)
				if result, err := probe.CombinedOutput(); err != nil || !json.Valid([]byte(data)) {
					t.Fatalf("direct VLAN fixture configuration: %v %s", err, result)
				}
			}
		}
		if filepath.Base(chart) == "platform" && !found["NodeNetworkConfigurationPolicy/sno-ceph-client-forwarding"] {
			t.Fatal("missing Ceph client forwarding")
		}
		if filepath.Base(chart) == "app" {
			for _, required := range []string{"PersistentVolumeClaim/block", "PersistentVolumeClaim/file", "ObjectBucketClaim/storage-object", "Pod/storage-writer", "Pod/storage-reader", "NetworkPolicy/storage-check"} {
				if !found[required] {
					t.Fatal("missing storage assertion fixture", required)
				}
			}
		}
	}
}
