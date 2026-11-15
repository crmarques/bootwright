//go:build linux && amd64

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestSNOIsolationFixtures(t *testing.T) {
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
			out, err := exec.Command("helm", argv...).CombinedOutput()
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
	root := filepath.Join("..", "..", "e2e", "lab-sno-fips-controller", "app")
	cmd := exec.Command("python3", "-B", "-m", "unittest", "discover", "-s", root, "-p", "verify_test.py")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("verifier failure-path tests: %v\n%s", err, output)
	}
}
