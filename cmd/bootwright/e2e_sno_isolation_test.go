//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"

	"go.yaml.in/yaml/v3"
)

func TestSNOIsolationFixtures(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "e2e", "lab-sno-fips-controller")
	image := "registry.example.test/python@sha256:" + strings.Repeat("a", 64)
	args := []string{"template", "isolation", filepath.Join(root, "isolation"), "-f", filepath.Join(root, "values.yaml"), "--set", "isolation.image=" + image}
	for _, test := range []struct {
		name    string
		sets    []string
		refusal string
	}{
		{name: "default"},
		{name: "custom", sets: []string{"environments.staging.egressAddress=192.0.2.61", "host.domain=alternate.example.test"}},
		{name: "no TPM", sets: []string{"security.tpm2=false"}, refusal: "requires security.tpm2=true"},
		{name: "no VLANs", sets: []string{"environments.enabled=false"}, refusal: "requires environments.enabled=true"},
		{name: "mutable image", sets: []string{"isolation.image=registry.example.test/python:latest"}, refusal: "image pinned"},
	} {
		t.Run(test.name, func(t *testing.T) {
			argv := append([]string{}, args...)
			for _, value := range test.sets {
				argv = append(argv, "--set", value)
			}
			out, err := exec.Command(pinnedHelm(t), argv...).CombinedOutput()
			if test.refusal != "" {
				if err == nil || !bytes.Contains(out, []byte(test.refusal)) {
					t.Fatalf("expected refusal %q: %v %s", test.refusal, err, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("render: %v: %s", err, out)
			}
			decoder := yaml.NewDecoder(bytes.NewReader(out))
			objects := map[string]map[string]any{}
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
				key := object["kind"].(string) + "/" + stringValue(meta["namespace"]) + "/" + meta["name"].(string)
				if objects[key] != nil {
					t.Fatalf("duplicate object %s", key)
				}
				objects[key] = object
			}
			configMap := objects["ConfigMap/staging/e2e-isolation-config"]
			configPath := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(configPath, []byte(configMap["data"].(map[string]any)["config.json"].(string)), 0600); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(pinnedPython(t), "-B", filepath.Join(root, "app", "verify.py"), "--config", configPath, "--mode", "config").CombinedOutput(); err != nil {
				t.Fatalf("offline verifier refuses rendered input: %v %s", err, out)
			}
			var config struct {
				Environments map[string]struct{ EgressAddress, CIDR, Pool string }
				Domain       string
			}
			if err := json.Unmarshal([]byte(configMap["data"].(map[string]any)["config.json"].(string)), &config); err != nil {
				t.Fatal(err)
			}
			for _, env := range []string{"staging", "production"} {
				for _, kindName := range []string{"Deployment/app", "Pod/probe", "Service/app", "Route/app", "NetworkPolicy/isolate", "NetworkPolicy/egress", "EgressFirewall/default"} {
					parts := strings.Split(kindName, "/")
					if objects[parts[0]+"/"+env+"/"+parts[1]] == nil {
						t.Fatalf("missing %s in %s", kindName, env)
					}
				}
				egress := objects["EgressIP//"+env]["spec"].(map[string]any)
				ips := egress["egressIPs"].([]any)
				if len(ips) != 1 || ips[0] != config.Environments[env].EgressAddress {
					t.Fatalf("egress identity differs from verifier configuration: %v", ips)
				}
				app := objects["Deployment/"+env+"/app"]["spec"].(map[string]any)["template"].(map[string]any)
				if app["metadata"].(map[string]any)["annotations"] != nil {
					t.Fatal("app bypasses primary-network policies through annotations")
				}
				probe := objects["Pod/"+env+"/probe"]
				if probe["metadata"].(map[string]any)["annotations"] != nil {
					t.Fatal("probe bypasses primary-network policies through annotations")
				}
				ingress := objects["IngressController/openshift-ingress-operator/"+env]["spec"].(map[string]any)
				lb := ingress["endpointPublishingStrategy"].(map[string]any)["loadBalancer"].(map[string]any)
				if ranges := lb["allowedSourceRanges"].([]any); len(ranges) != 1 || ranges[0] != config.Environments[env].CIDR {
					t.Fatalf("ingress source scope differs: %v", ranges)
				}
			}
			mc := objects["MachineConfig//99-sno-e2e-isolation"]["spec"].(map[string]any)["config"].(map[string]any)
			file := mc["storage"].(map[string]any)["files"].([]any)[0].(map[string]any)
			source := file["contents"].(map[string]any)["source"].(string)
			rules, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(source, "data:text/plain;base64,"))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(rules, []byte("flush ruleset")) {
				t.Fatal("fixture modifies unrelated firewall tables")
			}
			for _, required := range []string{"hook prerouting priority -150", "ct direction reply", "counter drop", config.Environments["staging"].EgressAddress, config.Environments["production"].Pool} {
				if !bytes.Contains(rules, []byte(required)) {
					t.Fatalf("filter lacks %q", required)
				}
			}
		})
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return value.(string)
}

func TestSNOIsolationVerifierFailures(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "e2e", "lab-sno-fips-controller", "app")
	cmd := exec.Command(pinnedPython(t), "-B", "-m", "unittest", "discover", "-s", root, "-p", "verify_test.py")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("verifier failure-path tests: %v\n%s", err, output)
	}
}

// Both networks must be realized by the provider, rather than requiring a
// manually created bridge; only their common NIC carriage is shared.
func TestSNOCaseOwnsIsolatedVLANNetworks(t *testing.T) {
	t.Parallel()
	for _, sets := range [][]string{
		nil,
		{"environments.trunk.bridge=br-fixture", "environments.staging.vlan=110", "environments.production.vlan=120"},
		{"environments.trunk.hostAddressing=static", "environments.staging.hostAddress=192.0.2.1", "environments.production.hostAddress=203.0.113.1"},
	} {
		wantMode := "none"
		if slices.Contains(sets, "environments.trunk.hostAddressing=static") {
			wantMode = "static"
		}
		render := e2eRender{name: "managed VLANs", sets: append(fipsControllerSubscriptions(), sets...)}
		for _, directory := range renderE2ECase(t, e2eRenderScript(t), "lab-sno-fips-controller", render) {
			if filepath.Base(directory) != "fips" {
				continue
			}
			sources, err := (inputfs.Reader{}).Read(context.Background(), []string{directory})
			if err != nil {
				t.Fatal(err)
			}
			state, _ := compileAcceptance(t, sources)
			catalog := state.Effective()
			staging := requireObject(t, catalog, api.Network, "staging")
			production := requireObject(t, catalog, api.Network, "production")
			for _, network := range []api.Object{staging, production} {
				spec := network.Spec()
				if spec.Get("provided").Bool() || spec.Get("libvirt", "providerRef").Text() != "lab-libvirt" {
					t.Fatalf("%s is not provider-managed", network.Name())
				}
				if spec.Get("libvirt", "forward").Text() != "none" || spec.Get("libvirt", "routedNetworkRefs").Len() != 0 {
					t.Fatalf("%s routes or translates environment traffic", network.Name())
				}
				if spec.Has("ipv4", "gateway") || spec.Get("libvirt", "hostAddressing").Text() != wantMode || spec.Get("libvirt").Has("hostAddress") != (wantMode == "static") {
					t.Fatalf("%s does not select %s host addressing", network.Name(), wantMode)
				}
			}
			if staging.Spec().Get("libvirt", "bridge").Text() != production.Spec().Get("libvirt", "bridge").Text() {
				t.Fatal("environment networks do not share a managed trunk")
			}
			refs := staging.Spec().Get("libvirt", "taggedNetworkRefs").Strings()
			if len(refs) != 1 || refs[0] != "production" {
				t.Fatalf("staging segment carriage = %v", refs)
			}
			provider := requireObject(t, catalog, api.InfraProvider, "lab-libvirt")
			for _, segment := range provider.Spec().Get("segments").Items() {
				if segment.Get("libvirt", "bridge").Text() == staging.Spec().Get("libvirt", "bridge").Text() {
					t.Fatal("an external segment still claims the managed trunk")
				}
			}
			machine := requireObject(t, catalog, api.Machine, "sno-01")
			nics := machine.Spec().Get("hardware", "nics").Items()
			if len(nics) != 2 || nics[1].Get("segmentRef").Text() != "staging" {
				t.Fatal("node does not attach to the managed tagged segment")
			}
			cluster := requireObject(t, catalog, api.OpenShiftCluster, "sno")
			labels := cluster.Spec().Get("nodes").Items()[0].Get("labels")
			if !labels.Has("k8s.ovn.org/egress-assignable") || labels.Get("k8s.ovn.org/egress-assignable").Text() != "" {
				t.Fatal("egress eligibility still needs a manual node label")
			}
		}
	}
}

// The resolver installed on the future controller remains usable for generic
// bootstrap downloads, then forwards cluster names to the second context.
func TestSNOCaseRoutesControllerDNSWithoutAResolverSwitch(t *testing.T) {
	t.Parallel()
	for _, sets := range [][]string{nil, {"host.domain=other.example.test", "controller.address=198.51.100.12", "host.upstreamDNS[0]=9.9.9.9"}} {
		catalogs := map[string]api.Catalog{}
		for _, directory := range renderE2ECase(t, e2eRenderScript(t), "lab-sno-fips-controller", e2eRender{name: "DNS chain", sets: append(fipsControllerSubscriptions(), sets...)}) {
			sources, err := (inputfs.Reader{}).Read(context.Background(), []string{directory})
			if err != nil {
				t.Fatal(err)
			}
			state, _ := compileAcceptance(t, sources)
			catalogs[filepath.Base(directory)] = state.Effective()
		}
		bootstrap, fips := catalogs["bootstrap"], catalogs["fips"]
		installed := requireObject(t, bootstrap, api.Machine, "fips-controller")
		network := requireObject(t, bootstrap, api.NetworkConfig, installed.Spec().Get("network", "configRef").Text())
		if network.Spec().Get("dns").Items()[0].Get("serverRef").Text() != "lab-dns" {
			t.Fatal("controller no longer uses bootstrap DNS")
		}
		dns := requireObject(t, bootstrap, api.DNSServer, "lab-dns")
		environment := requireObject(t, fips, api.Environment, "lab-sno-fips-controller")
		controller := requireObject(t, fips, api.Machine, "controller")
		target := ""
		for _, address := range controller.Spec().Get("network", "addresses").Items() {
			if address.Get("name").Text() == "ip" {
				target = address.Get("address").Text()
			}
		}
		var frozen managedservice.Request
		if err := dnsserver.Definition(nil).Extend(bootstrap, dns.Spec(), &frozen); err != nil {
			t.Fatal(err)
		}
		if len(frozen.ForwardZones) != 1 {
			t.Fatal("bootstrap DNS has no single cluster forwarding zone")
		}
		zone := frozen.ForwardZones[0]
		if zone.Domain != "sno."+environment.Spec().Get("domains", "base").Text() || len(zone.Forwarders) != 1 || zone.Forwarders[0] != target {
			t.Fatalf("wrong cluster DNS route: %+v", zone)
		}
		localDNS := requireObject(t, fips, api.DNSServer, "lab-dns")
		upstreams := localDNS.Spec().Get("forwarders").Strings()
		if !slices.Equal(upstreams, dns.Spec().Get("forwarders").Strings()) {
			t.Fatal("the two resolvers do not use the same declared upstreams")
		}
		if slices.Contains(upstreams, dns.Spec().Get("bindAddress").Text()) || slices.Contains(upstreams, target) {
			t.Fatal("cluster DNS forwards back into the bootstrap chain")
		}
	}
}
