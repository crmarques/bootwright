package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/environment/inspection"
)

func containerDetailsFixture(name, distribution, status string) *inspection.ContainerDetails {
	domain := name + ".example.test"
	result := &inspection.ContainerDetails{
		Console:      &inspection.ConsoleAccess{User: "kubeadmin"},
		Distribution: distribution, Release: "4.21.15", ReleaseImage: "quay.io/example/release@sha256:" + strings.Repeat("a", 64),
		Platform: "none", InstallMode: "connected", BaseDomain: "example.test", FIPS: distribution == "openshift",
		IngressDomain: "apps." + domain, NetworkType: "OVNKubernetes", Status: status,
		MachineNetworks: []string{"192.0.2.0/24"}, ClusterNetworks: []inspection.ContainerNetwork{{CIDR: "10.128.0.0/14", HostPrefix: 23}},
		ServiceNetworks: []string{"172.30.0.0/16"},
		Nodes:           []inspection.ContainerNode{{Name: "node-01", Role: "controlPlane", Machine: "host-0", FQDN: "node-01." + domain, Address: "192.0.2.10"}},
		Endpoints: []inspection.ContainerEndpoint{
			{Service: "api", URL: "https://api." + domain + ":6443", Address: "192.0.2.10", Source: "node"},
			{Service: "api-int", URL: "https://api-int." + domain + ":6443", Address: "192.0.2.10", Source: "node"},
			{Service: "console", URL: "https://console-openshift-console.apps." + domain, Address: "192.0.2.10", Source: "node"},
		},
	}
	if status != inspection.StatusReady {
		result.Reason = "no completed installation is recorded in this context"
		result.Remedy = "bootwright apply --context lab --stage clusters"
	}
	return result
}

func TestContainerInfoKeepsConfiguredURLsAndTopologyInEveryPresentation(t *testing.T) {
	result := clusterInfoResult(readyCeph(), inspection.AccessCommand{Status: inspection.StatusReady}, missingKubeconfig())
	result.Clusters = result.Clusters[1:]
	result.Storage = nil
	for _, format := range []string{"text", "json", "yaml"} {
		for _, detail := range []string{"compact", "full"} {
			var out bytes.Buffer
			writer := WithPresentation(&out, PresentationOptions{Format: format, Detail: detail})
			if err := writeClusterInfo(writer, io.Discard, "cluster info", result, format != "text"); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"https://api.okd-lab.example.test:6443", "https://console-openshift-console.apps.okd-lab.example.test",
				"node-01.okd-lab.example.test", "controlPlane", "192.0.2.10", "OVNKubernetes", "10.128.0.0/14", "172.30.0.0/16", "unavailable"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("%s/%s lacks %q: %s", format, detail, want, out.String())
				}
			}
		}
	}
}
