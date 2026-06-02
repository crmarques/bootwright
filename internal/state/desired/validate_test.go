package desiredstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestGoodFixtures(t *testing.T) {
	entries, err := os.ReadDir("testdata/good")
	if err != nil {
		t.Fatalf("read testdata/good: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			_, err := LoadNormalizeValidate([]string{filepath.Join("testdata/good", name)})
			if err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}
		})
	}
}

func TestCanonicalExamples(t *testing.T) {
	examplesRoot := filepath.Join("..", "..", "..", "examples")
	entries, err := os.ReadDir(examplesRoot)
	if err != nil {
		t.Fatalf("read examples: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			_, err := LoadNormalizeValidate([]string{filepath.Join(examplesRoot, name)})
			if err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}
		})
	}
}

func TestOpenShiftManagedVIPFixture(t *testing.T) {
	_, err := LoadNormalizeValidate([]string{filepath.Join("testdata/good", "005-3nodes-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
}

func TestContainerClusterNetworkingValidation(t *testing.T) {
	cases := []struct {
		name          string
		clusterYAML   string
		wantSubstring string
	}{
		{
			name: "networking-required",
			clusterYAML: strings.Replace(newClusterYAML,
				"  networking:\n    clusterNetwork: [{ cidr: 10.128.0.0/14, hostPrefix: 23 }]\n    serviceNetwork: [172.30.0.0/16]\n", "", 1),
			wantSubstring: "ContainerCluster/sno spec.networking is required",
		},
		{
			name:          "cluster-network-cidr-invalid",
			clusterYAML:   strings.Replace(newClusterYAML, "10.128.0.0/14", "not-a-cidr", 1),
			wantSubstring: `spec.networking.clusterNetwork[0].cidr "not-a-cidr" is not a valid CIDR`,
		},
		{
			name:          "cluster-network-host-prefix-required",
			clusterYAML:   strings.Replace(newClusterYAML, ", hostPrefix: 23", "", 1),
			wantSubstring: "spec.networking.clusterNetwork[0].hostPrefix is required",
		},
		{
			name:          "cluster-network-host-prefix-bounds",
			clusterYAML:   strings.Replace(newClusterYAML, "hostPrefix: 23", "hostPrefix: 14", 1),
			wantSubstring: "spec.networking.clusterNetwork[0].hostPrefix 14 must be greater than CIDR prefix length 14",
		},
		{
			name:          "service-network-required",
			clusterYAML:   strings.Replace(newClusterYAML, "    serviceNetwork: [172.30.0.0/16]\n", "", 1),
			wantSubstring: "ContainerCluster/sno spec.networking.serviceNetwork is required",
		},
		{
			name:          "service-network-cidr-invalid",
			clusterYAML:   strings.Replace(newClusterYAML, "172.30.0.0/16", "bad", 1),
			wantSubstring: `spec.networking.serviceNetwork[0] "bad" is not a valid CIDR`,
		},
		{
			name:          "network-type-whitespace",
			clusterYAML:   strings.Replace(newClusterYAML, "  networking:\n", "  networking:\n    networkType: \" OVNKubernetes\"\n", 1),
			wantSubstring: `spec.networking.networkType " OVNKubernetes" must not contain leading or trailing whitespace`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newBaselineFiles()
			files["cluster.yaml"] = tc.clusterYAML
			writeFiles(t, dir, files)
			_, err := LoadNormalizeValidate([]string{dir})
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
			}
		})
	}
}

func TestValidationErrorCarriesStructuredDiagnostics(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["cluster.yaml"] = strings.Replace(newClusterYAML, "10.128.0.0/14", "not-a-cidr", 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	diagnostics := Diagnostics(err)
	if len(diagnostics) == 0 {
		t.Fatalf("expected diagnostics for %v", err)
	}
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Object == "ContainerCluster/sno" &&
			diagnostic.Field == "spec.networking.clusterNetwork[0].cidr" &&
			diagnostic.Value == "not-a-cidr" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("diagnostics do not identify the invalid cluster CIDR: %+v", diagnostics)
	}
}

func TestBareMetalMachineNetworkTemplateRequiresDeclaredInterfaces(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["network.yaml"] = `apiVersion: bootwright.io/v1alpha1
kind: NetworkConfig
metadata: { name: cluster-net }
spec:
  machineNetwork:
    - { cidr: 192.168.132.0/24 }
  template:
    networkConfig:
      interfaces:
        - { name: primary, type: ethernet, controller: bond0, state: up, ipv4: { enabled: false }, ipv6: { enabled: false } }
        - { name: secondary, type: ethernet, controller: bond0, state: up, ipv4: { enabled: false }, ipv6: { enabled: false } }
        - name: bond0
          type: bond
          state: up
          link-aggregation:
            mode: 802.3ad
            port: [primary, secondary]
          ipv4: { enabled: false }
          ipv6: { enabled: false }
        - name: bond0.132
          type: vlan
          state: up
          vlan: { base-iface: bond0, id: 132 }
          ipv4: { enabled: true, dhcp: false }
          ipv6: { enabled: false }
      routes:
        config:
          - { destination: 0.0.0.0/0, next-hop-address: 192.168.132.1, next-hop-interface: bond0.132, table-id: 254 }
`
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"], "              - name: primary", "              - name: bond0.132", 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected missing baremetal interface error, got nil")
	}
	want := `spec.components.machines[master-0].networkConfig.ref "cluster-net" requires baremetal interface "secondary" but InfraProvider/rack spec.machines[srv1].baremetal.interfaces does not declare it`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestClusterMachineNetworkConfigAcceptsInlineSpec(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	delete(files, "network.yaml")
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"], baselineMachineNetworkConfigYAML(), inlineMachineNetworkConfigYAML(""), 1)
	writeFiles(t, dir, files)
	if _, err := LoadNormalizeValidate([]string{dir}); err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
}

func TestClusterMachineNetworkConfigRejectsRefAndSpec(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"], "          ref: { name: cluster-net }\n", "          ref: { name: cluster-net }\n"+inlineNetworkConfigSpecYAML(), 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected ref plus spec validation error, got nil")
	}
	want := "networkConfig must set exactly one of {ref, spec}"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestClusterMachineNetworkConfigRejectsInlineSpecOverrides(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	delete(files, "network.yaml")
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"], baselineMachineNetworkConfigYAML(), inlineMachineNetworkConfigYAML(`          overrides:
            interfaces:
              - name: primary
                ipv4:
                  address:
                    - { ip: 192.168.132.20, prefix-length: 24 }
`), 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected inline spec overrides validation error, got nil")
	}
	want := "networkConfig.overrides is only valid with networkConfig.ref"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestProviderMachineLabelsValidation(t *testing.T) {
	cases := []struct {
		name          string
		labelsYAML    string
		wantSubstring string
	}{
		{
			name: "valid-labels",
			labelsYAML: `        datacenter: dc1
        topology.kubernetes.io/zone: dc1-a
`,
		},
		{
			name: "invalid-key",
			labelsYAML: `        bad key: dc1
`,
			wantSubstring: `labels["bad key"] "bad key" is not a valid label key`,
		},
		{
			name: "invalid-value",
			labelsYAML: `        datacenter: "dc1/room"
`,
			wantSubstring: `labels["datacenter"] value "dc1/room" is not a valid label value`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newBaselineFiles()
			files["provider.yaml"] = strings.Replace(newProviderYAML, "    - name: srv1\n", "    - name: srv1\n      labels:\n"+tc.labelsYAML, 1)
			writeFiles(t, dir, files)
			state, err := LoadNormalizeValidate([]string{dir})
			if tc.wantSubstring == "" {
				if err != nil {
					t.Fatalf("LoadNormalizeValidate: %v", err)
				}
				if got := state.InfraProviders[0].Spec.Machines[0].Labels["datacenter"]; got != "dc1" {
					t.Fatalf("datacenter label got %q, want dc1", got)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSubstring)
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
			}
		})
	}
}

func TestArtifactServerEndpointNamesAreRouteSelectors(t *testing.T) {
	files := newBaselineFiles()
	files["hosts.yaml"] = strings.Replace(files["hosts.yaml"],
		"    - name: bmc-lan\n      address: 192.168.132.1",
		"    - name: bmc-lan\n      address: 192.168.132.1\n    - name: dnsAlias\n      address: artifact.example.test", 1)
	files["infra-component.yaml"] = strings.Replace(files["infra-component.yaml"],
		"name: bmc\n        listener: https\n        hostAddress: bmc-lan",
		"name: dnsAlias\n        listener: https\n        hostAddress: dnsAlias", 1)
	files["environment.yaml"] = strings.Replace(files["environment.yaml"],
		"endpoint: bmc",
		"endpoint: dnsAlias", 1)

	dir := t.TempDir()
	writeFiles(t, dir, files)
	if _, err := LoadNormalizeValidate([]string{dir}); err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
}

func TestSchemaRefactorValidation(t *testing.T) {
	cases := []struct {
		name          string
		files         map[string]string
		wantSubstring string
	}{
		{
			name: "old-network-kind-rejected",
			files: map[string]string{"network.yaml": `apiVersion: bootwright.io/v1alpha1
kind: Network
metadata: { name: old }
spec: {}
`},
			wantSubstring: `unsupported kind "Network"`,
		},
		{
			name: "infraprovider-capabilities-wrapper-rejected",
			files: map[string]string{"provider.yaml": `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata: { name: rack }
spec:
  capabilities: {}
`},
			wantSubstring: "field capabilities not found",
		},
		{
			name: "infraprovider-networkref-rejected",
			files: map[string]string{"provider.yaml": strings.Replace(newProviderYAML,
				"interfaces:\n          - { name: primary, macAddress: 52:54:00:32:11:10 }",
				"interfaces:\n          - { name: primary, macAddress: 52:54:00:32:11:10, networkRef: { name: cluster-net } }", 1)},
			wantSubstring: "field networkRef not found",
		},
		{
			name: "containercluster-infrastructureref-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"distribution:",
				"infrastructureRef: { name: sno }\n  distribution:", 1)},
			wantSubstring: "field infrastructureRef not found",
		},
		{
			name: "containercluster-installmode-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"  install:\n",
				"  installMode: connected\n  install:\n", 1)},
			wantSubstring: "field installMode not found",
		},
		{
			name: "containercluster-sshkeyref-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"    pullSecretRef: { name: openshift-pull-secret }",
				"    pullSecretRef: { name: openshift-pull-secret }\n    sshKeyRef: { name: cluster-admin-ssh-key }", 1)},
			wantSubstring: "field sshKeyRef not found",
		},
		{
			name: "containercluster-clusteradminssh-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"    pullSecretRef: { name: openshift-pull-secret }",
				"    pullSecretRef: { name: openshift-pull-secret }\n    clusterAdminSSH:\n      keyPairRef: { name: cluster-admin-ssh-key }", 1)},
			wantSubstring: "field clusterAdminSSH not found",
		},
		{
			name: "environment-default-clusteradminssh-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"  baseDomain: bootwright.test\n",
				"  baseDomain: bootwright.test\n  defaults:\n    install:\n      clusterAdminSSH:\n        keyPairRef: { name: cluster-admin-ssh-key }\n", 1)},
			wantSubstring: "field clusterAdminSSH not found",
		},
		{
			name: "host-ssh-address-rejected",
			files: map[string]string{"hosts.yaml": strings.Replace(newHostsYAML,
				"addressName: ssh",
				"address: 192.168.132.1", 1)},
			wantSubstring: "field address not found",
		},
		{
			name: "host-service-addresses-rejected",
			files: map[string]string{"hosts.yaml": strings.Replace(newHostsYAML,
				"capabilities: [container-runtime]",
				"capabilities: [container-runtime]\n  serviceAddresses:\n    bmc: 192.168.132.1", 1)},
			wantSubstring: "field serviceAddresses not found",
		},
		{
			name: "host-duplicate-address-name-rejected",
			files: map[string]string{"hosts.yaml": strings.Replace(newHostsYAML,
				"addresses:\n    - name: ssh\n      address: 192.168.132.1",
				"addresses:\n    - name: ssh\n      address: 192.168.132.1\n    - name: ssh\n      address: 192.168.132.2", 1)},
			wantSubstring: `spec.addresses[1].name "ssh" is duplicated`,
		},
		{
			name: "host-missing-ssh-address-name-rejected",
			files: map[string]string{"hosts.yaml": strings.Replace(newHostsYAML,
				"addressName: ssh",
				"addressName: missing", 1)},
			wantSubstring: `spec.ssh.addressName "missing" does not resolve`,
		},
		{
			name: "containercluster-role-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"distribution:",
				"role: managed\n  distribution:", 1)},
			wantSubstring: "field role not found",
		},
		{
			name: "clusterinfra-artifacts-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"---\napiVersion: bootwright.io/v1alpha1\nkind: ContainerCluster",
				"    artifacts: { from: { provider: rack, name: default } }\n---\napiVersion: bootwright.io/v1alpha1\nkind: ContainerCluster", 1)},
			wantSubstring: "field artifacts not found",
		},
		{
			name: "uppercase-provisioning-network-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"provisioningNetwork: disabled",
				"provisioningNetwork: Disabled", 1)},
			wantSubstring: `provisioningNetwork "Disabled" must be one of {disabled, managed, unmanaged}`,
		},
		{
			name: "baremetal-artifact-server-required",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"  infraComponents:\n    artifactServers:\n      - name: default\n        type: managed\n        componentRef:\n          name: artifact-server\n        routes:\n          redfishVirtualMedia:\n            endpoint: bmc\n\n", "", 1)},
			wantSubstring: "requires generated artifact publication; set Environment.spec.infraComponents.artifactServers",
		},
		{
			name: "environment-top-level-ntpsources-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"  infraComponents:\n",
				"  ntpSources:\n    - 192.168.132.1\n\n  infraComponents:\n", 1)},
			wantSubstring: "field ntpSources not found",
		},
		{
			name: "environment-clustertrust-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"  secrets:\n",
				"  clusterTrust:\n    caBundleRefs:\n      - name: corp-ca\n\n  secrets:\n", 1)},
			wantSubstring: "field clusterTrust not found",
		},
		{
			name: "environment-infra-ntpsource-invalid-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"  infraComponents:\n",
				"  infraComponents:\n    ntpSources:\n      - \" ntp.example.test\"\n", 1)},
			wantSubstring: `spec.infraComponents.ntpSources[0] " ntp.example.test" must not contain leading or trailing whitespace`,
		},
		{
			name: "artifact-server-listener-port-out-of-range-rejected",
			files: map[string]string{"infra-component.yaml": strings.Replace(newInfraComponentYAML,
				"port: 8443",
				"port: 70000", 1)},
			wantSubstring: "spec.artifactServer.listeners[0].port 70000 out of range",
		},
		{
			name: "artifact-server-bind-address-rejected",
			files: map[string]string{"infra-component.yaml": strings.Replace(newInfraComponentYAML,
				"hostRef:\n      name: services-host",
				"hostRef:\n      name: services-host\n    bindAddress: invalid", 1)},
			wantSubstring: `spec.artifactServer.bindAddress "invalid" is not a valid IP address`,
		},
		{
			name: "artifact-server-endpoint-address-rejected",
			files: map[string]string{"infra-component.yaml": strings.Replace(newInfraComponentYAML,
				"hostAddress: bmc-lan",
				"hostAddress: missing", 1)},
			wantSubstring: `spec.artifactServer.endpoints[0].hostAddress "missing" does not resolve to Host/services-host spec.addresses[].name`,
		},
		{
			name: "artifact-server-endpoint-address-name-rejected",
			files: map[string]string{"infra-component.yaml": strings.Replace(newInfraComponentYAML,
				"hostAddress: bmc-lan",
				"addressName: bmc-lan", 1)},
			wantSubstring: "field addressName not found",
		},
		{
			name: "artifact-server-route-endpoint-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"endpoint: bmc",
				"endpoint: missing", 1)},
			wantSubstring: `routes.redfishVirtualMedia.endpoint "missing" does not resolve on selected InfraComponent spec.artifactServer.endpoints`,
		},
		{
			name: "multiple-clusterinfra-refs-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"nodes:\n    - hostname: master-0",
				"nodes:\n    - hostname: master-x\n      role: master\n      machineRef: { clusterInfra: other, name: master-x }\n    - hostname: master-0", 1)},
			wantSubstring: "spec.nodes must reference exactly one ClusterInfra",
		},
		{
			name: "endpoint-owner-required",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"api:\n      address: 192.168.132.10\n      source:\n        type: external",
				"api: {}", 1)},
			wantSubstring: "spec.endpoints.api must set address, dnsName, or source.type=infraComponent",
		},
		{
			name: "missing-machine-ref-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"name: master-0", "name: missing", 1)},
			wantSubstring: `machineRef.name "master-0" is not defined`,
		},
		{
			name: "openshift-pull-secret-required",
			files: map[string]string{
				"cluster.yaml":     strings.Replace(newClusterYAML, "pullSecretRef: { name: openshift-pull-secret }", "", 1),
				"environment.yaml": strings.Replace(newEnvironmentYAML, "    openshift-pull-secret:\n", "", 1),
			},
			wantSubstring: `install.pullSecretRef "openshift-pull-secret" is not declared`,
		},
		{
			name: "secret-keyfile-without-file-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"    provider-host-ssh: { file: ~/ssh }",
				"    provider-host-ssh: { keyFile: ~/ssh.key }", 1)},
			wantSubstring: "spec.secrets[provider-host-ssh].keyFile requires file",
		},
		{
			name: "secretstorage-mode-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"  baseDomain: bootwright.test\n",
				"  baseDomain: bootwright.test\n  secretStorage: { mode: invalid }\n", 1)},
			wantSubstring: `spec.secretStorage.mode "invalid" must be one of {source, context}`,
		},
		{
			name: "generated-ssh-key-type-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"    cluster-admin-ssh-key: { file: ~/ssh.pub }",
				"    cluster-admin-ssh-key: { generated: { sshKeyPair: { type: rsa } } }", 1)},
			wantSubstring: `spec.secrets[cluster-admin-ssh-key].generated.sshKeyPair.type "rsa" must be "ed25519"`,
		},
		{
			name: "generated-secret-multiple-kinds-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"    bmc-credentials:\n      generated: { credentials: { username: admin } }",
				"    bmc-credentials:\n      generated: { credentials: { username: admin }, sshKeyPair: { type: ed25519 } }", 1)},
			wantSubstring: "spec.secrets[bmc-credentials].generated sets more than one generated kind",
		},
		{
			name: "generated-non-ssh-key-for-ssh-ref-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"    cluster-admin-ssh-key: { file: ~/ssh.pub }",
				"    cluster-admin-ssh-key:\n      generated: { credentials: { username: admin } }", 1)},
			wantSubstring: `install.nodeSSH.keyPairRef "cluster-admin-ssh-key" uses generated material but Environment/env spec.secrets[cluster-admin-ssh-key].generated is not sshKeyPair`,
		},
		{
			name: "cluster-admin-ssh-mixed-refs-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"    pullSecretRef: { name: openshift-pull-secret }",
				"    pullSecretRef: { name: openshift-pull-secret }\n    nodeSSH:\n      keyPairRef: { name: cluster-admin-ssh-key }\n      publicKeyRef: { name: cluster-admin-ssh-key }", 1)},
			wantSubstring: "spec.install.nodeSSH must use either keyPairRef or publicKeyRef/privateKeyRef, not both",
		},
		{
			name: "cluster-admin-ssh-private-only-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"    pullSecretRef: { name: openshift-pull-secret }",
				"    pullSecretRef: { name: openshift-pull-secret }\n    nodeSSH:\n      privateKeyRef: { name: cluster-admin-ssh-key }", 1)},
			wantSubstring: "spec.install.nodeSSH publicKeyRef.name is required when keyPairRef.name is empty",
		},
		{
			name: "installtrust-duplicate-ref-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"  secrets:\n",
				"  installTrust:\n    caBundleRefs:\n      - name: corp-ca\n      - name: corp-ca\n\n  secrets:\n", 1)},
			wantSubstring: `spec.installTrust.caBundleRefs[1].name "corp-ca" is duplicated`,
		},
		{
			name: "installtrust-unknown-ref-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"  secrets:\n",
				"  installTrust:\n    caBundleRefs:\n      - name: corp-ca\n\n  secrets:\n", 1)},
			wantSubstring: `spec.installTrust.caBundleRefs[0] "corp-ca" is not declared`,
		},
		{
			name: "additionaltrust-duplicate-ref-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"    pullSecretRef: { name: openshift-pull-secret }\n",
				"    pullSecretRef: { name: openshift-pull-secret }\n    additionalTrustBundleRefs:\n      - name: corp-ca\n      - name: corp-ca\n", 1)},
			wantSubstring: `spec.install.additionalTrustBundleRefs[1].name "corp-ca" is duplicated`,
		},
		{
			name: "api-serving-cert-names-required",
			files: map[string]string{
				"environment.yaml": strings.Replace(newEnvironmentYAML, "    provider-host-ssh: { file: ~/ssh }\n", "    provider-host-ssh: { file: ~/ssh }\n    api-tls:\n", 1),
				"cluster.yaml": strings.Replace(newClusterYAML,
					"    pullSecretRef: { name: openshift-pull-secret }\n",
					"    pullSecretRef: { name: openshift-pull-secret }\n    servingCertificates:\n      apiServer:\n        namedCertificates:\n          - secretRef: { name: api-tls }\n", 1),
			},
			wantSubstring: "namedCertificates[0].names requires at least one DNS name",
		},
		{
			name: "api-serving-cert-api-int-rejected",
			files: map[string]string{
				"environment.yaml": strings.Replace(newEnvironmentYAML, "    provider-host-ssh: { file: ~/ssh }\n", "    provider-host-ssh: { file: ~/ssh }\n    api-tls:\n", 1),
				"cluster.yaml": strings.Replace(newClusterYAML,
					"    pullSecretRef: { name: openshift-pull-secret }\n",
					"    pullSecretRef: { name: openshift-pull-secret }\n    servingCertificates:\n      apiServer:\n        namedCertificates:\n          - names:\n              - api-int.sno.bootwright.test\n            secretRef: { name: api-tls }\n", 1),
			},
			wantSubstring: `must not target the internal API endpoint`,
		},
		{
			name: "serving-cert-file-source-keyfile-required",
			files: map[string]string{
				"environment.yaml": strings.Replace(newEnvironmentYAML, "    provider-host-ssh: { file: ~/ssh }\n", "    provider-host-ssh: { file: ~/ssh }\n    api-tls: { file: ./api.crt }\n", 1),
				"cluster.yaml": strings.Replace(newClusterYAML,
					"    pullSecretRef: { name: openshift-pull-secret }\n",
					"    pullSecretRef: { name: openshift-pull-secret }\n    servingCertificates:\n      apiServer:\n        namedCertificates:\n          - names:\n              - api.sno.bootwright.test\n            secretRef: { name: api-tls }\n", 1),
			},
			wantSubstring: `uses file-sourced TLS material but Environment/env spec.secrets[api-tls].keyFile is empty`,
		},
		{
			name: "ingress-serving-cert-unknown-ref-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"    pullSecretRef: { name: openshift-pull-secret }\n",
				"    pullSecretRef: { name: openshift-pull-secret }\n    servingCertificates:\n      ingress:\n        defaultCertificateRef: { name: ingress-tls }\n", 1)},
			wantSubstring: `defaultCertificateRef "ingress-tls" is not declared`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, newBaselineFiles())
			for name, content := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}
			_, err := LoadNormalizeValidate([]string{dir})
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
			}
		})
	}
}

func TestNodeSSHSplitRefsValidate(t *testing.T) {
	files := newBaselineFiles()
	files["environment.yaml"] = strings.Replace(newEnvironmentYAML,
		"    cluster-admin-ssh-key: { file: ~/ssh.pub }",
		"    cluster-admin-public: { file: ~/ssh.pub }\n    cluster-admin-private: { file: ~/ssh }", 1)
	files["cluster.yaml"] = strings.Replace(newClusterYAML,
		"    pullSecretRef: { name: openshift-pull-secret }",
		"    pullSecretRef: { name: openshift-pull-secret }\n    nodeSSH:\n      publicKeyRef: { name: cluster-admin-public }\n      privateKeyRef: { name: cluster-admin-private }", 1)
	dir := t.TempDir()
	writeFiles(t, dir, files)

	state, err := LoadNormalizeValidate([]string{dir})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	ssh := state.ContainerClusters[0].Spec.Install.NodeSSH
	if got := ssh.PublicKeyRef.Name; got != "cluster-admin-public" {
		t.Fatalf("PublicKeyRef.Name = %q, want cluster-admin-public", got)
	}
	if got := ssh.PrivateKeyRef.Name; got != "cluster-admin-private" {
		t.Fatalf("PrivateKeyRef.Name = %q, want cluster-admin-private", got)
	}
}

func TestRemovedContainerClusterInstallFieldsRejectStrictDecode(t *testing.T) {
	tests := []struct {
		name          string
		fieldYAML     string
		wantSubstring string
	}{
		{
			name: "baseDomain",
			fieldYAML: `    baseDomain: cluster.example.test
`,
			wantSubstring: "field baseDomain not found",
		},
		{
			name: "imageDigestSources",
			fieldYAML: `    imageDigestSources:
      - source: quay.io/openshift-release-dev/ocp-release
        mirrors:
          - registry.example.test:5000/ocp-release
`,
			wantSubstring: "field imageDigestSources not found",
		},
		{
			name: "installConfigOverrides",
			fieldYAML: `    installConfigOverrides:
      proxy:
        httpProxy: http://proxy.example.test:3128
`,
			wantSubstring: "field installConfigOverrides not found",
		},
		{
			name: "agentConfigOverrides",
			fieldYAML: `    agentConfigOverrides:
      additionalNTPSources:
        - ntp.example.test
`,
			wantSubstring: "field agentConfigOverrides not found",
		},
		{
			name: "additionalTrustBundleRef",
			fieldYAML: `    additionalTrustBundleRef:
      name: old-trust
`,
			wantSubstring: "field additionalTrustBundleRef not found",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newBaselineFiles()
			files["cluster.yaml"] = strings.Replace(files["cluster.yaml"],
				"    pullSecretRef: { name: openshift-pull-secret }\n",
				"    pullSecretRef: { name: openshift-pull-secret }\n"+tc.fieldYAML, 1)
			writeFiles(t, dir, files)
			_, err := LoadNormalizeValidate([]string{dir})
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSubstring)
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
			}
		})
	}
}

func TestOKDDoesNotRequirePullSecret(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["cluster.yaml"] = strings.Replace(newClusterYAML,
		"type: openshift\n    release:\n      version: 4.21.15",
		"type: okd\n    release:\n      image: quay.io/okd/scos-release:4.20.0-okd-scos.13", 1)
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"], "pullSecretRef: { name: openshift-pull-secret }", "", 1)
	writeFiles(t, dir, files)
	if _, err := LoadNormalizeValidate([]string{dir}); err != nil {
		t.Fatalf("OKD without pullSecretRef should validate: %v", err)
	}
}

func TestEnvironmentSecretEmptyEntryDeclaresContextMaterial(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	writeFiles(t, dir, files)
	state, err := LoadNormalizeValidate([]string{dir})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	spec := state.Environments[0].Spec.Secrets["openshift-pull-secret"]
	if spec.File != "" || spec.Generated != nil {
		t.Fatalf("openshift-pull-secret = %+v, want context-local empty source", spec)
	}
}

func TestEnvironmentProxyForNoneIsReservedDisableValue(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = strings.Replace(files["environment.yaml"], "  infraComponents:\n", `  proxyFor:
    bootwright: none
    containerClusterInstall: none

  infraComponents:
`, 1)
	writeFiles(t, dir, files)
	state, err := LoadNormalizeValidate([]string{dir})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	if got := state.Environments[0].Spec.ProxyFor.Bootwright; got != v1alpha1.EnvironmentComponentNone {
		t.Fatalf("proxyFor.bootwright = %q, want none", got)
	}
}

func TestReleaseImageRequiresPinnedReference(t *testing.T) {
	cases := []struct {
		name          string
		image         string
		wantSubstring string
	}{
		{
			name:          "latest tag",
			image:         "quay.io/okd/scos-release:latest",
			wantSubstring: `spec.distribution.release.image "quay.io/okd/scos-release:latest" must not use mutable :latest tag`,
		},
		{
			name:          "missing tag",
			image:         "quay.io/okd/scos-release",
			wantSubstring: `spec.distribution.release.image "quay.io/okd/scos-release" must pin a version tag or digest`,
		},
		{
			name:          "floating tag",
			image:         "quay.io/okd/scos-release:stable",
			wantSubstring: `spec.distribution.release.image "quay.io/okd/scos-release:stable" must pin a version tag or digest`,
		},
		{
			name:  "version tag",
			image: "quay.io/okd/scos-release:4.20.0-okd-scos.13",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newBaselineFiles()
			files["cluster.yaml"] = strings.Replace(newClusterYAML,
				"type: openshift\n    release:\n      version: 4.21.15",
				"type: okd\n    release:\n      image: "+tc.image, 1)
			files["cluster.yaml"] = strings.Replace(files["cluster.yaml"], "pullSecretRef: { name: openshift-pull-secret }", "", 1)
			writeFiles(t, dir, files)
			_, err := LoadNormalizeValidate([]string{dir})
			if tc.wantSubstring == "" {
				if err != nil {
					t.Fatalf("LoadNormalizeValidate: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSubstring)
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
			}
		})
	}
}

func TestEnvironmentProxyURLValidation(t *testing.T) {
	cases := []struct {
		name          string
		proxyYAML     string
		wantSubstring string
	}{
		{
			name: "valid-http-proxy",
			proxyYAML: `    proxies:
      - name: default
        type: external
        connection:
          httpProxy: http://proxy.bootwright.test:3128
`,
		},
		{
			name: "missing-scheme",
			proxyYAML: `    proxies:
      - name: default
        type: external
        connection:
          httpProxy: proxy.bootwright.test:3128
`,
			wantSubstring: `spec.infraComponents.proxies[0].connection.httpProxy "proxy.bootwright.test:3128" is invalid: must include scheme and host`,
		},
		{
			name: "unsupported-scheme",
			proxyYAML: `    proxies:
      - name: default
        type: external
        connection:
          httpsProxy: socks5://proxy.bootwright.test:1080
`,
			wantSubstring: `spec.infraComponents.proxies[0].connection.httpsProxy "socks5://proxy.bootwright.test:1080" is invalid: scheme must be http or https`,
		},
		{
			name: "inline-credentials",
			proxyYAML: `    proxies:
      - name: default
        type: external
        connection:
          httpProxy: http://user:pass@proxy.bootwright.test:3128
`,
			wantSubstring: `spec.infraComponents.proxies[0].connection.httpProxy must not embed credentials`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newBaselineFiles()
			files["environment.yaml"] = strings.Replace(files["environment.yaml"], "    artifactServers:\n", tc.proxyYAML+"    artifactServers:\n", 1)
			writeFiles(t, dir, files)
			_, err := LoadNormalizeValidate([]string{dir})
			if tc.wantSubstring == "" {
				if err != nil {
					t.Fatalf("LoadNormalizeValidate: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSubstring)
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
			}
		})
	}
}

func TestEnvironmentProxyOldSpecRejectsStrictDecode(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = strings.Replace(files["environment.yaml"], "    artifactServers:\n", `    proxies:
      - name: default
        type: external
        spec:
          httpProxy: http://proxy.bootwright.test:3128
    artifactServers:
`, 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected old proxy spec field to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "field spec not found") {
		t.Fatalf("error %q does not contain %q", err, "field spec not found")
	}
}

func TestEnvironmentProxyDefaultRejected(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = strings.Replace(files["environment.yaml"], "    artifactServers:\n", `    proxies:
      - name: default
        default: true
        type: external
        connection:
          httpProxy: http://proxy.bootwright.test:3128
    artifactServers:
`, 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected proxy default decode error, got nil")
	}
	want := "field default not found"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestEnvironmentNameResolutionDefaultRejected(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = strings.Replace(files["environment.yaml"], "    artifactServers:\n", `    nameResolution:
      - name: default
        default: true
        type: external
        ip: 192.168.132.1
    artifactServers:
`, 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected nameResolution default decode error, got nil")
	}
	want := "field default not found"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestLibvirtBMCEmulationValidation(t *testing.T) {
	cases := []struct {
		name          string
		providerYAML  string
		wantSubstring string
	}{
		{
			name: "missing-emulation",
			providerYAML: `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata: { name: libvirt }
spec:
  machineProfiles:
    - name: sno
      libvirt:
        hostRef: { name: lab-host }
        uri: qemu:///system
`,
			wantSubstring: "libvirt.bmcEmulationDefaults is required for current libvirt apply support",
		},
		{
			name: "disabled-emulation",
			providerYAML: `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata: { name: libvirt }
spec:
  machineProfiles:
    - name: sno
      libvirt:
        hostRef: { name: lab-host }
        uri: qemu:///system
        bmcEmulationDefaults:
          enabled: false
          auth:
            credentialRef: { name: bmc-credentials }
`,
			wantSubstring: "libvirt.bmcEmulationDefaults.enabled=false is not supported",
		},
		{
			name: "duplicate-default-ports",
			providerYAML: `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata: { name: libvirt-a }
spec:
  machineProfiles:
    - name: sno
      libvirt:
        hostRef: { name: lab-host }
        uri: qemu:///system
        bmcEmulationDefaults:
          auth:
            credentialRef: { name: bmc-credentials }
---
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata: { name: libvirt-b }
spec:
  machineProfiles:
    - name: sno
      libvirt:
        hostRef: { name: lab-host }
        uri: qemu:///system
        bmcEmulationDefaults:
          auth:
            credentialRef: { name: bmc-credentials }
`,
			wantSubstring: "port 8000 conflicts",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, map[string]string{
				"environment.yaml": `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata: { name: env }
spec:
  baseDomain: bootwright.test
  secrets:
    bmc-credentials:
`,
				"hosts.yaml": `apiVersion: bootwright.io/v1alpha1
kind: Host
metadata: { name: lab-host }
spec:
  addresses:
    - { name: ssh, address: 192.168.132.1 }
  ssh: { addressName: ssh }
  capabilities: [libvirt]
`,
				"provider.yaml": tc.providerYAML,
			})
			_, err := LoadNormalizeValidate([]string{dir})
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSubstring)
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
			}
		})
	}
}

func TestComponentImagesRequirePinnedReference(t *testing.T) {
	cases := []struct {
		name          string
		image         string
		wantSubstring string
	}{
		{
			name:          "latest tag",
			image:         "quay.io/squid:latest",
			wantSubstring: `spec.componentImages[proxy][squid].public "quay.io/squid:latest" must not use mutable :latest tag`,
		},
		{
			name:          "missing tag",
			image:         "quay.io/squid",
			wantSubstring: `spec.componentImages[proxy][squid].public "quay.io/squid" must pin a version tag or digest`,
		},
		{
			name:          "floating tag",
			image:         "quay.io/squid:stable",
			wantSubstring: `spec.componentImages[proxy][squid].public "quay.io/squid:stable" must pin a version tag or digest`,
		},
		{
			name:  "version tag",
			image: "quay.io/squid:7.5-oe2403sp3",
		},
		{
			name:  "digest",
			image: "quay.io/squid@sha256:1111111111111111111111111111111111111111111111111111111111111111",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newBaselineFiles()
			files["environment.yaml"] = strings.Replace(files["environment.yaml"], "  secrets:\n", `  componentImages:
    proxy:
      squid:
        public: `+tc.image+`
  secrets:
`, 1)
			writeFiles(t, dir, files)
			_, err := LoadNormalizeValidate([]string{dir})
			if tc.wantSubstring == "" {
				if err != nil {
					t.Fatalf("LoadNormalizeValidate: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSubstring)
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
			}
		})
	}
}

func TestEnvironmentResourcesSelectsListedFiles(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = newEnvironmentYAMLWithResources("hosts.yaml", "network.yaml", "provider.yaml", "infra-component.yaml", "cluster.yaml")
	files["unused.yaml"] = `apiVersion: bootwright.io/v1alpha1
kind: Host
metadata: { name: unused-host }
spec:
  addresses:
    - name: ssh
      address: 192.168.132.50
  ssh:
    addressName: ssh
    keyRef: { name: provider-host-ssh }
  capabilities: [container-runtime]
`
	writeFiles(t, dir, files)
	state, err := LoadNormalizeValidate([]string{dir})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	if len(state.InfraProviders) != 1 || state.InfraProviders[0].Metadata.Name != "rack" {
		t.Fatalf("loaded InfraProviders = %#v, want rack only", state.InfraProviders)
	}
}

func TestEnvironmentContainerClustersFiltersEffectiveState(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = strings.Replace(files["environment.yaml"], "  baseDomain: bootwright.test\n", `  baseDomain: bootwright.test

  containerClusters:
    - sno

`, 1)
	files["cluster-b.yaml"] = strings.Replace(newClusterYAML, "name: sno", "name: unused", 2)
	files["cluster-b.yaml"] = strings.Replace(files["cluster-b.yaml"], "clusterInfra: sno", "clusterInfra: unused", 1)
	files["cluster-b.yaml"] = strings.Replace(files["cluster-b.yaml"], "from: { provider: rack, name: srv1 }", "from: { provider: unused-rack, name: srv1 }", 1)
	files["provider-b.yaml"] = strings.Replace(newProviderYAML, "name: rack", "name: unused-rack", 1)
	writeFiles(t, dir, files)

	state, err := LoadNormalizeValidate([]string{dir})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	if got := len(state.ContainerClusters); got != 1 {
		t.Fatalf("ContainerClusters = %d, want 1", got)
	}
	if got := state.ContainerClusters[0].Metadata.Name; got != "sno" {
		t.Fatalf("selected cluster = %q, want sno", got)
	}
	if got := len(state.ClusterInfras); got != 1 {
		t.Fatalf("ClusterInfras = %d, want 1", got)
	}
	if got := state.ClusterInfras[0].Metadata.Name; got != "sno" {
		t.Fatalf("selected infra = %q, want sno", got)
	}
	if got := len(state.InfraProviders); got != 1 {
		t.Fatalf("InfraProviders = %d, want 1", got)
	}
	if got := state.InfraProviders[0].Metadata.Name; got != "rack" {
		t.Fatalf("selected provider = %q, want rack", got)
	}
}

func TestEnvironmentResourcesUnsetLoadsAllFiles(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["unused.yaml"] = `apiVersion: bootwright.io/v1alpha1
kind: Network
metadata: { name: old }
spec: {}
`
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected unsupported unlisted kind to fail when spec.resources is unset")
	}
	if !strings.Contains(err.Error(), `unsupported kind "Network"`) {
		t.Fatalf("error %q does not contain unsupported kind", err)
	}
}

func TestEnvironmentResourcesRequireReferencedProvider(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = newEnvironmentYAMLWithResources("hosts.yaml", "network.yaml", "infra-component.yaml", "cluster.yaml")
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected omitted provider to fail")
	}
	want := `spec.resources excludes InfraProvider/rack required by ClusterInfra/sno spec.components.machines[master-0].from.provider; add "provider.yaml"`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestEnvironmentResourcesRequireReferencedHost(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = newEnvironmentYAMLWithResources("network.yaml", "provider.yaml", "infra-component.yaml", "cluster.yaml")
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected omitted host to fail")
	}
	want := `spec.resources excludes Host/services-host required by InfraComponent/artifact-server spec.artifactServer.hostRef; add "hosts.yaml"`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestEnvironmentResourcesRequireReferencedInfraComponent(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = newEnvironmentYAMLWithResources("hosts.yaml", "network.yaml", "provider.yaml", "cluster.yaml")
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidateInputFiles([]string{dir})
	if err == nil {
		t.Fatal("expected omitted infra component to fail")
	}
	want := `spec.resources excludes InfraComponent/artifact-server required by Environment/env spec.infraComponents.artifactServers[default].componentRef; add "infra-component.yaml"`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestEnvironmentResourcesSupportSubdirectories(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"environment.yaml": newEnvironmentYAMLWithResources(
			"shared/hosts.yaml",
			"shared/network.yaml",
			"shared/provider.yaml",
			"shared/infra-component.yaml",
			"clusters/sno/cluster.yaml",
		),
		"shared/hosts.yaml":           newHostsYAML,
		"shared/network.yaml":         newNetworkConfigYAML,
		"shared/provider.yaml":        newProviderYAML,
		"shared/infra-component.yaml": newInfraComponentYAML,
		"clusters/sno/cluster.yaml":   newClusterYAML,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("create %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	state, err := LoadNormalizeValidate([]string{dir})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	if got := len(state.ContainerClusters); got != 1 {
		t.Fatalf("ContainerClusters = %d, want 1", got)
	}
	if got := state.ContainerClusters[0].SourcePath; !strings.Contains(filepath.ToSlash(got), "clusters/sno/cluster.yaml") {
		t.Fatalf("cluster SourcePath = %q, want nested cluster resource", got)
	}
}

func TestEnvironmentResourcesSupportDirectories(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"environment.yaml":            newEnvironmentYAMLWithResources("shared", "clusters/sno"),
		"shared/hosts.yaml":           newHostsYAML,
		"shared/network.yaml":         newNetworkConfigYAML,
		"shared/provider.yaml":        newProviderYAML,
		"shared/infra-component.yaml": newInfraComponentYAML,
		"clusters/sno/cluster.yaml":   newClusterYAML,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("create %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	state, err := LoadNormalizeValidate([]string{dir})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	if got := len(state.ContainerClusters); got != 1 {
		t.Fatalf("ContainerClusters = %d, want 1", got)
	}
}

func TestEnvironmentStorageClusterSelectionOmitsContainerRoots(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = strings.Replace(newEnvironmentYAML, "  secrets:\n", "  storageClusters:\n    - imported-ceph\n\n  secrets:\n", 1)
	files["storage.yaml"] = `apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata:
  name: imported-ceph
spec:
  type: ceph
  management: external
`
	writeFiles(t, dir, files)
	state, err := LoadNormalizeValidate([]string{dir})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	if got := len(state.StorageClusters); got != 1 {
		t.Fatalf("StorageClusters = %d, want 1", got)
	}
	if got := len(state.ContainerClusters); got != 0 {
		t.Fatalf("ContainerClusters = %d, want 0", got)
	}
}

func TestEnvironmentResourcesIgnoreUnselectedInvalidFiles(t *testing.T) {
	cases := []struct {
		name           string
		unselectedYAML string
	}{
		{
			name: "unknown-field",
			unselectedYAML: `apiVersion: bootwright.io/v1alpha1
kind: Host
metadata:
  name: spare-host
spec:
  retiredField: true
`,
		},
		{
			name: "malformed-yaml",
			unselectedYAML: `apiVersion: bootwright.io/v1alpha1
kind: Host
metadata: [bad
`,
		},
		{
			name: "broken-reference",
			unselectedYAML: `apiVersion: bootwright.io/v1alpha1
kind: Host
metadata:
  name: spare-host
spec:
  addresses:
    - name: ssh
      address: 192.168.132.50
  ssh:
    addressName: ssh
    keyRef:
      name: missing-secret
  capabilities:
    - container-runtime
`,
		},
		{
			name: "unsupported-kind",
			unselectedYAML: `apiVersion: bootwright.io/v1alpha1
kind: Network
metadata:
  name: old
spec: {}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newBaselineFiles()
			files["environment.yaml"] = newEnvironmentYAMLWithResources("hosts.yaml", "network.yaml", "provider.yaml", "infra-component.yaml", "cluster.yaml")
			files["unselected.yaml"] = tc.unselectedYAML
			writeFiles(t, dir, files)
			state, err := LoadNormalizeValidateInputFiles([]string{dir})
			if err != nil {
				t.Fatalf("LoadNormalizeValidateInputFiles: %v", err)
			}
			if got := len(state.Hosts); got != 1 {
				t.Fatalf("Hosts = %d, want only selected host", got)
			}
		})
	}
}

func TestEndpointVIPOwnershipValidation(t *testing.T) {
	cases := []struct {
		name          string
		mutate        func(map[string]string)
		wantSubstring string
	}{
		{
			name: "external-endpoints-accepted",
		},
		{
			name: "sno-openshift-endpoints-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      address: 192.168.132.10
    api-int:
      address: 192.168.132.10
    apps:
      address: 192.168.132.11
`)
			},
			wantSubstring: "single-node clusters forbid ClusterInfra/sno spec.endpoints.api source.type=openshift",
		},
		{
			name: "single-bind-address-shortcut-accepted",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      source:
        type: infraComponent
        componentRef: { name: control-plane }
    api-int:
      source:
        type: infraComponent
        componentRef: { name: control-plane }
    apps:
      address: 192.168.132.11
      source:
        type: external
`)
				addLoadBalancerInfraComponent(files, "control-plane", "- ip: 192.168.132.10\n")
			},
		},
		{
			name: "named-bind-address-selection-accepted",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      source:
        type: infraComponent
        componentRef: { name: load-balancer }
        bindAddress: control-plane
    api-int:
      source:
        type: infraComponent
        componentRef: { name: load-balancer }
        bindAddress: control-plane
    apps:
      source:
        type: infraComponent
        componentRef: { name: load-balancer }
        bindAddress: apps
`)
				addLoadBalancerInfraComponent(files, "load-balancer", "- { name: control-plane, ip: 192.168.132.10 }\n    - { name: apps, ip: 192.168.132.11 }\n")
			},
		},
		{
			name: "missing-bind-address-names-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      source:
        type: infraComponent
        componentRef: { name: load-balancer }
        bindAddress: control-plane
    api-int:
      source:
        type: infraComponent
        componentRef: { name: load-balancer }
        bindAddress: control-plane
    apps:
      source:
        type: infraComponent
        componentRef: { name: load-balancer }
        bindAddress: apps
`)
				addLoadBalancerInfraComponent(files, "load-balancer", "- { ip: 192.168.132.10 }\n    - { ip: 192.168.132.11 }\n")
			},
			wantSubstring: "bindAddresses[0].name is required",
		},
		{
			name: "bad-loadbalancer-reference-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      source:
        type: infraComponent
        componentRef: { name: missing }
    api-int:
      address: 192.168.132.10
      source:
        type: external
    apps:
      address: 192.168.132.11
      source:
        type: external
`)
			},
			wantSubstring: `source.componentRef.name "missing" does not resolve to an InfraComponent loadBalancer`,
		},
		{
			name: "bad-bind-address-reference-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      source:
        type: infraComponent
        componentRef: { name: load-balancer }
        bindAddress: missing
    api-int:
      address: 192.168.132.10
      source:
        type: external
    apps:
      address: 192.168.132.11
      source:
        type: external
`)
				addLoadBalancerInfraComponent(files, "load-balancer", "- { name: control-plane, ip: 192.168.132.10 }\n")
			},
			wantSubstring: `source.bindAddress "missing" does not match`,
		},
		{
			name: "vip-outside-selected-machine-network-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      address: 192.168.140.10
      source:
        type: external
    api-int:
      address: 192.168.132.10
      source:
        type: external
    apps:
      address: 192.168.132.11
      source:
        type: external
`)
			},
			wantSubstring: `spec.endpoints.api.address "192.168.140.10" is outside selected NetworkConfig machine networks`,
		},
		{
			name: "invalid-source-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      address: 192.168.132.10
      source:
        type: manual
    api-int:
      address: 192.168.132.10
      source:
        type: external
    apps:
      address: 192.168.132.11
      source:
        type: external
`)
			},
			wantSubstring: `spec.endpoints.api.source.type "manual" must be one of`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newBaselineFiles()
			if tc.mutate != nil {
				tc.mutate(files)
			}
			writeFiles(t, dir, files)
			_, err := LoadNormalizeValidate([]string{dir})
			if tc.wantSubstring == "" {
				if err != nil {
					t.Fatalf("LoadNormalizeValidate: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSubstring)
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
			}
		})
	}
}

func TestNetworkConfigDNSRefsSelectEnvironmentEntries(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = strings.Replace(files["environment.yaml"], "  infraComponents:\n", `  infraComponents:
    nameResolution:
      - name: default
        type: external
        ip: 192.168.132.53
`, 1)
	files["network.yaml"] = strings.Replace(files["network.yaml"], "  template:\n", `  dnsRefs:
    - default
  template:
`, 1)
	writeFiles(t, dir, files)
	if _, err := LoadNormalizeValidate([]string{dir}); err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
}

func TestNetworkConfigDNSRefsRejectDuplicates(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = strings.Replace(files["environment.yaml"], "  infraComponents:\n", `  infraComponents:
    nameResolution:
      - name: default
        type: external
        ip: 192.168.132.53
`, 1)
	files["network.yaml"] = strings.Replace(files["network.yaml"], "  template:\n", `  dnsRefs:
    - default
    - default
  template:
`, 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected duplicate dnsRefs error, got nil")
	}
	want := `NetworkConfig/cluster-net spec.dnsRefs[1] "default" is duplicated`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestNetworkConfigDNSRefsRejectUnknownEnvironmentEntry(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["network.yaml"] = strings.Replace(files["network.yaml"], "  template:\n", `  dnsRefs:
    - missing
  template:
`, 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected unresolved dnsRefs error, got nil")
	}
	want := `NetworkConfig/cluster-net spec.dnsRefs[0] "missing" does not match any Environment spec.infraComponents.nameResolution[].name`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestNetworkConfigRejectsStaleNameResolutionRefs(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["network.yaml"] = strings.Replace(files["network.yaml"], "  template:\n", `  nameResolutionRefs:
    - default
  template:
`, 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected stale nameResolutionRefs field error, got nil")
	}
	if !strings.Contains(err.Error(), "nameResolutionRefs") {
		t.Fatalf("error %q does not mention nameResolutionRefs", err)
	}
}

func TestNetworkConfigRejectsTemplateDNSRefs(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["network.yaml"] = strings.Replace(files["network.yaml"], "    networkConfig:\n", `    networkConfig:
      dnsRefs:
        - default
`, 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected invalid networkConfig.dnsRefs error, got nil")
	}
	want := "spec.template.networkConfig.dnsRefs is not valid NMState; use spec.dnsRefs instead"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestVSphereMultiNICRequiresNodeNetworking(t *testing.T) {
	dir := t.TempDir()
	files := newVSphereFiles("")
	writeFiles(t, dir, files)
	if _, err := LoadNormalizeValidate([]string{dir}); err == nil {
		t.Fatal("expected vSphere multi-NIC profile without nodeNetworking to fail")
	} else if !strings.Contains(err.Error(), "declares multiple vSphere topology networks") {
		t.Fatalf("error %q does not mention multi-NIC nodeNetworking", err)
	}

	dir = t.TempDir()
	files = newVSphereFiles(`        nodeNetworking:
          external:
            networkSubnetCidr: [192.168.133.0/24]
`)
	writeFiles(t, dir, files)
	if _, err := LoadNormalizeValidate([]string{dir}); err != nil {
		t.Fatalf("vSphere multi-NIC profile with nodeNetworking should validate: %v", err)
	}
}

func TestVSphereFailureDomainRequiresInstallerFields(t *testing.T) {
	tests := []struct {
		name          string
		remove        string
		wantSubstring string
	}{
		{
			name:          "region",
			remove:        "            region: dc1\n",
			wantSubstring: ".vsphere.failureDomains[0].region is required",
		},
		{
			name:          "zone",
			remove:        "            zone: zone-a\n",
			wantSubstring: ".vsphere.failureDomains[0].zone is required",
		},
		{
			name:          "datastore",
			remove:        "              datastore: /dc1/datastore/datastore1\n",
			wantSubstring: ".vsphere.failureDomains[0].topology.datastore is required",
		},
		{
			name:          "networks",
			remove:        "              networks: [VM_Network_1, VM_Network_2]\n",
			wantSubstring: ".vsphere.failureDomains[0].topology.networks is required",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newVSphereFiles(`        nodeNetworking:
          external:
            networkSubnetCidr: [192.168.133.0/24]
`)
			files["provider.yaml"] = strings.Replace(files["provider.yaml"], tc.remove, "", 1)
			writeFiles(t, dir, files)
			_, err := LoadNormalizeValidate([]string{dir})
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSubstring)
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
			}
		})
	}
}

func TestKubeVirtHostClusterValidation(t *testing.T) {
	tests := []struct {
		name          string
		mutate        func(map[string]string)
		wantSubstring string
	}{
		{
			name: "host-cluster-ref",
		},
		{
			name: "missing-parent",
			mutate: func(files map[string]string) {
				files["child.yaml"] = strings.Replace(files["child.yaml"], "hostContainerClusterRef:\n          name: sno", "hostContainerClusterRef:\n          name: missing", 1)
			},
			wantSubstring: `kubevirt.hostContainerClusterRef.name "missing" does not match any ContainerCluster`,
		},
		{
			name: "both-host-and-kubeconfig",
			mutate: func(files map[string]string) {
				files["environment.yaml"] = addKubeVirtKubeconfigSecret(files["environment.yaml"])
				files["child.yaml"] = strings.Replace(files["child.yaml"], "hostContainerClusterRef:\n          name: sno\n        namespace:", "hostContainerClusterRef:\n          name: sno\n        kubeconfigRef:\n          name: external-virt-cluster-kubeconfig\n        namespace:", 1)
			},
			wantSubstring: "kubevirt must set exactly one of {hostContainerClusterRef, kubeconfigRef}",
		},
		{
			name: "invalid-namespace",
			mutate: func(files map[string]string) {
				files["child.yaml"] = strings.Replace(files["child.yaml"], "namespace: bootwright-child-ocp", "namespace: Bootwright_Child", 1)
			},
			wantSubstring: `kubevirt.namespace "Bootwright_Child" is not a DNS label`,
		},
		{
			name: "kubeconfig-ref",
			mutate: func(files map[string]string) {
				files["environment.yaml"] = addKubeVirtKubeconfigSecret(files["environment.yaml"])
				files["child.yaml"] = strings.Replace(files["child.yaml"], "hostContainerClusterRef:\n          name: sno", "kubeconfigRef:\n          name: external-virt-cluster-kubeconfig", 1)
			},
		},
		{
			name: "unknown-kubeconfig-ref-secret",
			mutate: func(files map[string]string) {
				files["child.yaml"] = strings.Replace(files["child.yaml"], "hostContainerClusterRef:\n          name: sno", "kubeconfigRef:\n          name: external-virt-cluster-kubeconfig", 1)
			},
			wantSubstring: `kubevirt.kubeconfigRef "external-virt-cluster-kubeconfig" is not declared in Environment/env spec.secrets`,
		},
		{
			name: "missing-network-binding",
			mutate: func(files map[string]string) {
				files["child.yaml"] = strings.Replace(files["child.yaml"], "  networkBindings:\n    - networkConfigRef: { name: child-machine-net }\n      providerRef: { name: child-kubevirt-provider }\n      attachmentRef: { name: child-machine-net }\n", "", 1)
			},
			wantSubstring: `networkConfig.ref "child-machine-net" has no ClusterInfra/child-ocp-infra spec.networkBindings entry for InfraProvider/child-kubevirt-provider`,
		},
		{
			name: "missing-network-attachment",
			mutate: func(files map[string]string) {
				files["child.yaml"] = strings.Replace(files["child.yaml"], "attachmentRef: { name: child-machine-net }", "attachmentRef: { name: missing }", 1)
			},
			wantSubstring: `attachmentRef.name "missing" does not match any InfraProvider/child-kubevirt-provider spec.networkAttachments[].name`,
		},
		{
			name: "network-attachment-kind-mismatch",
			mutate: func(files map[string]string) {
				files["child.yaml"] = strings.Replace(files["child.yaml"], "      kubevirt:\n        nadRef:\n          name: child-ocp-net\n          namespace: bootwright-child-ocp\n", "      libvirt:\n        bridge: vbr-child\n", 1)
			},
			wantSubstring: `networkConfig.ref "child-machine-net" binds to InfraProvider/child-kubevirt-provider networkAttachment "child-machine-net" of kind "libvirt", but machine substrate is "kubevirt"`,
		},
		{
			name: "network-attachment-nad-namespace-required",
			mutate: func(files map[string]string) {
				files["child.yaml"] = strings.Replace(files["child.yaml"], "          namespace: bootwright-child-ocp\n", "", 1)
			},
			wantSubstring: `networkAttachments[child-machine-net].kubevirt.nadRef.namespace is required`,
		},
		{
			name: "missing-kubevirt-capability",
			mutate: func(files map[string]string) {
				files["extension.yaml"] = strings.Replace(files["extension.yaml"], "  provides:\n    - kubevirt\n", "", 1)
			},
			wantSubstring: `requires a ClusterAddonBinding that applies a ClusterAddon providing "kubevirt"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newKubeVirtChildFiles()
			if tc.mutate != nil {
				tc.mutate(files)
			}
			writeFiles(t, dir, files)
			_, err := LoadNormalizeValidate([]string{dir})
			if tc.wantSubstring == "" {
				if err != nil {
					t.Fatalf("LoadNormalizeValidate: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSubstring)
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
			}
		})
	}
}

func TestKubeVirtHostClusterDependencyCycleValidation(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, newKubeVirtCycleFiles())
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected KubeVirt hostContainerClusterRef cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "KubeVirt hostContainerClusterRef creates ContainerCluster dependency cycle") {
		t.Fatalf("error %q does not mention dependency cycle", err)
	}
}

func TestReleaseChannelDerivation(t *testing.T) {
	cluster := v1alpha1.ContainerCluster{
		Spec: v1alpha1.ContainerClusterSpec{
			Distribution: v1alpha1.DistributionSpec{
				Type:    v1alpha1.DistributionOpenShift,
				Release: v1alpha1.ReleaseSpec{Version: "4.21.15"},
			},
		},
	}
	if got := v1alpha1.ReleaseChannel(cluster); got != "stable-4.21" {
		t.Fatalf("ReleaseChannel = %q, want stable-4.21", got)
	}
	cluster.Spec.Distribution.Type = v1alpha1.DistributionOKD
	if got := v1alpha1.ReleaseChannel(cluster); got != "" {
		t.Fatalf("OKD ReleaseChannel = %q, want empty", got)
	}
}

func newKubeVirtChildFiles() map[string]string {
	files := newBaselineFiles()
	files["extension.yaml"] = strings.Replace(extensionYAML("openshift-virtualization"), "  type: olm-operator\n", "  type: olm-operator\n  provides:\n    - kubevirt\n", 1)
	files["set.yaml"] = extensionSetYAML("virtualization-platform", "openshift-virtualization")
	files["binding.yaml"] = extensionBindingYAML("parent-addons", "virtualization-platform")
	files["child.yaml"] = `apiVersion: bootwright.io/v1alpha1
kind: NetworkConfig
metadata: { name: child-machine-net }
spec:
  machineNetwork:
    - { cidr: 192.168.134.0/24 }
  template:
    networkConfig:
      interfaces:
        - { name: primary, type: ethernet, state: up, ipv4: { enabled: true, dhcp: false }, ipv6: { enabled: false } }
      routes:
        config:
          - { destination: 0.0.0.0/0, next-hop-address: 192.168.134.1, next-hop-interface: primary, table-id: 254 }
---
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata: { name: child-kubevirt-provider }
spec:
  machineProfiles:
    - name: child-sno
      cpu: 8
      memoryMiB: 16384
      diskGiB: 120
      kubevirt:
        hostContainerClusterRef:
          name: sno
        namespace: bootwright-child-ocp
        storageClassRef:
          name: lvms-vg1
  networkAttachments:
    - name: child-machine-net
      kubevirt:
        nadRef:
          name: child-ocp-net
          namespace: bootwright-child-ocp
---
apiVersion: bootwright.io/v1alpha1
kind: ClusterInfra
metadata: { name: child-ocp-infra }
spec:
  platform:
    type: none
  endpoints:
    api:
      address: 192.168.134.20
      source:
        type: external
    api-int:
      address: 192.168.134.20
      source:
        type: external
    apps:
      address: 192.168.134.21
      source:
        type: external
  networkBindings:
    - networkConfigRef: { name: child-machine-net }
      providerRef: { name: child-kubevirt-provider }
      attachmentRef: { name: child-machine-net }
  components:
    machines:
      - name: master-0
        from: { provider: child-kubevirt-provider, profile: child-sno }
        networkConfig:
          ref: { name: child-machine-net }
          overrides:
            interfaces:
              - name: primary
                ipv4:
                  address:
                    - { ip: 192.168.134.20, prefix-length: 24 }
---
apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata: { name: child-ocp }
spec:
  distribution:
    type: openshift
    release: { version: 4.21.15 }
  install:
    endpointRefs:
      api:
        name: api
      apiInt:
        name: api-int
      ingress:
        name: apps
    method: agent
    mode: connected
    pullSecretRef: { name: openshift-pull-secret }
    nodeSSH:
      keyPairRef: { name: cluster-admin-ssh-key }
  controlPlane: { name: master, replicas: 1 }
  compute:
    - { name: worker, replicas: 0 }
  networking:
    clusterNetwork: [{ cidr: 10.128.0.0/14, hostPrefix: 23 }]
    serviceNetwork: [172.30.0.0/16]
  nodes:
    - hostname: master-0
      role: master
      machineRef: { clusterInfra: child-ocp-infra, name: master-0 }
`
	return files
}

func addKubeVirtKubeconfigSecret(environmentYAML string) string {
	return strings.Replace(environmentYAML, "    bmc-credentials:\n", "    external-virt-cluster-kubeconfig:\n      file: ~/virt.kubeconfig\n    bmc-credentials:\n", 1)
}

func newKubeVirtCycleFiles() map[string]string {
	return map[string]string{
		"environment.yaml": `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata: { name: env }
spec:
  baseDomain: bootwright.test
  secrets:
    openshift-pull-secret:
    cluster-admin-ssh-key: { file: ~/ssh.pub }
`,
		"network-a.yaml": kubeVirtCycleNetworkYAML("net-a", "192.168.140.0/24", "192.168.140.1"),
		"network-b.yaml": kubeVirtCycleNetworkYAML("net-b", "192.168.141.0/24", "192.168.141.1"),
		"provider-a.yaml": `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata: { name: provider-a }
spec:
  machineProfiles:
    - name: cp
      cpu: 4
      memoryMiB: 8192
      diskGiB: 80
      kubevirt:
        hostContainerClusterRef:
          name: cluster-b
        namespace: ns-a
  networkAttachments:
    - name: net-a
      kubevirt:
        nadRef:
          name: net-a
          namespace: ns-a
`,
		"provider-b.yaml": `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata: { name: provider-b }
spec:
  machineProfiles:
    - name: cp
      cpu: 4
      memoryMiB: 8192
      diskGiB: 80
      kubevirt:
        hostContainerClusterRef:
          name: cluster-a
        namespace: ns-b
  networkAttachments:
    - name: net-b
      kubevirt:
        nadRef:
          name: net-b
          namespace: ns-b
`,
		"infra-a.yaml":   kubeVirtCycleInfraYAML("infra-a", "provider-a", "net-a", "192.168.140.20", "192.168.140.21"),
		"infra-b.yaml":   kubeVirtCycleInfraYAML("infra-b", "provider-b", "net-b", "192.168.141.20", "192.168.141.21"),
		"cluster-a.yaml": kubeVirtCycleClusterYAML("cluster-a", "infra-a"),
		"cluster-b.yaml": kubeVirtCycleClusterYAML("cluster-b", "infra-b"),
		"extension.yaml": `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata: { name: openshift-virtualization }
spec:
  type: manifest-set
  provides:
    - kubevirt
  manifestSet:
    manifests:
      - path: manifests/placeholder.yaml
  readiness:
    checks:
      - type: resourceExists
        apiVersion: kubevirt.io/v1
        kind: KubeVirt
        name: kubevirt
        namespace: openshift-cnv
`,
		"binding-a.yaml": `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata: { name: virt-a }
spec:
  clusterRef:
    name: cluster-a
  addons:
    - name: openshift-virtualization
`,
		"binding-b.yaml": `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata: { name: virt-b }
spec:
  clusterRef:
    name: cluster-b
  addons:
    - name: openshift-virtualization
`,
		"manifests/placeholder.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: placeholder
  namespace: openshift-cnv
`,
	}
}

func kubeVirtCycleNetworkYAML(name, cidr, gateway string) string {
	return `apiVersion: bootwright.io/v1alpha1
kind: NetworkConfig
metadata: { name: ` + name + ` }
spec:
  machineNetwork:
    - { cidr: ` + cidr + ` }
  template:
    networkConfig:
      interfaces:
        - { name: primary, type: ethernet, state: up, ipv4: { enabled: true, dhcp: false }, ipv6: { enabled: false } }
      routes:
        config:
          - { destination: 0.0.0.0/0, next-hop-address: ` + gateway + `, next-hop-interface: primary, table-id: 254 }
`
}

func kubeVirtCycleInfraYAML(name, provider, network, nodeIP, ingressIP string) string {
	return `apiVersion: bootwright.io/v1alpha1
kind: ClusterInfra
metadata: { name: ` + name + ` }
spec:
  platform:
    type: none
  endpoints:
    api:
      address: ` + nodeIP + `
      source:
        type: external
    api-int:
      address: ` + nodeIP + `
      source:
        type: external
    apps:
      address: ` + ingressIP + `
      source:
        type: external
  networkBindings:
    - networkConfigRef: { name: ` + network + ` }
      providerRef: { name: ` + provider + ` }
      attachmentRef: { name: ` + network + ` }
  components:
    machines:
      - name: master-0
        from: { provider: ` + provider + `, profile: cp }
        networkConfig:
          ref: { name: ` + network + ` }
          overrides:
            interfaces:
              - name: primary
                ipv4:
                  address:
                    - { ip: ` + nodeIP + `, prefix-length: 24 }
`
}

func kubeVirtCycleClusterYAML(name, infra string) string {
	return `apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata: { name: ` + name + ` }
spec:
  distribution:
    type: openshift
    release: { version: 4.21.15 }
  install:
    endpointRefs:
      api:
        name: api
      apiInt:
        name: api-int
      ingress:
        name: apps
    method: agent
    mode: connected
    pullSecretRef: { name: openshift-pull-secret }
    nodeSSH:
      keyPairRef: { name: cluster-admin-ssh-key }
  controlPlane: { name: master, replicas: 1 }
  compute:
    - { name: worker, replicas: 0 }
  networking:
    clusterNetwork: [{ cidr: 10.128.0.0/14, hostPrefix: 23 }]
    serviceNetwork: [172.30.0.0/16]
  nodes:
    - hostname: master-0
      role: master
      machineRef: { clusterInfra: ` + infra + `, name: master-0 }
`
}

func newVSphereFiles(nodeNetworking string) map[string]string {
	return map[string]string{
		"environment.yaml": `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata: { name: env }
spec:
  baseDomain: bootwright.test

  secrets:
    openshift-pull-secret:
    cluster-admin-ssh-key: { file: ~/ssh.pub }
    provider-host-ssh: { file: ~/ssh }
    vcenter-credentials: { file: ~/vcenter }
`,
		"hosts.yaml": `apiVersion: bootwright.io/v1alpha1
kind: Host
metadata: { name: services-host }
spec:
  addresses:
    - { name: ssh, address: 192.168.133.1 }
  ssh:
    addressName: ssh
    keyRef: { name: provider-host-ssh }
  capabilities: [container-runtime]
`,
		"network.yaml": `apiVersion: bootwright.io/v1alpha1
kind: NetworkConfig
metadata: { name: vsphere-net }
spec:
  machineNetwork:
    - { cidr: 192.168.133.0/24 }
  template:
    networkConfig:
      interfaces:
        - { name: ens192, type: ethernet, state: up, ipv4: { enabled: true, dhcp: false }, ipv6: { enabled: false } }
`,
		"provider.yaml": strings.Replace(`apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata: { name: vsphere }
spec:
  machineProfiles:
    - name: control-plane
      cpu: 8
      memoryMiB: 16384
      diskGiB: 120
      vsphere:
        vcenters:
          - server: vcenter.example.test
            port: 443
            datacenters: [dc1]
            credentialsRef: { name: vcenter-credentials }
        failureDomains:
          - name: dc1-zone-a
            region: dc1
            zone: zone-a
            server: vcenter.example.test
            topology:
              datacenter: dc1
              computeCluster: /dc1/host/cluster1
              datastore: /dc1/datastore/datastore1
              networks: [VM_Network_1, VM_Network_2]
__NODE_NETWORKING__  networkAttachments:
    - name: vsphere-net
      vsphere:
        portgroup: VM_Network_1
`, "__NODE_NETWORKING__", nodeNetworking, 1),
		"cluster.yaml": `apiVersion: bootwright.io/v1alpha1
kind: ClusterInfra
metadata: { name: vsphere-infra }
spec:
  platform:
    type: vsphere
  endpoints:
    api:
      address: 192.168.133.10
      source:
        type: external
    api-int:
      address: 192.168.133.10
      source:
        type: external
    apps:
      address: 192.168.133.11
      source:
        type: external
  networkBindings:
    - networkConfigRef: { name: vsphere-net }
      providerRef: { name: vsphere }
      attachmentRef: { name: vsphere-net }
  components:
    machines:
      - name: master-0
        from: { provider: vsphere, profile: control-plane }
        networkConfig:
          ref: { name: vsphere-net }
          overrides:
            interfaces:
              - name: ens192
                ipv4:
                  address:
                    - { ip: 192.168.133.20, prefix-length: 24 }
---
apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata: { name: vsphere-cluster }
spec:
  distribution:
    type: openshift
    release: { version: 4.21.15 }
  install:
    endpointRefs:
      api:
        name: api
      apiInt:
        name: api-int
      ingress:
        name: apps
    pullSecretRef: { name: openshift-pull-secret }
    nodeSSH:
      keyPairRef: { name: cluster-admin-ssh-key }
  controlPlane: { name: master, replicas: 1 }
  compute:
    - { name: worker, replicas: 0 }
  networking:
    clusterNetwork: [{ cidr: 10.128.0.0/14, hostPrefix: 23 }]
    serviceNetwork: [172.30.0.0/16]
  nodes:
    - hostname: master-0
      role: master
      machineRef: { clusterInfra: vsphere-infra, name: master-0 }
`,
	}
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func replaceBaselineEndpoints(t *testing.T, clusterYAML, endpoints string) string {
	t.Helper()
	old := `    api:
      address: 192.168.132.10
      source:
        type: external
    api-int:
      address: 192.168.132.10
      source:
        type: external
    apps:
      address: 192.168.132.11
      source:
        type: external
`
	if !strings.Contains(clusterYAML, old) {
		t.Fatalf("baseline cluster endpoints block not found")
	}
	return strings.Replace(clusterYAML, old, endpoints, 1)
}

func addLoadBalancerInfraComponent(files map[string]string, name, bindAddresses string) {
	files["infra-component.yaml"] += `---
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata: { name: ` + name + ` }
spec:
  loadBalancer:
    type: haProxy
    hostRef: { name: services-host }
    bindAddresses:
    ` + bindAddresses
}

func newBaselineFiles() map[string]string {
	return map[string]string{
		"environment.yaml":     newEnvironmentYAML,
		"hosts.yaml":           newHostsYAML,
		"network.yaml":         newNetworkConfigYAML,
		"provider.yaml":        newProviderYAML,
		"infra-component.yaml": newInfraComponentYAML,
		"cluster.yaml":         newClusterYAML,
	}
}

const newEnvironmentYAML = `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata: { name: env }
spec:
  baseDomain: bootwright.test
  infraComponents:
    artifactServers:
      - name: default
        type: managed
        componentRef:
          name: artifact-server
        routes:
          redfishVirtualMedia:
            endpoint: bmc

  secrets:
    openshift-pull-secret:
    cluster-admin-ssh-key: { file: ~/ssh.pub }
    provider-host-ssh: { file: ~/ssh }
    bmc-credentials:
      generated: { credentials: { username: admin } }
`

func newEnvironmentYAMLWithResources(resources ...string) string {
	var b strings.Builder
	b.WriteString(`apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata: { name: env }
spec:
  baseDomain: bootwright.test
  infraComponents:
    artifactServers:
      - name: default
        type: managed
        componentRef:
          name: artifact-server
        routes:
          redfishVirtualMedia:
            endpoint: bmc

  resources:
`)
	for _, resource := range resources {
		b.WriteString("    - ")
		b.WriteString(resource)
		b.WriteByte('\n')
	}
	b.WriteString(`  secrets:
    openshift-pull-secret:
    cluster-admin-ssh-key: { file: ~/ssh.pub }
    provider-host-ssh: { file: ~/ssh }
    bmc-credentials:
      generated: { credentials: { username: admin } }
`)
	return b.String()
}

const newHostsYAML = `apiVersion: bootwright.io/v1alpha1
kind: Host
metadata: { name: services-host }
spec:
  addresses:
    - name: ssh
      address: 192.168.132.1
    - name: bmc-lan
      address: 192.168.132.1

  ssh:
    addressName: ssh
    keyRef: { name: provider-host-ssh }
  capabilities: [container-runtime]
`

const newNetworkConfigYAML = `apiVersion: bootwright.io/v1alpha1
kind: NetworkConfig
metadata: { name: cluster-net }
spec:
  machineNetwork:
    - { cidr: 192.168.132.0/24 }
  template:
    networkConfig:
      interfaces:
        - { name: primary, type: ethernet, state: up, ipv4: { enabled: true, dhcp: false }, ipv6: { enabled: false } }
      routes:
        config:
          - { destination: 0.0.0.0/0, next-hop-address: 192.168.132.1, next-hop-interface: primary, table-id: 254 }
`

const newProviderYAML = `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata: { name: rack }
spec:
  machines:
    - name: srv1
      baremetal:
        bootMACAddress: 52:54:00:32:11:10
        interfaces:
          - { name: primary, macAddress: 52:54:00:32:11:10 }
        bmc:
          address: redfish-virtualmedia+http://10.0.0.1/redfish/v1/Systems/1
          credentialsRef: { name: bmc-credentials }
`

const newInfraComponentYAML = `apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata: { name: artifact-server }
spec:
  artifactServer:
    hostRef:
      name: services-host
    listeners:
      - name: https
        protocol: https
        port: 8443
    endpoints:
      - name: bmc
        listener: https
        hostAddress: bmc-lan
`

func baselineMachineNetworkConfigYAML() string {
	return `        networkConfig:
          ref: { name: cluster-net }
          overrides:
            interfaces:
              - name: primary
                ipv4:
                  address:
                    - { ip: 192.168.132.20, prefix-length: 24 }
`
}

func inlineMachineNetworkConfigYAML(extra string) string {
	return `        networkConfig:
` + inlineNetworkConfigSpecYAML() + extra
}

func inlineNetworkConfigSpecYAML() string {
	return `          spec:
            machineNetwork:
              - { cidr: 192.168.132.0/24 }
            template:
              networkConfig:
                interfaces:
                  - name: primary
                    type: ethernet
                    state: up
                    ipv4:
                      enabled: true
                      dhcp: false
                      address:
                        - { ip: 192.168.132.20, prefix-length: 24 }
                    ipv6:
                      enabled: false
                routes:
                  config:
                    - { destination: 0.0.0.0/0, next-hop-address: 192.168.132.1, next-hop-interface: primary, table-id: 254 }
`
}

var newClusterYAML = `apiVersion: bootwright.io/v1alpha1
kind: ClusterInfra
metadata: { name: sno }
spec:
  platform:
    type: baremetal
    baremetal: { provisioningNetwork: disabled }
  endpoints:
    api:
      address: 192.168.132.10
      source:
        type: external
    api-int:
      address: 192.168.132.10
      source:
        type: external
    apps:
      address: 192.168.132.11
      source:
        type: external
  components:
    machines:
      - name: master-0
        from: { provider: rack, name: srv1 }
` + baselineMachineNetworkConfigYAML() + `
---
apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata: { name: sno }
spec:
  distribution:
    type: openshift
    release:
      version: 4.21.15
  install:
    endpointRefs:
      api:
        name: api
      apiInt:
        name: api-int
      ingress:
        name: apps
    method: agent
    mode: connected
    pullSecretRef: { name: openshift-pull-secret }
  controlPlane: { name: master, replicas: 1 }
  compute:
    - { name: worker, replicas: 0 }
  networking:
    clusterNetwork: [{ cidr: 10.128.0.0/14, hostPrefix: 23 }]
    serviceNetwork: [172.30.0.0/16]
  nodes:
    - hostname: master-0
      role: master
      machineRef: { clusterInfra: sno, name: master-0 }
`
