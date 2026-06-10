package desiredstate

import (
	"fmt"
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
		if strings.HasPrefix(name, ".") {
			continue
		}
		// _wip holds intentionally-incomplete scratch examples (gitignored);
		// they are not canonical and are not validated here.
		if name == "_wip" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			_, err := LoadNormalizeValidate([]string{filepath.Join(examplesRoot, name)})
			if err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}
		})
	}
}

func TestHostSSHKnownHostsRefOptionalAndExplicitCompatible(t *testing.T) {
	t.Run("omitted", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, newBaselineFiles())
		if _, err := LoadNormalizeValidate([]string{dir}); err != nil {
			t.Fatalf("LoadNormalizeValidate: %v", err)
		}
	})
	t.Run("explicit-declared", func(t *testing.T) {
		dir := t.TempDir()
		files := newBaselineFiles()
		files["environment.yaml"] = strings.Replace(newEnvironmentYAML,
			"    - bastion-host-ssh:\n        file: ~/ssh\n",
			"    - bastion-host-ssh:\n        file: ~/ssh\n    - bastion-host-known-hosts\n", 1)
		files["service-machines.yaml"] = strings.Replace(newHostsYAML,
			"      keyRef: bastion-host-ssh\n",
			"      keyRef: bastion-host-ssh\n      knownHostsRef: bastion-host-known-hosts\n", 1)
		writeFiles(t, dir, files)
		if _, err := LoadNormalizeValidate([]string{dir}); err != nil {
			t.Fatalf("LoadNormalizeValidate: %v", err)
		}
	})
	t.Run("explicit-undeclared", func(t *testing.T) {
		dir := t.TempDir()
		files := newBaselineFiles()
		files["service-machines.yaml"] = strings.Replace(newHostsYAML,
			"      keyRef: bastion-host-ssh\n",
			"      keyRef: bastion-host-ssh\n      knownHostsRef: bastion-host-known-hosts\n", 1)
		writeFiles(t, dir, files)
		_, err := LoadNormalizeValidate([]string{dir})
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
		want := `Machine/services-host spec.access.ssh.knownHostsRef "bastion-host-known-hosts" is not declared`
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	})
}

func TestOpenShiftManagedVIPFixture(t *testing.T) {
	_, err := LoadNormalizeValidate([]string{filepath.Join("testdata/good", "005-3nodes-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
}

func TestEnvironmentSafetyDestroyProtectionValidation(t *testing.T) {
	for _, value := range []string{v1alpha1.EnvironmentDestroyProtectionAllow, v1alpha1.EnvironmentDestroyProtectionRequiredOverride} {
		t.Run(value, func(t *testing.T) {
			dir := t.TempDir()
			files := newBaselineFiles()
			files["environment.yaml"] = strings.Replace(newEnvironmentYAML,
				"  baseDomain: bootwright.test\n",
				"  baseDomain: bootwright.test\n  safety:\n    destroyProtection: "+value+"\n", 1)
			writeFiles(t, dir, files)
			if _, err := LoadNormalizeValidate([]string{dir}); err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}
		})
	}
}

func TestContainerClusterNetworkingValidation(t *testing.T) {
	cases := []struct {
		name          string
		clusterYAML   string
		wantSubstring string
	}{
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
	want := `Machine/srv1 spec.network.interfaceBinding must bind NetworkConfig interface "secondary" to a hardware NIC for baremetal machines`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestMachineNetworkAcceptsInlineSpec(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	delete(files, "network.yaml")
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"], baselineMachineNetworkConfigYAML(), inlineMachineNetworkConfigYAML(""), 1)
	writeFiles(t, dir, files)
	if _, err := LoadNormalizeValidate([]string{dir}); err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
}

func TestMachineNetworkRejectsRefAndSpec(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"], "      networkConfigRef: cluster-net\n", "      networkConfigRef: cluster-net\n"+inlineNetworkConfigSpecYAML(), 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected ref plus spec validation error, got nil")
	}
	want := "spec.network.config must set only one of networkConfigRef or spec"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestMachineNetworkRejectsInlineSpecOverrides(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	delete(files, "network.yaml")
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"], baselineMachineNetworkConfigYAML(), inlineMachineNetworkConfigYAML(`      overrides:
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
	want := "spec.network.config.overrides is only valid with Machine/srv1 spec.network.config.networkConfigRef"
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
			files["cluster.yaml"] = strings.Replace(newClusterYAML, "metadata: { name: srv1 }\n", "metadata:\n  name: srv1\n  labels:\n"+tc.labelsYAML, 1)
			writeFiles(t, dir, files)
			state, err := LoadNormalizeValidate([]string{dir})
			if tc.wantSubstring == "" {
				if err != nil {
					t.Fatalf("LoadNormalizeValidate: %v", err)
				}
				if got := state.Machines[1].Metadata.Labels["datacenter"]; got != "dc1" {
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

func TestArtifactAccessEndpointNamesSelectInfraEndpoint(t *testing.T) {
	files := newBaselineFiles()
	files["service-machines.yaml"] = strings.Replace(files["service-machines.yaml"],
		"    - name: bmc-lan\n      address: 192.168.132.1",
		"    - name: bmc-lan\n      address: 192.168.132.1\n    - name: dnsAlias\n      address: artifact.example.test", 1)
	files["infra-component.yaml"] = strings.Replace(files["infra-component.yaml"],
		"name: bmc\n        listenerRef: https\n        addressRef: bmc-lan",
		"name: dnsAlias\n        listenerRef: https\n        addressRef: dnsAlias", 1)
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"],
		"endpointRef: bmc",
		"endpointRef: dnsAlias", 1)

	dir := t.TempDir()
	writeFiles(t, dir, files)
	if _, err := LoadNormalizeValidate([]string{dir}); err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
}

func TestEnvironmentArtifactAccessDefaultsValidateInheritedEndpoint(t *testing.T) {
	files := newBaselineFiles()
	files["environment.yaml"] = strings.Replace(files["environment.yaml"],
		"  baseDomain: bootwright.test\n",
		`  baseDomain: bootwright.test
  defaults:
    artifactAccess:
      serverRef: default
      redfishVirtualMedia:
        endpointRef: missing
`, 1)
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"], `    artifactAccess:
      serverRef: default
      redfishVirtualMedia:
        endpointRef: bmc
`, "", 1)

	dir := t.TempDir()
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("LoadNormalizeValidate: expected inherited artifact endpoint error")
	}
	wants := []string{
		// The dangling default fails at its declaration site...
		`Environment/env spec.defaults.artifactAccess.redfishVirtualMedia.endpointRef "missing" does not resolve to the selected artifact server endpoints`,
		// ...and the per-cluster check says the injected value was defaulted,
		// since it appears nowhere in the cluster author's file.
		`ContainerCluster/sno spec.install.artifactAccess.redfishVirtualMedia.endpointRef "missing" does not resolve to the selected artifact server endpoints (defaulted from Environment/env spec.defaults.artifactAccess.redfishVirtualMedia.endpointRef)`,
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

func TestEnvironmentArtifactAccessDefaultsValidateAtDeclarationSite(t *testing.T) {
	cases := []struct {
		name          string
		defaults      string
		wantSubstring string
	}{
		{
			name: "dangling-server-ref",
			defaults: `  defaults:
    artifactAccess:
      serverRef: missing
`,
			wantSubstring: `Environment/env spec.defaults.artifactAccess.serverRef "missing" does not resolve to Environment/env spec.infraComponents.artifactServers[].name`,
		},
		{
			name: "dangling-endpoint-ref",
			defaults: `  defaults:
    artifactAccess:
      serverRef: default
      redfishVirtualMedia:
        endpointRef: missing
`,
			wantSubstring: `Environment/env spec.defaults.artifactAccess.redfishVirtualMedia.endpointRef "missing" does not resolve to the selected artifact server endpoints`,
		},
		{
			name: "endpoint-refs-without-server-ref",
			defaults: `  defaults:
    artifactAccess:
      containerClusterInstall:
        endpointRef: bmc
`,
			wantSubstring: `Environment/env spec.defaults.artifactAccess.serverRef is required when artifactAccess endpoints are set`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The baseline cluster authors its own artifactAccess, so nothing
			// consumes the Environment default; the dangling name must still
			// fail where it was written.
			files := newBaselineFiles()
			files["environment.yaml"] = strings.Replace(files["environment.yaml"],
				"  baseDomain: bootwright.test\n",
				"  baseDomain: bootwright.test\n"+tc.defaults, 1)

			dir := t.TempDir()
			writeFiles(t, dir, files)
			_, err := LoadNormalizeValidate([]string{dir})
			if err == nil {
				t.Fatal("LoadNormalizeValidate: expected declaration-site artifactAccess error")
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
			}
		})
	}
}

func TestDefaultedPullSecretRefErrorSaysDefaulted(t *testing.T) {
	files := newBaselineFiles()
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"],
		"    pullSecretRef: openshift-pull-secret\n", "", 1)
	files["environment.yaml"] = strings.Replace(files["environment.yaml"],
		"    - openshift-pull-secret\n", "", 1)

	dir := t.TempDir()
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("LoadNormalizeValidate: expected defaulted pull secret error")
	}
	want := `ContainerCluster/sno install.pullSecretRef "openshift-pull-secret" is not declared in Environment/env spec.secrets (defaulted; declare the secret or set spec.install.pullSecretRef)`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestAuthoredPullSecretRefErrorHasNoDefaultedNote(t *testing.T) {
	files := newBaselineFiles()
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"],
		"    pullSecretRef: openshift-pull-secret", "    pullSecretRef: my-pull-secret", 1)

	dir := t.TempDir()
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("LoadNormalizeValidate: expected authored pull secret error")
	}
	want := `ContainerCluster/sno install.pullSecretRef "my-pull-secret" is not declared in Environment/env spec.secrets`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
	if strings.Contains(err.Error(), "(defaulted") {
		t.Fatalf("error %q claims an authored ref was defaulted", err)
	}
}

func TestDefaultedNodeSSHKeyPairRefErrorSaysDefaulted(t *testing.T) {
	files := newBaselineFiles()
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"],
		"    nodeSSH:\n      keyPairRef: sno-cluster-admin-ssh-key\n", "", 1)

	// With install.nodeSSH omitted, the derived <cluster>-cluster-admin-ssh-key
	// name resolves against the declared secret.
	dir := t.TempDir()
	writeFiles(t, dir, files)
	if _, err := LoadNormalizeValidate([]string{dir}); err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	// Renaming the cluster re-derives the secret name; the resulting dangling
	// ref must say it was defaulted rather than blame a field the author
	// never wrote.
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"],
		"metadata: { name: sno }", "metadata: { name: sno2 }", 1)
	dir = t.TempDir()
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("LoadNormalizeValidate: expected defaulted nodeSSH secret error")
	}
	want := `ContainerCluster/sno2 install.nodeSSH.keyPairRef "sno2-cluster-admin-ssh-key" is not declared in Environment/env spec.secrets (defaulted; declare the secret or set spec.install.nodeSSH)`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
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
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"- { name: primary, macAddress: 52:54:00:32:11:10 }",
				"- { name: primary, macAddress: 52:54:00:32:11:10, networkRef: cluster-net }", 1)},
			wantSubstring: "field networkRef not found",
		},
		{
			name: "containermachine-infrastructureref-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"distribution:",
				"infrastructureRef: sno\n  distribution:", 1)},
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
				"    pullSecretRef: openshift-pull-secret",
				"    pullSecretRef: openshift-pull-secret\n    sshKeyRef: sno-cluster-admin-ssh-key", 1)},
			wantSubstring: "field sshKeyRef not found",
		},
		{
			name: "containercluster-clusteradminssh-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"    pullSecretRef: openshift-pull-secret",
				"    pullSecretRef: openshift-pull-secret\n    clusterAdminSSH:\n      keyPairRef: sno-cluster-admin-ssh-key", 1)},
			wantSubstring: "field clusterAdminSSH not found",
		},
		{
			name: "environment-default-clusteradminssh-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"  baseDomain: bootwright.test\n",
				"  baseDomain: bootwright.test\n  defaults:\n    install:\n      clusterAdminSSH:\n        keyPairRef: sno-cluster-admin-ssh-key\n", 1)},
			wantSubstring: "field clusterAdminSSH not found",
		},
		{
			name: "host-ssh-address-rejected",
			files: map[string]string{"service-machines.yaml": strings.Replace(newHostsYAML,
				"addressRef: ssh",
				"address: 192.168.132.1", 1)},
			wantSubstring: "field address not found",
		},
		{
			name: "host-service-addresses-rejected",
			files: map[string]string{"service-machines.yaml": strings.Replace(newHostsYAML,
				"capabilities: [container-runtime]",
				"capabilities: [container-runtime]\n  serviceAddresses:\n    bmc: 192.168.132.1", 1)},
			wantSubstring: "field serviceAddresses not found",
		},
		{
			name: "host-duplicate-address-name-rejected",
			files: map[string]string{"service-machines.yaml": strings.Replace(newHostsYAML,
				"addresses:\n    - name: ssh\n      address: 192.168.132.1",
				"addresses:\n    - name: ssh\n      address: 192.168.132.1\n    - name: ssh\n      address: 192.168.132.2", 1)},
			wantSubstring: `spec.addresses[1].name "ssh" is duplicated`,
		},
		{
			name: "host-missing-ssh-address-name-rejected",
			files: map[string]string{"service-machines.yaml": strings.Replace(newHostsYAML,
				"addressRef: ssh",
				"addressRef: missing", 1)},
			wantSubstring: `spec.access.ssh.addressRef "missing" does not resolve`,
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
				"  os:\n",
				"  artifacts: { source: { providerRef: rack, machineRef: default } }\n  os:\n", 1)},
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
				"  infraComponents:\n    artifactServers:\n      - name: default\n        management: managed\n        componentRef: artifact-server\n\n", "", 1)},
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
			name: "environment-infra-ntpsource-scalar-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"  infraComponents:\n",
				"  infraComponents:\n    ntp:\n      - \" ntp.example.test\"\n", 1)},
			wantSubstring: `cannot unmarshal`,
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
				"machineRef: services-host",
				"machineRef: services-host\n    bindAddress: invalid", 1)},
			wantSubstring: `spec.artifactServer.bindAddress "invalid" is not a valid IP address`,
		},
		{
			name: "artifact-server-endpoint-address-rejected",
			files: map[string]string{"infra-component.yaml": strings.Replace(newInfraComponentYAML,
				"addressRef: bmc-lan",
				"addressRef: missing", 1)},
			wantSubstring: `spec.artifactServer.endpoints[0].addressRef "missing" does not resolve to Machine/services-host spec.addresses[].name`,
		},
		{
			name: "artifact-server-endpoint-address-name-rejected",
			files: map[string]string{"infra-component.yaml": strings.Replace(newInfraComponentYAML,
				"addressRef: bmc-lan",
				"addressName: bmc-lan", 1)},
			wantSubstring: "field addressName not found",
		},
		{
			name: "artifact-access-endpoint-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"endpointRef: bmc",
				"endpointRef: missing", 1)},
			wantSubstring: `spec.install.artifactAccess.redfishVirtualMedia.endpointRef "missing" does not resolve to the selected artifact server endpoints`,
		},
		{
			name: "artifact-access-server-ref-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"    artifactAccess:\n      serverRef: default",
				"    artifactAccess:\n      serverRef: missing", 1)},
			wantSubstring: `spec.install.artifactAccess.serverRef "missing" does not resolve to Environment/env spec.infraComponents.artifactServers[].name`,
		},
		{
			name: "environment-artifact-server-routes-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"        componentRef: artifact-server\n\n",
				"        componentRef: artifact-server\n        routes:\n          redfishVirtualMedia:\n            endpoint: bmc\n\n", 1)},
			wantSubstring: "field routes not found",
		},
		{
			name: "environment-external-artifact-server-spec-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"      - name: default\n        management: managed\n        componentRef: artifact-server",
				"      - name: default\n        management: external\n        spec:\n          redfishVirtualMedia"+"URL: https://artifacts.example.test:8443/\n          clusterInstall"+"URL: https://artifacts.example.test:8443/", 1)},
			wantSubstring: "field spec not found",
		},
		{
			name: "containermachine-infranoderef-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"hosts:\n    - hostname: master-0",
				"hosts:\n    - hostname: master-x\n      role: master\n      infraNodeRef: { clusterInstall: other, name: master-x }\n    - hostname: master-0", 1)},
			wantSubstring: "field infraNodeRef not found",
		},
		{
			name: "endpoint-owner-required",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"api:\n        address: 192.168.132.10\n        source:\n          type: external",
				"api: {}", 1)},
			wantSubstring: "spec.install.endpoints.api must set address, dnsName, or source.type=infraComponent",
		},
		{
			name: "missing-machine-ref-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"machineRef: srv1", "machineRef: missing", 1)},
			wantSubstring: `spec.hosts[0].machineRef "missing" does not match any Machine`,
		},
		{
			// machineRef is required: no default is derived from the
			// hostname, so omission fails instead of silently binding a
			// same-named Machine.
			name: "omitted-machine-ref-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"\n      machineRef: srv1", "", 1)},
			wantSubstring: "spec.hosts[0].machineRef is required",
		},
		{
			name: "openshift-pull-secret-required",
			files: map[string]string{
				"cluster.yaml":     strings.Replace(newClusterYAML, "pullSecretRef: openshift-pull-secret", "", 1),
				"environment.yaml": strings.Replace(newEnvironmentYAML, "    - openshift-pull-secret\n", "", 1),
			},
			wantSubstring: `install.pullSecretRef "openshift-pull-secret" is not declared`,
		},
		{
			name: "secret-keyfile-without-file-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"    - bastion-host-ssh:\n        file: ~/ssh",
				"    - bastion-host-ssh:\n        keyFile: ~/ssh.key", 1)},
			wantSubstring: "spec.secrets[bastion-host-ssh].keyFile requires file",
		},
		{
			name: "secretstorage-mode-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"  baseDomain: bootwright.test\n",
				"  baseDomain: bootwright.test\n  secretStorage: { mode: invalid }\n", 1)},
			wantSubstring: `spec.secretStorage.mode "invalid" must be one of {source, context}`,
		},
		{
			name: "destroy-protection-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"  baseDomain: bootwright.test\n",
				"  baseDomain: bootwright.test\n  safety:\n    destroyProtection: production\n", 1)},
			wantSubstring: `spec.safety.destroyProtection "production" must be one of {allow, requiredOverride}`,
		},
		{
			name: "generated-ssh-key-type-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"    - sno-cluster-admin-ssh-key:\n        file: ~/ssh.pub",
				"    - sno-cluster-admin-ssh-key:\n        generated:\n          sshKeyPair:\n            type: rsa", 1)},
			wantSubstring: `spec.secrets[sno-cluster-admin-ssh-key].generated.sshKeyPair.type "rsa" must be "ed25519"`,
		},
		{
			name: "generated-secret-multiple-kinds-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"    - bmc-credentials:\n        generated:\n          credentials:\n            username: admin",
				"    - bmc-credentials:\n        generated:\n          credentials:\n            username: admin\n          sshKeyPair:\n            type: ed25519", 1)},
			wantSubstring: "spec.secrets[bmc-credentials].generated sets more than one generated kind",
		},
		{
			name: "generated-non-ssh-key-for-ssh-ref-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"    - sno-cluster-admin-ssh-key:\n        file: ~/ssh.pub",
				"    - sno-cluster-admin-ssh-key:\n        generated:\n          credentials:\n            username: admin", 1)},
			wantSubstring: `install.nodeSSH.keyPairRef "sno-cluster-admin-ssh-key" uses generated material but Environment/env spec.secrets[sno-cluster-admin-ssh-key].generated is not sshKeyPair`,
		},
		{
			name: "cluster-admin-ssh-mixed-refs-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"    nodeSSH:\n      keyPairRef: sno-cluster-admin-ssh-key",
				"    nodeSSH:\n      keyPairRef: sno-cluster-admin-ssh-key\n      publicKeyRef: sno-cluster-admin-ssh-key", 1)},
			wantSubstring: "spec.install.nodeSSH must use either keyPairRef or publicKeyRef/privateKeyRef, not both",
		},
		{
			name: "cluster-admin-ssh-private-only-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"    nodeSSH:\n      keyPairRef: sno-cluster-admin-ssh-key",
				"    nodeSSH:\n      privateKeyRef: sno-cluster-admin-ssh-key", 1)},
			wantSubstring: "spec.install.nodeSSH publicKeyRef is required when keyPairRef is empty",
		},
		{
			name: "installtrust-duplicate-ref-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"  secrets:\n",
				"  installTrust:\n    caBundleRefs:\n      - corp-ca\n      - corp-ca\n\n  secrets:\n", 1)},
			wantSubstring: `spec.installTrust.caBundleRefs[1] "corp-ca" is duplicated`,
		},
		{
			name: "installtrust-unknown-ref-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"  secrets:\n",
				"  installTrust:\n    caBundleRefs:\n      - corp-ca\n\n  secrets:\n", 1)},
			wantSubstring: `spec.installTrust.caBundleRefs[0] "corp-ca" is not declared`,
		},
		{
			name: "additionaltrust-duplicate-ref-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"    pullSecretRef: openshift-pull-secret\n",
				"    pullSecretRef: openshift-pull-secret\n    additionalTrustBundleRefs:\n      - corp-ca\n      - corp-ca\n", 1)},
			wantSubstring: `spec.install.additionalTrustBundleRefs[1] "corp-ca" is duplicated`,
		},
		{
			name: "api-serving-cert-names-required",
			files: map[string]string{
				"environment.yaml": strings.Replace(newEnvironmentYAML, "    - bastion-host-ssh:\n        file: ~/ssh\n", "    - bastion-host-ssh:\n        file: ~/ssh\n    - api-tls\n", 1),
				"cluster.yaml": strings.Replace(newClusterYAML,
					"    pullSecretRef: openshift-pull-secret\n",
					"    pullSecretRef: openshift-pull-secret\n    servingCertificates:\n      apiServer:\n        namedCertificates:\n          - secretRef: api-tls\n", 1),
			},
			wantSubstring: "namedCertificates[0].names requires at least one DNS name",
		},
		{
			name: "api-serving-cert-api-int-rejected",
			files: map[string]string{
				"environment.yaml": strings.Replace(newEnvironmentYAML, "    - bastion-host-ssh:\n        file: ~/ssh\n", "    - bastion-host-ssh:\n        file: ~/ssh\n    - api-tls\n", 1),
				"cluster.yaml": strings.Replace(newClusterYAML,
					"    pullSecretRef: openshift-pull-secret\n",
					"    pullSecretRef: openshift-pull-secret\n    servingCertificates:\n      apiServer:\n        namedCertificates:\n          - names:\n              - api-int.sno.bootwright.test\n            secretRef: api-tls\n", 1),
			},
			wantSubstring: `must not target the internal API endpoint`,
		},
		{
			name: "serving-cert-file-source-keyfile-required",
			files: map[string]string{
				"environment.yaml": strings.Replace(newEnvironmentYAML, "    - bastion-host-ssh:\n        file: ~/ssh\n", "    - bastion-host-ssh:\n        file: ~/ssh\n    - api-tls:\n        file: ./api.crt\n", 1),
				"cluster.yaml": strings.Replace(newClusterYAML,
					"    pullSecretRef: openshift-pull-secret\n",
					"    pullSecretRef: openshift-pull-secret\n    servingCertificates:\n      apiServer:\n        namedCertificates:\n          - names:\n              - api.sno.bootwright.test\n            secretRef: api-tls\n", 1),
			},
			wantSubstring: `uses file-sourced TLS material but Environment/env spec.secrets[api-tls].keyFile is empty`,
		},
		{
			name: "ingress-serving-cert-unknown-ref-rejected",
			files: map[string]string{"cluster.yaml": strings.Replace(newClusterYAML,
				"    pullSecretRef: openshift-pull-secret\n",
				"    pullSecretRef: openshift-pull-secret\n    servingCertificates:\n      ingress:\n        defaultCertificateRef: ingress-tls\n", 1)},
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

func TestSchemaCompatibilityValidationRejectsKnownIncompatibleFields(t *testing.T) {
	cases := []struct {
		name          string
		mutate        func(map[string]string)
		wantSubstring string
	}{
		{
			name: "provider-artifact-access",
			mutate: func(files map[string]string) {
				files["provider.yaml"] = strings.Replace(files["provider.yaml"], "  baremetal: {}\n", "  baremetal: {}\n  artifactAccess:\n    serverRef: default\n", 1)
			},
			wantSubstring: "InfraProvider/rack spec.artifactAccess is not valid on InfraProvider",
		},
		{
			name: "provider-network-attachment-arm",
			mutate: func(files map[string]string) {
				files["provider.yaml"] = strings.Replace(files["provider.yaml"], "      baremetal: {}\n", "      libvirt:\n        bridge: br0\n", 1)
			},
			wantSubstring: "InfraProvider/rack spec.networkAttachments[cluster-net].libvirt must be empty when InfraProvider/rack spec.type=baremetal",
		},
		{
			name: "machine-os-profile-ref",
			mutate: func(files map[string]string) {
				files["managed-machine.yaml"] = `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata: { name: managed-os }
spec:
  os:
    provided: false
    profileRef: rhel
`
			},
			wantSubstring: "field profileRef not found",
		},
		{
			name: "cluster-artifact-access-provider-ref",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = strings.Replace(files["cluster.yaml"], "    artifactAccess:\n", "    artifactAccess:\n      providerRef: rack\n", 1)
			},
			wantSubstring: "ContainerCluster/sno spec.install.artifactAccess.providerRef is not valid",
		},
		{
			name: "environment-artifact-access-provider-ref",
			mutate: func(files map[string]string) {
				files["environment.yaml"] = strings.Replace(files["environment.yaml"], "  infraComponents:\n", "  defaults:\n    artifactAccess:\n      providerRef: rack\n\n  infraComponents:\n", 1)
			},
			wantSubstring: "Environment/env spec.defaults.artifactAccess.providerRef is not valid",
		},
		{
			name: "external-name-resolution-endpoint",
			mutate: func(files map[string]string) {
				files["environment.yaml"] = strings.Replace(files["environment.yaml"], "    artifactServers:\n", "    nameResolution:\n      - name: dns\n        management: external\n        address: 192.168.132.53\n        endpointRef: cluster\n    artifactServers:\n", 1)
			},
			wantSubstring: "spec.infraComponents.nameResolution[0].endpointRef is only valid for managed nameResolution entries",
		},
		{
			name: "external-registry-component-ref",
			mutate: func(files map[string]string) {
				files["environment.yaml"] = strings.Replace(files["environment.yaml"], "    artifactServers:\n", "    registries:\n      - name: mirror\n        management: external\n        url: registry.example.test:5000\n        componentRef: artifact-server\n    artifactServers:\n", 1)
			},
			wantSubstring: "spec.infraComponents.registries[0].componentRef is only valid for managed registry entries",
		},
		{
			name: "managed-registry-url",
			mutate: func(files map[string]string) {
				files["environment.yaml"] = strings.Replace(files["environment.yaml"], "    artifactServers:\n", "    registries:\n      - name: mirror\n        management: managed\n        componentRef: mirror-registry\n        url: registry.example.test:5000\n    artifactServers:\n", 1)
				files["service-machines.yaml"] = strings.Replace(files["service-machines.yaml"], "capabilities: [container-runtime]", "capabilities: [container-runtime, registry]", 1)
				files["infra-component.yaml"] += `---
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata: { name: mirror-registry }
spec:
  type: registry
  registry:
    implementation: mirror-registry
    machineRef: services-host
`
			},
			wantSubstring: "spec.infraComponents.registries[0].url is only valid for external registry entries",
		},
		{
			name: "machine-install-hostname-source",
			mutate: func(files map[string]string) {
				files["machine-install.yaml"] = machineInstallProfileYAML("rhel", `
    hostname:
      source: static
`)
			},
			wantSubstring: `MachineInstallProfile/rhel spec.customizations.hostname.source "static" must be "machineName"`,
		},
		{
			name: "machine-install-packages-object-environment",
			mutate: func(files map[string]string) {
				files["machine-install.yaml"] = machineInstallProfileYAML("rhel", `
    packages:
      environment: standard
`)
			},
			wantSubstring: `MachineInstallProfile/rhel spec.customizations.packages.environment "standard" must be "minimal"`,
		},
		{
			name: "machine-install-packages-list-rejected",
			mutate: func(files map[string]string) {
				files["machine-install.yaml"] = machineInstallProfileYAML("rhel", `
    packages:
      - cephadm
`)
			},
			wantSubstring: `cannot unmarshal !!seq into v1alpha1.MachineInstallPackages`,
		},
		{
			name: "machine-install-package-name-whitespace",
			mutate: func(files map[string]string) {
				files["machine-install.yaml"] = machineInstallProfileYAML("rhel", `
    packages:
      install:
        - " cephadm"
`)
			},
			wantSubstring: `MachineInstallProfile/rhel spec.customizations.packages.install[0] " cephadm" must not contain leading or trailing whitespace`,
		},
		{
			name: "machine-install-service-disabled-conflict",
			mutate: func(files map[string]string) {
				files["machine-install.yaml"] = machineInstallProfileYAML("rhel", `
    services:
      enabled:
        - sshd
      disabled:
        - sshd
`)
			},
			wantSubstring: `MachineInstallProfile/rhel spec.customizations.services.disabled[0] "sshd" must not also be enabled`,
		},
		{
			name: "machine-install-selinux-mode",
			mutate: func(files map[string]string) {
				files["machine-install.yaml"] = machineInstallProfileYAML("rhel", `
    security:
      selinux:
        mode: warn
`)
			},
			wantSubstring: `MachineInstallProfile/rhel spec.customizations.security.selinux.mode "warn" must be one of: enforcing, permissive, disabled`,
		},
		{
			name: "machine-install-firewall-requires-package",
			mutate: func(files map[string]string) {
				files["machine-install.yaml"] = machineInstallProfileYAML("rhel", `
    packages:
      install:
        - cephadm
    services:
      enabled:
        - sshd
        - firewalld
    security:
      firewall:
        enabled: true
`)
			},
			wantSubstring: `MachineInstallProfile/rhel spec.customizations.security.firewall.enabled requires customizations.packages.install to include firewalld`,
		},
		{
			name: "machine-install-fips-requires-rhel",
			mutate: func(files map[string]string) {
				files["machine-install.yaml"] = machineInstallProfileYAML("fedora", `
    services:
      enabled:
        - sshd
    security:
      fips:
        enabled: true
`)
			},
			wantSubstring: `MachineInstallProfile/rhel spec.customizations.security.fips.enabled is only supported for RHEL install profiles`,
		},
		{
			name: "machine-install-repository-base-url-scheme",
			mutate: func(files map[string]string) {
				files["machine-install.yaml"] = strings.Replace(machineInstallProfileYAML("rhel", `
    services:
      enabled:
        - sshd
`), "      imageRef: rhel-iso\n", "      imageRef: rhel-iso\n      repositories:\n        - id: extras\n          baseURL: bootwright-secret-ref:extras-repo\n", 1)
			},
			wantSubstring: `MachineInstallProfile/rhel spec.installer.anaconda.repositories[0].baseURL must be http:// or https://`,
		},
		{
			name: "machine-os-install-profile-ref-missing",
			mutate: func(files map[string]string) {
				files["managed-machine.yaml"] = managedOSMachineYAML("missing")
			},
			wantSubstring: `Machine/managed-os spec.os.installProfileRef "missing" does not match any MachineInstallProfile`,
		},
		{
			name: "machine-os-install-profile-ref-requires-sshd",
			mutate: func(files map[string]string) {
				files["machine-install.yaml"] = machineInstallProfileYAML("rhel", `
    services:
      enabled:
        - chronyd
`)
				files["managed-machine.yaml"] = managedOSMachineYAML("rhel")
			},
			wantSubstring: `Machine/managed-os spec.os.installProfileRef "rhel" references MachineInstallProfile/rhel without customizations.services.enabled containing sshd`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newBaselineFiles()
			tc.mutate(files)
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

func TestEnvironmentNTPValidateTypedEntries(t *testing.T) {
	files := baselineFilesWithNTPComponent()
	files["environment.yaml"] = environmentYAMLWithNTP(`      - name: external
        management: external
        address: ntp.example.test
      - name: managed
        management: managed
        componentRef: ntp-server
        endpointRef: cluster
`)
	dir := t.TempDir()
	writeFiles(t, dir, files)

	state, err := LoadNormalizeValidate([]string{dir})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	if got := len(state.Environments[0].Spec.InfraComponents.NTP); got != 2 {
		t.Fatalf("infraComponents.ntp got %d, want 2", got)
	}
	if got := state.InfraComponents[1].Spec.NTP.Port; got != v1alpha1.DefaultNTPPort {
		t.Fatalf("ntp default port got %d, want %d", got, v1alpha1.DefaultNTPPort)
	}
}

func TestEnvironmentNTPRejectInvalidTypedEntries(t *testing.T) {
	cases := []struct {
		name          string
		sources       string
		withComponent bool
		wantSubstring string
	}{
		{
			name: "duplicate names",
			sources: `      - name: default
        management: external
        address: ntp.example.test
      - name: default
        management: external
        address: time.example.test
`,
			wantSubstring: `spec.infraComponents.ntp[1].name "default" is duplicated`,
		},
		{
			name: "invalid management value",
			sources: `      - name: default
        management: sometimes
        address: ntp.example.test
`,
			wantSubstring: `spec.infraComponents.ntp[0].management "sometimes" must be one of {external, managed}`,
		},
		{
			// The managed/external axis is spelled management; the stale
			// per-entry type key fails strict decode.
			name: "stale type key rejected",
			sources: `      - name: default
        type: external
        address: ntp.example.test
`,
			wantSubstring: "field type not found in type v1alpha1.EnvironmentNTPComponent",
		},
		{
			name: "invalid external address",
			sources: `      - name: default
        management: external
        address: " ntp.example.test"
`,
			wantSubstring: `spec.infraComponents.ntp[0].address " ntp.example.test" must not contain leading or trailing whitespace`,
		},
		{
			name: "external component ref",
			sources: `      - name: default
        management: external
        address: ntp.example.test
        componentRef: ntp-server
`,
			wantSubstring: `componentRef is only valid for managed ntp entries`,
		},
		{
			name: "managed missing component ref",
			sources: `      - name: default
        management: managed
`,
			wantSubstring: `componentRef is required for managed entries`,
		},
		{
			name: "managed wrong component arm",
			sources: `      - name: default
        management: managed
        componentRef: artifact-server
`,
			wantSubstring: `resolves to InfraComponent/artifact-server without spec.ntp`,
		},
		{
			name: "managed bad endpoint",
			sources: `      - name: default
        management: managed
        componentRef: ntp-server
        endpointRef: missing
`,
			withComponent: true,
			wantSubstring: `endpointRef "missing" does not resolve on selected InfraComponent spec.ntp.endpoints`,
		},
		{
			name: "managed address",
			sources: `      - name: default
        management: managed
        componentRef: ntp-server
        address: ntp.example.test
`,
			withComponent: true,
			wantSubstring: `address is only valid for external ntp entries`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := newBaselineFiles()
			if tc.withComponent {
				files = baselineFilesWithNTPComponent()
			}
			files["environment.yaml"] = environmentYAMLWithNTP(tc.sources)
			dir := t.TempDir()
			writeFiles(t, dir, files)

			_, err := LoadNormalizeValidate([]string{dir})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
			}
		})
	}
}

func TestNTPInfraComponentRejectsInvalidFields(t *testing.T) {
	cases := []struct {
		name          string
		replaceOld    string
		replaceNew    string
		wantSubstring string
	}{
		{
			name:          "implementation",
			replaceOld:    "implementation: chrony",
			replaceNew:    "implementation: ntpd",
			wantSubstring: `spec.ntp.implementation "ntpd" must be "chrony"`,
		},
		{
			name:          "port",
			replaceOld:    "bindAddress: 192.168.132.1",
			replaceNew:    "bindAddress: 192.168.132.1\n    port: 70000",
			wantSubstring: `spec.ntp.port 70000 out of range`,
		},
		{
			name:          "upstream",
			replaceOld:    "time.bootwright.test",
			replaceNew:    `" time.bootwright.test"`,
			wantSubstring: `spec.ntp.upstreamSources[0] " time.bootwright.test" must not contain leading or trailing whitespace`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := baselineFilesWithNTPComponent()
			files["infra-component.yaml"] = strings.Replace(files["infra-component.yaml"], tc.replaceOld, tc.replaceNew, 1)
			dir := t.TempDir()
			writeFiles(t, dir, files)

			_, err := LoadNormalizeValidate([]string{dir})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
			}
		})
	}
}

// TestComponentEndpointRefsRejectObjectForm guards the uniform reference
// grammar on the wrapper-typed endpoint refs: the {name: ...} object form
// fails decode with the shared reference error instead of a raw YAML type
// mismatch.
func TestComponentEndpointRefsRejectObjectForm(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(files map[string]string)
	}{
		{
			name: "artifact server listenerRef",
			mutate: func(files map[string]string) {
				files["infra-component.yaml"] = strings.Replace(files["infra-component.yaml"], "listenerRef: https", "listenerRef: {name: https}", 1)
			},
		},
		{
			name: "artifact server addressRef",
			mutate: func(files map[string]string) {
				files["infra-component.yaml"] = strings.Replace(files["infra-component.yaml"], "addressRef: bmc-lan", "addressRef: {name: bmc-lan}", 1)
			},
		},
		{
			name: "ntp service endpoint addressRef",
			mutate: func(files map[string]string) {
				files["infra-component.yaml"] = strings.Replace(files["infra-component.yaml"], "- name: cluster\n        addressRef: bmc-lan", "- name: cluster\n        addressRef: {name: bmc-lan}", 1)
			},
		},
		{
			name: "environment ntp endpointRef",
			mutate: func(files map[string]string) {
				files["environment.yaml"] = environmentYAMLWithNTP(`      - name: managed
        management: managed
        componentRef: ntp-server
        endpointRef: {name: cluster}
`)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := baselineFilesWithNTPComponent()
			tc.mutate(files)
			dir := t.TempDir()
			writeFiles(t, dir, files)

			_, err := LoadNormalizeValidate([]string{dir})
			if err == nil {
				t.Fatal("expected object-form reference to be rejected")
			}
			if !strings.Contains(err.Error(), "a reference is a plain name string") {
				t.Fatalf("error %q does not reject the object form", err)
			}
		})
	}
}

func TestNameResolutionComponentForwarderValidation(t *testing.T) {
	machines := map[string]v1alpha1.Machine{
		"bastion": {
			Metadata: v1alpha1.Metadata{Name: "bastion"},
			Spec: v1alpha1.MachineSpec{
				Capabilities: []string{v1alpha1.MachineCapabilityContainerRuntime},
			},
		},
	}
	component := func(forwarders []string) v1alpha1.InfraComponent {
		return v1alpha1.InfraComponent{
			Metadata: v1alpha1.Metadata{Name: "lab-dns"},
			Spec: v1alpha1.InfraComponentSpec{
				Type: v1alpha1.ComponentSlotNameResolution, NameResolution: &v1alpha1.NameResolutionComponent{
					Implementation: v1alpha1.InfraComponentTypeDnsmasq,
					MachineRef:     v1alpha1.LocalObjectReference{Name: "bastion"},
					Forwarders:     forwarders,
				}},
		}
	}

	if errs := validateNameResolutionComponent(component([]string{"1.1.1.1", "9.9.9.9"}), machines); len(errs) != 0 {
		t.Fatalf("valid forwarders produced errors: %v", errs)
	}

	errs := strings.Join(validateNameResolutionComponent(component([]string{"", "not-an-ip"}), machines), "\n")
	if !strings.Contains(errs, "spec.nameResolution.forwarders[0] is required") {
		t.Fatalf("missing empty-forwarder error: %s", errs)
	}
	if !strings.Contains(errs, `spec.nameResolution.forwarders[1] "not-an-ip" is not a valid IP address`) {
		t.Fatalf("missing invalid-IP error: %s", errs)
	}
}

func TestNodeSSHSplitRefsValidate(t *testing.T) {
	files := newBaselineFiles()
	files["environment.yaml"] = strings.Replace(newEnvironmentYAML,
		"    - sno-cluster-admin-ssh-key:\n        file: ~/ssh.pub",
		"    - cluster-admin-public:\n        file: ~/ssh.pub\n    - cluster-admin-private:\n        file: ~/ssh", 1)
	files["cluster.yaml"] = strings.Replace(newClusterYAML,
		"    nodeSSH:\n      keyPairRef: sno-cluster-admin-ssh-key",
		"    nodeSSH:\n      publicKeyRef: cluster-admin-public\n      privateKeyRef: cluster-admin-private", 1)
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
				"    pullSecretRef: openshift-pull-secret\n",
				"    pullSecretRef: openshift-pull-secret\n"+tc.fieldYAML, 1)
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
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"], "pullSecretRef: openshift-pull-secret", "", 1)
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
			name:          "malformed digest",
			image:         "quay.io/okd/scos-release@not-a-digest",
			wantSubstring: `spec.distribution.release.image "quay.io/okd/scos-release@not-a-digest" must pin a version tag or digest`,
		},
		{
			name:  "version tag",
			image: "quay.io/okd/scos-release:4.20.0-okd-scos.13",
		},
		{
			name:  "digest",
			image: "quay.io/okd/scos-release@sha256:1111111111111111111111111111111111111111111111111111111111111111",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newBaselineFiles()
			files["cluster.yaml"] = strings.Replace(newClusterYAML,
				"type: openshift\n    release:\n      version: 4.21.15",
				"type: okd\n    release:\n      image: "+tc.image, 1)
			files["cluster.yaml"] = strings.Replace(files["cluster.yaml"], "pullSecretRef: openshift-pull-secret", "", 1)
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
        management: external
        connection:
          httpProxy: http://proxy.bootwright.test:3128
`,
		},
		{
			name: "missing-scheme",
			proxyYAML: `    proxies:
      - name: default
        management: external
        connection:
          httpProxy: proxy.bootwright.test:3128
`,
			wantSubstring: `spec.infraComponents.proxies[0].connection.httpProxy "proxy.bootwright.test:3128" is invalid: must include scheme and host`,
		},
		{
			name: "unsupported-scheme",
			proxyYAML: `    proxies:
      - name: default
        management: external
        connection:
          httpsProxy: socks5://proxy.bootwright.test:1080
`,
			wantSubstring: `spec.infraComponents.proxies[0].connection.httpsProxy "socks5://proxy.bootwright.test:1080" is invalid: scheme must be http or https`,
		},
		{
			name: "inline-credentials",
			proxyYAML: `    proxies:
      - name: default
        management: external
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

func TestUnknownFieldRejectedAcrossAllKinds(t *testing.T) {
	kinds := []string{
		v1alpha1.KindEnvironment,
		v1alpha1.KindMachine,
		v1alpha1.KindMachineImage,
		v1alpha1.KindMachineInstallProfile,
		v1alpha1.KindNetworkConfig,
		v1alpha1.KindInfraProvider,
		v1alpha1.KindInfraComponent,
		v1alpha1.KindContainerCluster,
		v1alpha1.KindStorageCluster,
		v1alpha1.KindStoragePlacementPolicy,
		v1alpha1.KindStoragePool,
		v1alpha1.KindStorageFilesystem,
		v1alpha1.KindStorageObjectGateway,
		v1alpha1.KindStorageExport,
		v1alpha1.KindClusterAddon,
		v1alpha1.KindClusterAddonProfile,
		v1alpha1.KindClusterAddonBinding,
	}
	if len(kinds) != 17 {
		t.Fatalf("expected 17 user-authored kinds, listed %d", len(kinds))
	}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			body := "apiVersion: bootwright.io/v1alpha1\n" +
				"kind: " + kind + "\n" +
				"metadata:\n  name: unknown-field-probe\n" +
				"spec:\n  zzzBootwrightUnknownField: true\n"
			dir := t.TempDir()
			writeFiles(t, dir, map[string]string{"doc.yaml": body})
			_, err := LoadNormalizeValidate([]string{dir})
			if err == nil {
				t.Fatalf("kind %s: expected strict-decode error for an unknown spec field, got nil", kind)
			}
			if !strings.Contains(err.Error(), "zzzBootwrightUnknownField") {
				t.Fatalf("kind %s: error %q does not reject the unknown spec field", kind, err)
			}
		})
	}
}

func TestEnvironmentProxyOldSpecRejectsStrictDecode(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = strings.Replace(files["environment.yaml"], "    artifactServers:\n", `    proxies:
      - name: default
        management: external
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
        management: external
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
        management: external
        address: 192.168.132.1
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
  type: libvirt
  libvirt:
    machineRef: bastion
    uri: qemu:///system
    machineProfiles:
      - name: sno
        cpu: 8
        memoryMiB: 22528
        diskGiB: 120
`,
			wantSubstring: "libvirt.bmcEmulationDefaults is required for current libvirt apply support",
		},
		{
			name: "disabled-emulation",
			providerYAML: `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata: { name: libvirt }
spec:
  type: libvirt
  libvirt:
    machineRef: bastion
    uri: qemu:///system
    bmcEmulationDefaults:
      enabled: false
      auth:
        credentialsRef: bmc-credentials
    machineProfiles:
      - name: sno
        cpu: 8
        memoryMiB: 22528
        diskGiB: 120
`,
			wantSubstring: "libvirt.bmcEmulationDefaults.enabled=false is not supported",
		},
		{
			name: "duplicate-default-ports",
			providerYAML: `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata: { name: libvirt-a }
spec:
  type: libvirt
  libvirt:
    machineRef: bastion
    uri: qemu:///system
    bmcEmulationDefaults:
      auth:
        credentialsRef: bmc-credentials
    machineProfiles:
      - name: sno
        cpu: 8
        memoryMiB: 22528
        diskGiB: 120
---
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata: { name: libvirt-b }
spec:
  type: libvirt
  libvirt:
    machineRef: bastion
    uri: qemu:///system
    bmcEmulationDefaults:
      auth:
        credentialsRef: bmc-credentials
    machineProfiles:
      - name: sno
        cpu: 8
        memoryMiB: 22528
        diskGiB: 120
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
    - bmc-credentials
    - bastion-host-ssh
`,
				"service-machines.yaml": `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata: { name: bastion }
spec:
  capabilities: [libvirt]
  os:
    provided: true
  addresses:
    - { name: ssh, address: 192.168.132.1 }
  access:
    ssh:
      keyRef: bastion-host-ssh
      addressRef: ssh
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
			name:          "malformed digest",
			image:         "quay.io/squid@not-a-digest",
			wantSubstring: `spec.componentImages[proxy][squid].public "quay.io/squid@not-a-digest" must pin a version tag or digest`,
		},
		{
			name:          "mutable tag containing a digit",
			image:         "quay.io/squid:edge-1",
			wantSubstring: `spec.componentImages[proxy][squid].public "quay.io/squid:edge-1" must pin a version tag or digest`,
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
	files["environment.yaml"] = newEnvironmentYAMLWithResources("service-machines.yaml", "network.yaml", "provider.yaml", "infra-component.yaml", "cluster.yaml")
	files["unused.yaml"] = `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata: { name: unused-machine }
spec:
  retiredField: true
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
	files["cluster-b.yaml"] = strings.Replace(newClusterYAML, "metadata: { name: sno }", "metadata: { name: unused }", 1)
	files["cluster-b.yaml"] = strings.Replace(files["cluster-b.yaml"], "metadata: { name: srv1 }", "metadata: { name: unused-srv1 }", 1)
	files["cluster-b.yaml"] = strings.Replace(files["cluster-b.yaml"], "machineRef: srv1", "machineRef: unused-srv1", 1)
	files["cluster-b.yaml"] = strings.Replace(files["cluster-b.yaml"], "providerRef: rack", "providerRef: unused-rack", 1)
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
	if got := len(state.Machines); got != 2 {
		t.Fatalf("Machines = %d, want service host plus selected node", got)
	}
	if got := state.Machines[1].Metadata.Name; got != "srv1" {
		t.Fatalf("selected node machine = %q, want srv1", got)
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
	files["environment.yaml"] = newEnvironmentYAMLWithResources("service-machines.yaml", "network.yaml", "infra-component.yaml", "cluster.yaml")
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected omitted provider to fail")
	}
	want := `spec.resources excludes InfraProvider/rack required by Machine/srv1 spec.substrate.providerRef; add "provider.yaml"`
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
	want := `spec.resources excludes Machine/services-host required by InfraComponent/artifact-server spec.artifactServer.machineRef; add "service-machines.yaml"`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestEnvironmentResourcesRequireReferencedInfraComponent(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = newEnvironmentYAMLWithResources("service-machines.yaml", "network.yaml", "provider.yaml", "cluster.yaml")
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
			"shared/service-machines.yaml",
			"shared/network.yaml",
			"shared/provider.yaml",
			"shared/infra-component.yaml",
			"clusters/sno/cluster.yaml",
		),
		"shared/service-machines.yaml": newHostsYAML,
		"shared/network.yaml":          newNetworkConfigYAML,
		"shared/provider.yaml":         newProviderYAML,
		"shared/infra-component.yaml":  newInfraComponentYAML,
		"clusters/sno/cluster.yaml":    newClusterYAML,
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
		"environment.yaml":             newEnvironmentYAMLWithResources("shared", "clusters/sno"),
		"shared/service-machines.yaml": newHostsYAML,
		"shared/network.yaml":          newNetworkConfigYAML,
		"shared/provider.yaml":         newProviderYAML,
		"shared/infra-component.yaml":  newInfraComponentYAML,
		"clusters/sno/cluster.yaml":    newClusterYAML,
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
	files["environment.yaml"] = strings.Replace(files["environment.yaml"], "  infraComponents:\n    artifactServers:\n      - name: default\n        management: managed\n        componentRef: artifact-server\n\n", "", 1)
	delete(files, "infra-component.yaml")
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
kind: Machine
metadata:
  name: spare-machine
spec:
  retiredField: true
`,
		},
		{
			name: "malformed-yaml",
			unselectedYAML: `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata: [bad
`,
		},
		{
			name: "broken-reference",
			unselectedYAML: `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: spare-machine
spec:
  capabilities:
    - container-runtime
  os:
    provided: true
  addresses:
    - name: ssh
      address: 192.168.132.50
  access:
    ssh:
      keyRef: missing-secret
      addressRef: ssh
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
			files["environment.yaml"] = newEnvironmentYAMLWithResources("service-machines.yaml", "network.yaml", "provider.yaml", "infra-component.yaml", "cluster.yaml")
			files["unselected.yaml"] = tc.unselectedYAML
			writeFiles(t, dir, files)
			state, err := LoadNormalizeValidateInputFiles([]string{dir})
			if err != nil {
				t.Fatalf("LoadNormalizeValidateInputFiles: %v", err)
			}
			if got := len(state.Machines); got != 2 {
				t.Fatalf("Machines = %d, want selected service and cluster machines", got)
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
			wantSubstring: "single-node clusters forbid spec.install.endpoints.api source.type=openshift",
		},
		{
			name: "single-bind-address-shortcut-accepted",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      source:
        type: infraComponent
        componentRef: control-plane
    api-int:
      source:
        type: infraComponent
        componentRef: control-plane
    apps:
      address: 192.168.132.11
      source:
        type: external
`)
				addLoadBalancerInfraComponent(files, "control-plane", "- address: 192.168.132.10\n")
			},
		},
		{
			name: "single-bind-named-selection-accepted",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      source:
        type: infraComponent
        componentRef: control-plane
        bindAddressRef: vip-a
    api-int:
      source:
        type: infraComponent
        componentRef: control-plane
        bindAddressRef: vip-a
    apps:
      address: 192.168.132.11
      source:
        type: external
`)
				addLoadBalancerInfraComponent(files, "control-plane", "- { name: vip-a, address: 192.168.132.10 }\n")
			},
		},
		{
			name: "single-bind-dangling-bind-address-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      source:
        type: infraComponent
        componentRef: control-plane
        bindAddressRef: vip-b
    api-int:
      address: 192.168.132.10
      source:
        type: external
    apps:
      address: 192.168.132.11
      source:
        type: external
`)
				addLoadBalancerInfraComponent(files, "control-plane", "- { name: vip-a, address: 192.168.132.10 }\n")
			},
			wantSubstring: `source.bindAddressRef "vip-b" does not match any bindAddresses[].name`,
		},
		{
			name: "single-bind-literal-ip-bind-address-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      source:
        type: infraComponent
        componentRef: control-plane
        bindAddressRef: 192.168.132.10
    api-int:
      address: 192.168.132.10
      source:
        type: external
    apps:
      address: 192.168.132.11
      source:
        type: external
`)
				addLoadBalancerInfraComponent(files, "control-plane", "- address: 192.168.132.10\n")
			},
			wantSubstring: `source.bindAddressRef "192.168.132.10" does not match any bindAddresses[].name`,
		},
		{
			name: "multi-bind-address-without-selection-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      source:
        type: infraComponent
        componentRef: load-balancer
    api-int:
      source:
        type: infraComponent
        componentRef: load-balancer
        bindAddressRef: control-plane
    apps:
      source:
        type: infraComponent
        componentRef: load-balancer
        bindAddressRef: apps
`)
				addLoadBalancerInfraComponent(files, "load-balancer", "- { name: control-plane, address: 192.168.132.10 }\n    - { name: apps, address: 192.168.132.11 }\n")
			},
			wantSubstring: "spec.install.endpoints.api.source.bindAddressRef is required unless the referenced loadBalancer declares exactly one bindAddress",
		},
		{
			name: "named-bind-address-selection-accepted",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      source:
        type: infraComponent
        componentRef: load-balancer
        bindAddressRef: control-plane
    api-int:
      source:
        type: infraComponent
        componentRef: load-balancer
        bindAddressRef: control-plane
    apps:
      source:
        type: infraComponent
        componentRef: load-balancer
        bindAddressRef: apps
`)
				addLoadBalancerInfraComponent(files, "load-balancer", "- { name: control-plane, address: 192.168.132.10 }\n    - { name: apps, address: 192.168.132.11 }\n")
			},
		},
		{
			name: "missing-bind-address-names-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      source:
        type: infraComponent
        componentRef: load-balancer
        bindAddressRef: control-plane
    api-int:
      source:
        type: infraComponent
        componentRef: load-balancer
        bindAddressRef: control-plane
    apps:
      source:
        type: infraComponent
        componentRef: load-balancer
        bindAddressRef: apps
`)
				addLoadBalancerInfraComponent(files, "load-balancer", "- { address: 192.168.132.10 }\n    - { address: 192.168.132.11 }\n")
			},
			wantSubstring: "bindAddresses[0].name is required",
		},
		{
			name: "bad-loadbalancer-reference-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      source:
        type: infraComponent
        componentRef: missing
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
			wantSubstring: `source.componentRef "missing" does not match any InfraComponent`,
		},
		{
			name: "bad-bind-address-reference-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      source:
        type: infraComponent
        componentRef: load-balancer
        bindAddressRef: missing
    api-int:
      address: 192.168.132.10
      source:
        type: external
    apps:
      address: 192.168.132.11
      source:
        type: external
`)
				addLoadBalancerInfraComponent(files, "load-balancer", "- { name: control-plane, address: 192.168.132.10 }\n    - { name: apps, address: 192.168.132.11 }\n")
			},
			wantSubstring: `source.bindAddressRef "missing" does not match`,
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
			wantSubstring: `spec.install.endpoints.api.address "192.168.140.10" is outside selected NetworkConfig machine networks`,
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
			wantSubstring: `spec.install.endpoints.api.source.type "manual" must be one of`,
		},
		{
			name: "cephadm-source-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      address: 192.168.132.10
      source:
        type: cephadm
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
			wantSubstring: `spec.install.endpoints.api.source.type "cephadm" must be one of {openshift, external, infraComponent}`,
		},
		{
			name: "unknown-endpoint-key-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
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
    monitoring:
      address: 192.168.132.12
      source:
        type: external
`)
			},
			wantSubstring: "spec.install.endpoints.monitoring is not a consumed endpoint; accepted keys are {api, api-int, ingress}",
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

func TestNetworkConfigNameResolutionRefsSelectEnvironmentEntries(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = strings.Replace(files["environment.yaml"], "  infraComponents:\n", `  infraComponents:
    nameResolution:
      - name: default
        management: external
        address: 192.168.132.53
`, 1)
	files["network.yaml"] = strings.Replace(files["network.yaml"], "  template:\n", `  nameResolutionRefs:
    - default
  template:
`, 1)
	writeFiles(t, dir, files)
	if _, err := LoadNormalizeValidate([]string{dir}); err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
}

func TestNetworkConfigsAllowSharedMachineCIDR(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["vm-network.yaml"] = strings.Replace(newNetworkConfigYAML, "name: cluster-net", "name: vm-net", 1)
	files["vm-network.yaml"] = strings.Replace(files["vm-network.yaml"], "next-hop-interface: primary", "next-hop-interface: eth0", 1)
	files["vm-network.yaml"] = strings.Replace(files["vm-network.yaml"], "name: primary", "name: eth0", 1)
	writeFiles(t, dir, files)
	if _, err := LoadNormalizeValidate([]string{dir}); err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
}

func TestNetworkConfigNameResolutionRefsRejectDuplicates(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = strings.Replace(files["environment.yaml"], "  infraComponents:\n", `  infraComponents:
    nameResolution:
      - name: default
        management: external
        address: 192.168.132.53
`, 1)
	files["network.yaml"] = strings.Replace(files["network.yaml"], "  template:\n", `  nameResolutionRefs:
    - default
    - default
  template:
`, 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected duplicate nameResolutionRefs error, got nil")
	}
	want := `NetworkConfig/cluster-net spec.nameResolutionRefs[1] "default" is duplicated`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestNetworkConfigNameResolutionRefsRejectUnknownEnvironmentEntry(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["network.yaml"] = strings.Replace(files["network.yaml"], "  template:\n", `  nameResolutionRefs:
    - missing
  template:
`, 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected unresolved nameResolutionRefs error, got nil")
	}
	want := `NetworkConfig/cluster-net spec.nameResolutionRefs[0] "missing" does not match any Environment spec.infraComponents.nameResolution[].name`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestNetworkConfigRejectsStaleDNSRefs(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["network.yaml"] = strings.Replace(files["network.yaml"], "  template:\n", `  dnsRefs:
    - default
  template:
`, 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected stale dnsRefs field error, got nil")
	}
	if !strings.Contains(err.Error(), "field dnsRefs not found") {
		t.Fatalf("error %q does not reject the stale dnsRefs field", err)
	}
}

func TestNetworkConfigRejectsTemplateNameResolutionRefs(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["network.yaml"] = strings.Replace(files["network.yaml"], "    networkConfig:\n", `    networkConfig:
      nameResolutionRefs:
        - default
`, 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected invalid networkConfig.nameResolutionRefs error, got nil")
	}
	want := "spec.template.networkConfig.nameResolutionRefs is not valid NMState; use spec.nameResolutionRefs instead"
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
	files = newVSphereFiles(`    nodeNetworking:
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
			remove:        "        region: dc1\n",
			wantSubstring: ".vsphere.failureDomains[0].region is required",
		},
		{
			name:          "zone",
			remove:        "        zone: zone-a\n",
			wantSubstring: ".vsphere.failureDomains[0].zone is required",
		},
		{
			name:          "datastore",
			remove:        "          datastore: /dc1/datastore/datastore1\n",
			wantSubstring: ".vsphere.failureDomains[0].topology.datastore is required",
		},
		{
			name:          "networks",
			remove:        "          networks: [VM_Network_1, VM_Network_2]\n",
			wantSubstring: ".vsphere.failureDomains[0].topology.networks is required",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newVSphereFiles(`    nodeNetworking:
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

// TestVSphereFailureDomainRefMustResolve covers F24/F18/F34: a
// machineProfiles[].failureDomainRef that names no spec.vsphere.failureDomains[]
// entry must fail validation instead of flowing raw into Ansible vars.
func TestVSphereFailureDomainRefMustResolve(t *testing.T) {
	dir := t.TempDir()
	files := newVSphereFiles(`    nodeNetworking:
      external:
        networkSubnetCidr: [192.168.133.0/24]
`)
	files["provider.yaml"] = strings.Replace(files["provider.yaml"], "failureDomainRef: dc1-zone-a", "failureDomainRef: dc1-zone-b", 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected dangling failureDomainRef error, got nil")
	}
	want := `InfraProvider/vsphere spec.vsphere.machineProfiles[0].failureDomainRef "dc1-zone-b" does not match any failureDomains[].name`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

// TestVSphereFailureDomainServerMustMatchVCenter covers F59/F24: every
// failureDomains[].server must equal a declared vcenters[].server instead of
// binding by unchecked string equality.
func TestVSphereFailureDomainServerMustMatchVCenter(t *testing.T) {
	dir := t.TempDir()
	files := newVSphereFiles(`    nodeNetworking:
      external:
        networkSubnetCidr: [192.168.133.0/24]
`)
	files["provider.yaml"] = strings.Replace(files["provider.yaml"],
		"        server: vcenter.example.test\n",
		"        server: other-vcenter.example.test\n", 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected unmatched failure domain server error, got nil")
	}
	want := `InfraProvider/vsphere spec.vsphere.failureDomains[0].server "other-vcenter.example.test" does not match any vcenters[].server`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
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
				files["child.yaml"] = strings.Replace(files["child.yaml"], "hostClusterRef: sno", "hostClusterRef: missing", 1)
			},
			wantSubstring: `kubevirt.hostClusterRef "missing" does not match any ContainerCluster`,
		},
		{
			name: "both-host-and-kubeconfig",
			mutate: func(files map[string]string) {
				files["environment.yaml"] = addKubeVirtKubeconfigSecret(files["environment.yaml"])
				files["child.yaml"] = strings.Replace(files["child.yaml"], "hostClusterRef: sno\n    namespace:", "hostClusterRef: sno\n    kubeconfigRef: external-virt-cluster-kubeconfig\n    namespace:", 1)
			},
			wantSubstring: "kubevirt must set exactly one of {hostClusterRef, kubeconfigRef}",
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
				files["child.yaml"] = strings.Replace(files["child.yaml"], "hostClusterRef: sno", "kubeconfigRef: external-virt-cluster-kubeconfig", 1)
			},
		},
		{
			name: "unknown-kubeconfig-ref-secret",
			mutate: func(files map[string]string) {
				files["child.yaml"] = strings.Replace(files["child.yaml"], "hostClusterRef: sno", "kubeconfigRef: external-virt-cluster-kubeconfig", 1)
			},
			wantSubstring: `kubevirt.kubeconfigRef "external-virt-cluster-kubeconfig" is not declared in Environment/env spec.secrets`,
		},
		{
			// An omitted attachmentRef defaults to the networkConfigRef name
			// during normalize, so the binding resolves and validates clean.
			name: "omitted-attachment-ref-defaults-to-network-config",
			mutate: func(files map[string]string) {
				files["child.yaml"] = strings.Replace(files["child.yaml"], "      attachmentRef: child-machine-net\n", "", 1)
			},
		},
		{
			// The networkConfigRef-name default is rejected when the provider
			// declares several attachments: a NetworkConfig rename could
			// silently re-bind the machine, so the choice must be authored.
			name: "defaulted-attachment-ref-ambiguous-across-multiple-attachments",
			mutate: func(files map[string]string) {
				files["child.yaml"] = addSecondKubeVirtNetworkAttachment(files["child.yaml"])
				files["child.yaml"] = strings.Replace(files["child.yaml"], "      attachmentRef: child-machine-net\n", "", 1)
			},
			wantSubstring: `Machine/child-master-0 spec.network.config.attachmentRef was defaulted from networkConfigRef "child-machine-net", but InfraProvider/child-kubevirt-provider declares multiple networkAttachments {child-machine-net, child-storage-net}; author attachmentRef to pick one`,
		},
		{
			// An authored attachmentRef names its attachment explicitly, so
			// several provider attachments are not ambiguous.
			name: "authored-attachment-ref-with-multiple-attachments",
			mutate: func(files map[string]string) {
				files["child.yaml"] = addSecondKubeVirtNetworkAttachment(files["child.yaml"])
			},
		},
		{
			name: "missing-network-attachment",
			mutate: func(files map[string]string) {
				files["child.yaml"] = strings.Replace(files["child.yaml"], "attachmentRef: child-machine-net", "attachmentRef: missing", 1)
			},
			wantSubstring: `Machine/child-master-0 spec.network.config.attachmentRef "missing" does not match any networkAttachments[] on InfraProvider/child-kubevirt-provider`,
		},
		{
			name: "network-attachment-kind-mismatch",
			mutate: func(files map[string]string) {
				files["child.yaml"] = strings.Replace(files["child.yaml"], "      kubevirt:\n        nadRef:\n          name: child-ocp-net\n          namespace: bootwright-child-ocp\n", "      libvirt:\n        bridge: vbr-child\n", 1)
			},
			wantSubstring: `spec.network.config.attachmentRef "child-machine-net" binds to InfraProvider/child-kubevirt-provider networkAttachment of kind "libvirt", but provider type is "kubevirt"`,
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
		t.Fatal("expected KubeVirt hostClusterRef cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "KubeVirt hostClusterRef creates ContainerCluster dependency cycle") {
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

func TestMachineNodeBindingValidation(t *testing.T) {
	containerCluster := func(name string, machineRefs ...string) v1alpha1.ContainerCluster {
		cluster := v1alpha1.ContainerCluster{Metadata: v1alpha1.Metadata{Name: name}}
		for i, ref := range machineRefs {
			cluster.Spec.Hosts = append(cluster.Spec.Hosts, v1alpha1.OCPHostSpec{
				Hostname:   fmt.Sprintf("%s-node-%d", name, i),
				Role:       v1alpha1.NodeRoleMaster,
				MachineRef: v1alpha1.LocalObjectReference{Name: ref},
			})
		}
		return cluster
	}
	storageCluster := func(name string, machineRefs ...string) v1alpha1.StorageCluster {
		cluster := v1alpha1.StorageCluster{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{},
			},
		}
		for i, ref := range machineRefs {
			cluster.Spec.Ceph.Topology.Hosts = append(cluster.Spec.Ceph.Topology.Hosts, v1alpha1.StorageCephHost{
				Hostname:   fmt.Sprintf("%s-host-%d", name, i),
				MachineRef: v1alpha1.LocalObjectReference{Name: ref},
				Site:       "lab",
				Roles:      []string{"mon"},
			})
		}
		return cluster
	}
	cases := []struct {
		name  string
		state v1alpha1.State
		want  []string
	}{
		{
			name: "disjoint-fleet-passes",
			state: v1alpha1.State{
				ContainerClusters: []v1alpha1.ContainerCluster{
					containerCluster("dc1", "dc1-master-0", "dc1-master-1"),
					containerCluster("dc2", "dc2-master-0", "dc2-master-1"),
				},
				StorageClusters: []v1alpha1.StorageCluster{storageCluster("ceph", "ceph-0", "ceph-1")},
			},
		},
		{
			name: "two-container-clusters-sharing-a-machine",
			state: v1alpha1.State{
				ContainerClusters: []v1alpha1.ContainerCluster{
					containerCluster("dc1", "dc1-master-0", "master-1"),
					containerCluster("dc2", "dc2-master-0", "master-1"),
				},
			},
			want: []string{`ContainerCluster/dc2 spec.hosts[1].machineRef "master-1" is already node-bound by ContainerCluster/dc1 spec.hosts[1]; a Machine may be node-bound by at most one cluster`},
		},
		{
			name: "container-and-storage-cluster-sharing-a-machine",
			state: v1alpha1.State{
				ContainerClusters: []v1alpha1.ContainerCluster{containerCluster("dc1", "shared-0")},
				StorageClusters:   []v1alpha1.StorageCluster{storageCluster("ceph", "ceph-0", "shared-0")},
			},
			want: []string{`StorageCluster/ceph spec.ceph.topology.hosts[1].machineRef "shared-0" is already node-bound by ContainerCluster/dc1 spec.hosts[0]; a Machine may be node-bound by at most one cluster`},
		},
		{
			name: "one-cluster-binding-a-machine-twice",
			state: v1alpha1.State{
				ContainerClusters: []v1alpha1.ContainerCluster{containerCluster("dc1", "master-0", "master-0")},
			},
			want: []string{`ContainerCluster/dc1 spec.hosts[1].machineRef "master-0" is already node-bound by spec.hosts[0] in the same cluster`},
		},
		{
			name: "one-storage-cluster-binding-a-machine-twice",
			state: v1alpha1.State{
				StorageClusters: []v1alpha1.StorageCluster{storageCluster("ceph", "ceph-0", "ceph-0")},
			},
			want: []string{`StorageCluster/ceph spec.ceph.topology.hosts[1].machineRef "ceph-0" is already node-bound by spec.ceph.topology.hosts[0] in the same cluster`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := validateMachineNodeBindings(tc.state)
			if len(got) != len(tc.want) {
				t.Fatalf("validateMachineNodeBindings = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("validateMachineNodeBindings[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestClusterRootNameCollisionValidation(t *testing.T) {
	containerCluster := func(name string) v1alpha1.ContainerCluster {
		return v1alpha1.ContainerCluster{Metadata: v1alpha1.Metadata{Name: name}}
	}
	storageCluster := func(name string) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{Metadata: v1alpha1.Metadata{Name: name}}
	}
	cases := []struct {
		name  string
		state v1alpha1.State
		want  []string
	}{
		{
			name: "disjoint-names-pass",
			state: v1alpha1.State{
				ContainerClusters: []v1alpha1.ContainerCluster{containerCluster("dc1"), containerCluster("dc2")},
				StorageClusters:   []v1alpha1.StorageCluster{storageCluster("ceph")},
			},
		},
		{
			name: "container-and-storage-cluster-sharing-a-name",
			state: v1alpha1.State{
				ContainerClusters: []v1alpha1.ContainerCluster{containerCluster("dc1")},
				StorageClusters:   []v1alpha1.StorageCluster{storageCluster("dc1")},
			},
			want: []string{`StorageCluster/dc1 metadata.name "dc1" is already used by ContainerCluster/dc1; ContainerCluster and StorageCluster names share one cluster selection namespace (--clusters, Environment cluster lists)`},
		},
		{
			// Same-kind duplicates stay with the per-kind `duplicate <Kind>`
			// rules; the cross-kind check fires once per colliding name even
			// when the StorageCluster side declares it twice.
			name: "duplicated-storage-name-colliding-once",
			state: v1alpha1.State{
				ContainerClusters: []v1alpha1.ContainerCluster{containerCluster("dc1")},
				StorageClusters:   []v1alpha1.StorageCluster{storageCluster("dc1"), storageCluster("dc1")},
			},
			want: []string{`StorageCluster/dc1 metadata.name "dc1" is already used by ContainerCluster/dc1; ContainerCluster and StorageCluster names share one cluster selection namespace (--clusters, Environment cluster lists)`},
		},
		{
			name: "same-kind-duplicate-alone-passes-here",
			state: v1alpha1.State{
				ContainerClusters: []v1alpha1.ContainerCluster{containerCluster("dc1"), containerCluster("dc1")},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := validateClusterRootNameCollisions(tc.state)
			if len(got) != len(tc.want) {
				t.Fatalf("validateClusterRootNameCollisions = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("validateClusterRootNameCollisions[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestSameKindDuplicateClusterNamesKeepPerKindErrors(t *testing.T) {
	cases := []struct {
		name  string
		state v1alpha1.State
		want  string
	}{
		{
			name: "duplicate-container-cluster",
			state: v1alpha1.State{
				ContainerClusters: []v1alpha1.ContainerCluster{
					{Metadata: v1alpha1.Metadata{Name: "dc1"}},
					{Metadata: v1alpha1.Metadata{Name: "dc1"}},
				},
			},
			want: `duplicate ContainerCluster "dc1"`,
		},
		{
			name: "duplicate-storage-cluster",
			state: v1alpha1.State{
				StorageClusters: []v1alpha1.StorageCluster{
					{Metadata: v1alpha1.Metadata{Name: "ceph"}},
					{Metadata: v1alpha1.Metadata{Name: "ceph"}},
				},
			},
			want: `duplicate StorageCluster "ceph"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.state)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "share one cluster selection namespace") {
				t.Fatalf("same-kind duplicate must not trip the cross-kind collision rule: %q", err)
			}
		})
	}
}

func TestMachineNodeBindingExclusivityAcrossClusters(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	_, clusterDoc, ok := strings.Cut(newClusterYAML, "---\n")
	if !ok {
		t.Fatal("baseline cluster YAML is missing the Machine document separator")
	}
	clusterB := strings.Replace(clusterDoc, "metadata: { name: sno }", "metadata: { name: sno-b }", 1)
	clusterB = strings.Replace(clusterB, "address: 192.168.132.10", "address: 192.168.132.12", 2)
	clusterB = strings.Replace(clusterB, "address: 192.168.132.11", "address: 192.168.132.13", 1)
	files["cluster-b.yaml"] = clusterB
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected two clusters node-binding one machine to fail")
	}
	want := `ContainerCluster/sno-b spec.hosts[0].machineRef "srv1" is already node-bound by ContainerCluster/sno spec.hosts[0]; a Machine may be node-bound by at most one cluster`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func newKubeVirtChildFiles() map[string]string {
	files := newBaselineFiles()
	files["extension.yaml"] = strings.Replace(extensionYAML("openshift-virtualization"), "  type: olm\n", "  type: olm\n  provides:\n    - kubevirt\n", 1)
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
kind: Machine
metadata: { name: child-master-0 }
spec:
  capabilities: [openshift-node]
  substrate:
    providerRef: child-kubevirt-provider
    profileRef: child-sno
  os:
    provided: false
  network:
    config:
      networkConfigRef: child-machine-net
      attachmentRef: child-machine-net
      overrides:
        interfaces:
          - name: primary
            ipv4:
              address:
                - { ip: 192.168.134.20, prefix-length: 24 }
  addresses:
    - { name: ip, address: 192.168.134.20 }
---
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata: { name: child-kubevirt-provider }
spec:
  type: kubevirt
  kubevirt:
    hostClusterRef: sno
    namespace: bootwright-child-ocp
    storageClassRef: lvms-vg1
    machineProfiles:
      - name: child-sno
        cpu: 8
        memoryMiB: 16384
        diskGiB: 120
  networkAttachments:
    - name: child-machine-net
      kubevirt:
        nadRef:
          name: child-ocp-net
          namespace: bootwright-child-ocp
---
apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata: { name: child-ocp }
spec:
  distribution:
    type: openshift
    release: { version: 4.21.15 }
  install:
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
      ingress:
        address: 192.168.134.21
        source:
          type: external
    method: agent
    mode: connected
    pullSecretRef: openshift-pull-secret
    nodeSSH:
      keyPairRef: sno-cluster-admin-ssh-key
  controlPlane: { replicas: 1 }
  compute:
    - { replicas: 0 }
  networking:
    clusterNetwork: [{ cidr: 10.128.0.0/14, hostPrefix: 23 }]
    serviceNetwork: [172.30.0.0/16]
  hosts:
    - hostname: master-0
      role: master
      machineRef: child-master-0
`
	return files
}

func addKubeVirtKubeconfigSecret(environmentYAML string) string {
	return strings.Replace(environmentYAML, "    - bmc-credentials:\n", "    - external-virt-cluster-kubeconfig:\n        file: ~/virt.kubeconfig\n    - bmc-credentials:\n", 1)
}

func addSecondKubeVirtNetworkAttachment(childYAML string) string {
	return strings.Replace(childYAML,
		"    - name: child-machine-net\n      kubevirt:\n        nadRef:\n          name: child-ocp-net\n          namespace: bootwright-child-ocp\n",
		"    - name: child-machine-net\n      kubevirt:\n        nadRef:\n          name: child-ocp-net\n          namespace: bootwright-child-ocp\n    - name: child-storage-net\n      kubevirt:\n        nadRef:\n          name: child-storage-net\n          namespace: bootwright-child-ocp\n",
		1)
}

func newKubeVirtCycleFiles() map[string]string {
	return map[string]string{
		"environment.yaml": `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata: { name: env }
spec:
  baseDomain: bootwright.test
  secrets:
    - openshift-pull-secret
    - sno-cluster-admin-ssh-key:
        file: ~/ssh.pub
`,
		"network-a.yaml": kubeVirtCycleNetworkYAML("net-a", "192.168.140.0/24", "192.168.140.1"),
		"network-b.yaml": kubeVirtCycleNetworkYAML("net-b", "192.168.141.0/24", "192.168.141.1"),
		"provider-a.yaml": `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata: { name: provider-a }
spec:
  type: kubevirt
  kubevirt:
    hostClusterRef: cluster-b
    namespace: ns-a
    machineProfiles:
      - name: cp
        cpu: 4
        memoryMiB: 8192
        diskGiB: 80
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
  type: kubevirt
  kubevirt:
    hostClusterRef: cluster-a
    namespace: ns-b
    machineProfiles:
      - name: cp
        cpu: 4
        memoryMiB: 8192
        diskGiB: 80
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
  type: manifestSet
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
  clusterRef: cluster-a
  addons:
    - addonRef: openshift-virtualization
`,
		"binding-b.yaml": `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata: { name: virt-b }
spec:
  clusterRef: cluster-b
  addons:
    - addonRef: openshift-virtualization
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
kind: Machine
metadata: { name: ` + name + `-master-0 }
spec:
  capabilities: [openshift-node]
  substrate:
    providerRef: ` + provider + `
    profileRef: cp
  os:
    provided: false
  network:
    config:
      networkConfigRef: ` + network + `
      attachmentRef: ` + network + `
      overrides:
        interfaces:
          - name: primary
            ipv4:
              address:
                - { ip: ` + nodeIP + `, prefix-length: 24 }
  addresses:
    - { name: ip, address: ` + nodeIP + ` }
`
}

func kubeVirtCycleClusterYAML(name, infra string) string {
	nodeIP := "192.168.140.20"
	ingressIP := "192.168.140.21"
	if infra == "infra-b" {
		nodeIP = "192.168.141.20"
		ingressIP = "192.168.141.21"
	}
	return `apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata: { name: ` + name + ` }
spec:
  distribution:
    type: openshift
    release: { version: 4.21.15 }
  install:
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
      ingress:
        address: ` + ingressIP + `
        source:
          type: external
    method: agent
    mode: connected
    pullSecretRef: openshift-pull-secret
    nodeSSH:
      keyPairRef: sno-cluster-admin-ssh-key
  controlPlane: { replicas: 1 }
  compute:
    - { replicas: 0 }
  networking:
    clusterNetwork: [{ cidr: 10.128.0.0/14, hostPrefix: 23 }]
    serviceNetwork: [172.30.0.0/16]
  hosts:
    - hostname: master-0
      role: master
      machineRef: ` + infra + `-master-0
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
    - openshift-pull-secret
    - sno-cluster-admin-ssh-key:
        file: ~/ssh.pub
    - bastion-host-ssh:
        file: ~/ssh
    - vcenter-credentials:
        file: ~/vcenter
`,
		"service-machines.yaml": `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata: { name: services-host }
spec:
  capabilities: [container-runtime]
  os:
    provided: true
  addresses:
    - { name: ssh, address: 192.168.133.1 }
  access:
    ssh:
      keyRef: bastion-host-ssh
      addressRef: ssh
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
  type: vsphere
  vsphere:
    vcenters:
      - server: vcenter.example.test
        port: 443
        datacenters: [dc1]
        credentialsRef: vcenter-credentials
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
__NODE_NETWORKING__    machineProfiles:
      - name: control-plane
        cpu: 8
        memoryMiB: 16384
        diskGiB: 120
        template: rhcos
        failureDomainRef: dc1-zone-a
  networkAttachments:
    - name: vsphere-net
      vsphere:
        portgroup: VM_Network_1
`, "__NODE_NETWORKING__", nodeNetworking, 1),
		"cluster.yaml": `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata: { name: vsphere-master-0 }
spec:
  capabilities: [openshift-node]
  substrate:
    providerRef: vsphere
    profileRef: control-plane
  os:
    provided: false
  network:
    config:
      networkConfigRef: vsphere-net
      attachmentRef: vsphere-net
      overrides:
        interfaces:
          - name: ens192
            ipv4:
              address:
                - { ip: 192.168.133.20, prefix-length: 24 }
  addresses:
    - { name: ip, address: 192.168.133.20 }
---
apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata: { name: vsphere-cluster }
spec:
  distribution:
    type: openshift
    release: { version: 4.21.15 }
  install:
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
      ingress:
        address: 192.168.133.11
        source:
          type: external
    pullSecretRef: openshift-pull-secret
    nodeSSH:
      keyPairRef: sno-cluster-admin-ssh-key
  controlPlane: { replicas: 1 }
  compute:
    - { replicas: 0 }
  networking:
    clusterNetwork: [{ cidr: 10.128.0.0/14, hostPrefix: 23 }]
    serviceNetwork: [172.30.0.0/16]
  hosts:
    - hostname: master-0
      role: master
      machineRef: vsphere-master-0
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
	old := `      api:
        address: 192.168.132.10
        source:
          type: external
      api-int:
        address: 192.168.132.10
        source:
          type: external
      ingress:
        address: 192.168.132.11
        source:
          type: external
`
	if !strings.Contains(clusterYAML, old) {
		t.Fatalf("baseline cluster endpoints block not found")
	}
	return strings.Replace(clusterYAML, old, normalizeEndpointTestBlock(endpoints), 1)
}

func normalizeEndpointTestBlock(endpoints string) string {
	endpoints = strings.ReplaceAll(endpoints, "apps:", "ingress:")
	lines := strings.Split(endpoints, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = "  " + line
		}
	}
	return strings.Join(lines, "\n")
}

func addLoadBalancerInfraComponent(files map[string]string, name, bindAddresses string) {
	files["infra-component.yaml"] += `---
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata: { name: ` + name + ` }
spec:
  type: loadBalancer
  loadBalancer:
    implementation: haproxy
    machineRef: services-host
    bindAddresses:
    ` + bindAddresses
}

func newBaselineFiles() map[string]string {
	return map[string]string{
		"environment.yaml":      newEnvironmentYAML,
		"service-machines.yaml": newHostsYAML,
		"network.yaml":          newNetworkConfigYAML,
		"provider.yaml":         newProviderYAML,
		"infra-component.yaml":  newInfraComponentYAML,
		"cluster.yaml":          newClusterYAML,
	}
}

func baselineFilesWithNTPComponent() map[string]string {
	files := newBaselineFiles()
	files["infra-component.yaml"] = files["infra-component.yaml"] + "---\n" + newNTPComponentYAML
	return files
}

func machineInstallProfileYAML(osFamily, customizations string) string {
	return `apiVersion: bootwright.io/v1alpha1
kind: MachineImage
metadata: { name: rhel-iso }
spec:
  type: iso
  url: local-media:rhel-9.4.iso
---
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata: { name: rhel }
spec:
  os:
    family: ` + osFamily + `
    version: "9.4"
    architecture: x86_64
  installer:
    type: anaconda
    anaconda:
      imageRef: rhel-iso
  customizations:
` + customizations
}

func managedOSMachineYAML(profileName string) string {
	return `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata: { name: managed-os }
spec:
  capabilities: [ceph-node]
  substrate:
    providerRef: rack
  hardware:
    nics:
      - { name: primary, macAddress: 52:54:00:32:11:11 }
    boot:
      nicRef: primary
    management:
      bmc:
        address: redfish-virtualmedia+http://10.0.0.2/redfish/v1/Systems/1
        credentialsRef: bmc-credentials
  os:
    provided: false
    installProfileRef: ` + profileName + `
    install:
      rootDeviceHints:
        deviceName: /dev/sda
` + baselineMachineNetworkConfigYAML() + `
  addresses:
    - { name: ip, address: 192.168.132.20 }
  access:
    ssh:
      keyRef: bastion-host-ssh
      addressRef: ip
`
}

func environmentYAMLWithNTP(sources string) string {
	return strings.Replace(newEnvironmentYAML, "    artifactServers:\n", "    ntp:\n"+sources+"    artifactServers:\n", 1)
}

const newEnvironmentYAML = `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata: { name: env }
spec:
  baseDomain: bootwright.test
  infraComponents:
    artifactServers:
      - name: default
        management: managed
        componentRef: artifact-server

  secrets:
    - openshift-pull-secret
    - sno-cluster-admin-ssh-key:
        file: ~/ssh.pub
    - bastion-host-ssh:
        file: ~/ssh
    - bmc-credentials:
        generated:
          credentials:
            username: admin
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
        management: managed
        componentRef: artifact-server

  resources:
`)
	for _, resource := range resources {
		b.WriteString("    - ")
		b.WriteString(resource)
		b.WriteByte('\n')
	}
	b.WriteString(`  secrets:
    - openshift-pull-secret
    - sno-cluster-admin-ssh-key:
        file: ~/ssh.pub
    - bastion-host-ssh:
        file: ~/ssh
    - bmc-credentials:
        generated:
          credentials:
            username: admin
`)
	return b.String()
}

const newHostsYAML = `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata: { name: services-host }
spec:
  capabilities: [container-runtime]
  os:
    provided: true
  addresses:
    - name: ssh
      address: 192.168.132.1
    - name: bmc-lan
      address: 192.168.132.1
  access:
    ssh:
      keyRef: bastion-host-ssh
      addressRef: ssh
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
  type: baremetal
  baremetal: {}
  networkAttachments:
    - name: cluster-net
      baremetal: {}
`

const newInfraComponentYAML = `apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata: { name: artifact-server }
spec:
  type: artifactServer
  artifactServer:
    machineRef: services-host
    listeners:
      - name: https
        protocol: https
        port: 8443
    endpoints:
      - name: bmc
        listenerRef: https
        addressRef: bmc-lan
`

const newNTPComponentYAML = `apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata: { name: ntp-server }
spec:
  type: ntp
  ntp:
    implementation: chrony
    machineRef: services-host
    bindAddress: 192.168.132.1
    endpoints:
      - name: cluster
        addressRef: bmc-lan
    upstreamSources:
      - time.bootwright.test
`

func baselineMachineNetworkConfigYAML() string {
	return `  network:
    config:
      networkConfigRef: cluster-net
      attachmentRef: cluster-net
      overrides:
        interfaces:
          - name: primary
            ipv4:
              address:
                - { ip: 192.168.132.20, prefix-length: 24 }
    interfaceBinding:
      - nicRef: primary
        interfaceName: primary
`
}

func inlineMachineNetworkConfigYAML(extra string) string {
	return `  network:
    config:
` + inlineNetworkConfigSpecYAML() + extra + `    interfaceBinding:
      - nicRef: primary
        interfaceName: primary
`
}

func inlineNetworkConfigSpecYAML() string {
	return `      spec:
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
kind: Machine
metadata: { name: srv1 }
spec:
  capabilities: [openshift-node]
  substrate:
    providerRef: rack
  hardware:
    nics:
      - { name: primary, macAddress: 52:54:00:32:11:10 }
    boot:
      nicRef: primary
    management:
      bmc:
        address: redfish-virtualmedia+http://10.0.0.1/redfish/v1/Systems/1
        credentialsRef: bmc-credentials
  os:
    provided: false
` + baselineMachineNetworkConfigYAML() + `
  addresses:
    - { name: ip, address: 192.168.132.20 }
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
      ingress:
        address: 192.168.132.11
        source:
          type: external
    artifactAccess:
      serverRef: default
      redfishVirtualMedia:
        endpointRef: bmc
    method: agent
    mode: connected
    pullSecretRef: openshift-pull-secret
    nodeSSH:
      keyPairRef: sno-cluster-admin-ssh-key
  controlPlane: { replicas: 1 }
  compute:
    - { replicas: 0 }
  networking:
    clusterNetwork: [{ cidr: 10.128.0.0/14, hostPrefix: 23 }]
    serviceNetwork: [172.30.0.0/16]
  hosts:
    - hostname: master-0
      role: master
      machineRef: srv1
`
