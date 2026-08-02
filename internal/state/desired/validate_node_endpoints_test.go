package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

const nodeSourceEndpointsYAML = `    api:
      source:
        type: node
    api-int:
      source:
        type: node
    apps:
      source:
        type: node
`

func nodeEndpointBaselineFiles(t *testing.T) map[string]string {
	t.Helper()
	files := newBaselineFiles()
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"],
		`  network:
    config:
      networkConfigRef: cluster-net
      attachmentRef: cluster-net
      overrides:
        interfaces:
          - name: primary
            ipv4:
              address:
                - { ip: 192.168.132.20, prefix-length: 24 }`,
		`  network:
    config:
      networkConfigRef: cluster-net
      attachmentRef: cluster-net
      interfaceAddresses:
        - interface: primary
          addressRef: ip
          prefixLength: 24`, 1)
	if strings.Contains(files["cluster.yaml"], "overrides:") {
		t.Fatal("the baseline machine network block moved; the node-endpoint fixture no longer rewrites it")
	}
	files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], nodeSourceEndpointsYAML)
	return files
}

func loadNodeEndpointCluster(t *testing.T, files map[string]string) (v1alpha1.State, error) {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, files)
	return LoadNormalizeValidate([]string{dir})
}

func TestNodeEndpointSourceResolvesTheNodeInstallAddress(t *testing.T) {
	state, err := loadNodeEndpointCluster(t, nodeEndpointBaselineFiles(t))
	if err != nil {
		t.Fatalf("a single-node cluster sourcing its endpoints from the node must validate: %v", err)
	}
	if len(state.ContainerClusters) != 1 {
		t.Fatalf("expected one ContainerCluster, got %d", len(state.ContainerClusters))
	}
	for _, name := range []string{v1alpha1.EndpointAPI, v1alpha1.EndpointAPIInt, v1alpha1.EndpointIngress} {
		endpoint := state.ContainerClusters[0].Spec.Install.Endpoints[name]
		if endpoint.Source.Type != v1alpha1.EndpointSourceNode {
			t.Fatalf("endpoint %s kept source.type %q", name, endpoint.Source.Type)
		}
		if endpoint.Address != "192.168.132.20" {
			t.Fatalf("normalize must materialize endpoint %s to the node install address, got %q", name, endpoint.Address)
		}
	}
}

func TestNodeEndpointSourceRejectedOnMultiNodeCluster(t *testing.T) {
	files := nodeEndpointBaselineFiles(t)
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"],
		`  nodes:
    - name: master-0`,
		`  nodes:
    - name: master-1
      role: master
      machineRef: srv1
    - name: master-0`, 1)
	_, err := loadNodeEndpointCluster(t, files)
	if err == nil {
		t.Fatal("source.type=node on a multi-node cluster must be rejected")
	}
	if !strings.Contains(err.Error(), "source.type=node is valid only on a cluster with exactly one node") {
		t.Fatalf("the refusal must name the reason, got: %v", err)
	}
}

func TestNodeEndpointSourceForbidsAnAuthoredAddress(t *testing.T) {
	files := nodeEndpointBaselineFiles(t)
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"],
		`      api:
        source:
          type: node`,
		`      api:
        address: 192.168.132.20
        source:
          type: node`, 1)
	_, err := loadNodeEndpointCluster(t, files)
	if err == nil {
		t.Fatal("source.type=node must forbid an authored address on the same slot")
	}
	if !strings.Contains(err.Error(), "address must be empty when source.type=node") {
		t.Fatalf("the refusal must name the forbidden field, got: %v", err)
	}
}

func TestNodeEndpointSourceRefusesAnUnresolvableInstallAddress(t *testing.T) {
	files := nodeEndpointBaselineFiles(t)
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"],
		`      interfaceAddresses:
        - interface: primary
          addressRef: ip
          prefixLength: 24
`, "", 1)
	_, err := loadNodeEndpointCluster(t, files)
	if err == nil {
		t.Fatal("source.type=node must fail closed when the node declares no install address")
	}
	if !strings.Contains(err.Error(), "does not resolve to an install address") {
		t.Fatalf("the refusal must name the missing input, got: %v", err)
	}
}

func TestNodeEndpointSourceRefusesAmbiguousInstallAddresses(t *testing.T) {
	files := nodeEndpointBaselineFiles(t)
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"],
		`      interfaceAddresses:
        - interface: primary
          addressRef: ip
          prefixLength: 24`,
		`      interfaceAddresses:
        - interface: primary
          addressRef: ip
          prefixLength: 24
        - interface: secondary
          addressRef: ip2
          prefixLength: 24`, 1)
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"],
		"    - { name: ip, address: 192.168.132.20 }",
		`    - { name: ip, address: 192.168.132.20 }
    - { name: ip2, address: 192.168.132.21 }`, 1)
	_, err := loadNodeEndpointCluster(t, files)
	if err == nil {
		t.Fatal("source.type=node must fail closed when the node declares more than one install address")
	}
	if !strings.Contains(err.Error(), "resolves to 2 install addresses") {
		t.Fatalf("the refusal must name the ambiguity, got: %v", err)
	}
}
