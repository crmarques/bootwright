package containercluster_test

import (
	"context"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

const clusterYAML = `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: example
spec:
  domains:
    base: example.test
---
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: host-provider
spec:
  kubevirt:
    kubeconfigRef: host-credentials
    namespace: workloads
    machineProfiles:
      - name: standard
        cpu: 4
        memoryMiB: 8192
        diskGiB: 80
  networkAttachments:
    - name: workload
      kubevirt:
        networkRef:
          name: workload
---
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: node
spec:
  capabilities: [openshift-node]
  substrate:
    providerRef: host-provider
    profileRef: standard
  os:
    provided: false
  network:
    inline:
      machineNetwork:
        - cidr: 192.0.2.0/24
      nmstate:
        interfaces:
          - name: eth0
            type: ethernet
    attachmentRef: workload
    addresses:
      - name: primary
        address: 192.0.2.10/24
        interface: eth0
---
apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata:
  name: cluster
spec:
  distribution:
    release:
      version: 4.99.1
  install:
    endpoints:
      api:
        source:
          type: node
      ingress:
        source:
          type: node
  nodes:
    - name: master
      role: master
      machineRef: node
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: host-credentials
spec:
  type: opaque
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: openshift-pull-secret
spec:
  type: dockerConfigJson
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: cluster-cluster-admin-ssh-key
spec:
  type: sshKeyPair
`

func compiler() compilation.Compiler {
	return compilation.NewCompiler(yamlstream.Parser{}, nil,
		compilation.Rules{Normalize: environment.Normalize, Validate: environment.Validate},
		compilation.Rules{Normalize: secrets.Normalize, ValidateAuthored: secrets.ValidateAuthored, Validate: secrets.Validate},
		compilation.Rules{Normalize: substrate.Normalize, ValidateAuthored: substrate.ValidateAuthored, Validate: substrate.Validate},
		compilation.Rules{Normalize: machine.Normalize, ValidateAuthored: machine.ValidateAuthored, Validate: machine.Validate},
		compilation.Rules{Normalize: managedos.Normalize, ValidateAuthored: managedos.ValidateAuthored, Validate: managedos.Validate},
		compilation.Rules{Normalize: infrastructureservices.Normalize, ValidateAuthored: infrastructureservices.ValidateAuthored, Validate: infrastructureservices.Validate},
		compilation.Rules{Normalize: containercluster.Normalize, ValidateAuthored: containercluster.ValidateAuthored, Validate: containercluster.Validate},
	)
}
func sources(content string) desiredstate.Sources {
	return desiredstate.Sources{Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(content))}, Roots: []string{"/synthetic"}}
}

func TestCompleteContainerGraphCompilation(t *testing.T) {
	state, _, err := compiler().Compile(context.Background(), sources(clusterYAML))
	if err != nil {
		t.Fatal(desiredstate.DiagnosticsOf(err))
	}
	cluster, ok := state.Effective().Find(api.ContainerCluster, "cluster")
	if !ok {
		t.Fatal("cluster missing")
	}
	if cluster.Spec().Get("install", "platform", "type").Text() != "none" || cluster.Spec().Get("install", "endpoints", "api", "address").Text() != "192.0.2.10" {
		t.Fatal("effective container fields missing")
	}
	authored, _ := state.Authored().Find(api.ContainerCluster, "cluster")
	if authored.Spec().Has("install", "endpoints", "api", "address") || authored.Spec().Has("install", "nodeSSH") {
		t.Fatal("authored container changed")
	}
	if cluster.Spec().Get("networking", "clusterNetwork").Items()[0].Get("hostPrefix").Text() != "23" {
		t.Fatal("schema-dependent network default missing")
	}
}

func TestContainerSchemaAndAuthoredFailures(t *testing.T) {
	cases := map[string]string{
		"retired pool":             strings.Replace(clusterYAML, "  nodes:\n", "  controlPlane: {}\n  nodes:\n", 1),
		"nonmatching platform":     strings.Replace(clusterYAML, "  install:\n    endpoints:", "  install:\n    platform:\n      type: none\n      vsphere: {}\n    endpoints:", 1),
		"derived address as input": strings.Replace(clusterYAML, "      api:\n        source:", "      api:\n        address: 192.0.2.10\n        source:", 1),
		"mixed node SSH":           strings.Replace(clusterYAML, "  install:\n    endpoints:", "  install:\n    nodeSSH:\n      keyPairRef: cluster-cluster-admin-ssh-key\n      publicKeyRef: cluster-cluster-admin-ssh-key\n    endpoints:", 1),
		"wrong pull Secret":        strings.Replace(clusterYAML, "  type: dockerConfigJson", "  type: opaque", 1),
		"unpinned image":           strings.Replace(clusterYAML, "      version: 4.99.1", "      image: quay.io/example/release:latest", 1),
		"PCR fields":               strings.Replace(clusterYAML, "  nodes:\n", "  security:\n    diskEncryption:\n      unlock:\n        tpm2:\n          pcrIds: [7]\n  nodes:\n", 1),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			state, report, err := compiler().Compile(context.Background(), sources(content))
			if err == nil || state != nil || report != nil {
				t.Fatal("invalid compilation succeeded")
			}
		})
	}
}

func TestSplitPublicSSHAndNativeExternalPlatform(t *testing.T) {
	content := strings.Replace(clusterYAML, "  install:\n    endpoints:", "  install:\n    nodeSSH:\n      publicKeyRef: cluster-cluster-admin-ssh-key\n    platform:\n      type: external\n      external:\n        name: custom\n        settings:\n          enabled: true\n          count: 2\n          ratio: 0.25\n    endpoints:", 1)
	state, _, err := compiler().Compile(context.Background(), sources(content))
	if err != nil {
		t.Fatal(desiredstate.DiagnosticsOf(err))
	}
	cluster, _ := state.Effective().Find(api.ContainerCluster, "cluster")
	if cluster.Spec().Get("install", "nodeSSH").Has("keyPairRef") || cluster.Spec().Get("install", "nodeSSH").Has("privateKeyRef") {
		t.Fatal("public-only SSH acquired private material")
	}
	if cluster.Spec().Get("install", "platform", "external", "settings", "ratio").Type() != api.Number {
		t.Fatal("native external number not retained")
	}
}

func TestExplicitEndpointAndPlatformChoicesSuppressDefaults(t *testing.T) {
	defaults := `    base: example.test
  defaults:
    ContainerCluster:
      install:
        platform:
          type: baremetal
          baremetal:
            provisioningNetwork: disabled
        endpoints:
          api:
            address: 192.0.2.2
            source:
              type: external
          ingress:
            source:
              type: infraComponent
              componentRef: unused-lb
              bindAddressRef: unused
`
	// The inherited endpoint fragment is partial; the explicit source controls
	// which of its conditional fields can reach this recipient.
	content := strings.Replace(clusterYAML, "    base: example.test\n", defaults, 1)
	content = strings.Replace(content, "  install:\n    endpoints:", "  install:\n    platform:\n      type: none\n    endpoints:", 1)
	state, _, err := compiler().Compile(context.Background(), sources(content))
	if err != nil {
		t.Fatal(desiredstate.DiagnosticsOf(err))
	}
	cluster, _ := state.Effective().Find(api.ContainerCluster, "cluster")
	if cluster.Spec().Get("install", "platform").Has("baremetal") {
		t.Fatal("inactive platform defaults inherited")
	}
	endpoint := cluster.Spec().Get("install", "endpoints", "api")
	if endpoint.Get("address").Text() != "192.0.2.10" || endpoint.Get("source").Has("componentRef") || endpoint.Get("source").Has("bindAddressRef") {
		t.Fatal("inactive endpoint defaults inherited")
	}
	if cluster.Spec().Get("install", "endpoints", "ingress", "source").Has("componentRef") {
		t.Fatal("inactive component selection inherited")
	}
}
