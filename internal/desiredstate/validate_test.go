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
	examplesRoot := filepath.Join("..", "..", "examples")
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
				"install:\n    method: agent\n    mode: connected",
				"installMode: connected\n  install:\n    method: agent\n    mode: connected", 1)},
			wantSubstring: "field installMode not found",
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
				"  infraComponents:\n    artifactServers:\n      - name: default\n        type: managed\n        default: true\n        componentRef:\n          name: artifact-server\n        routes:\n          redfishVirtualMedia:\n            endpoint: bmc\n\n", "", 1)},
			wantSubstring: "requires generated artifact publication; set Environment.spec.infraComponents.artifactServers",
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
				"api:\n      externalVip: 192.168.132.10",
				"api: {}", 1)},
			wantSubstring: "spec.endpoints.api must set exactly one of {vip, externalVip, providedBy}",
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
			name: "clustertrust-duplicate-ref-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"  secrets:\n",
				"  clusterTrust:\n    caBundleRefs:\n      - name: corp-ca\n      - name: corp-ca\n\n  secrets:\n", 1)},
			wantSubstring: `spec.clusterTrust.caBundleRefs[1].name "corp-ca" is duplicated`,
		},
		{
			name: "clustertrust-unknown-ref-rejected",
			files: map[string]string{"environment.yaml": strings.Replace(newEnvironmentYAML,
				"  secrets:\n",
				"  clusterTrust:\n    caBundleRefs:\n      - name: corp-ca\n\n  secrets:\n", 1)},
			wantSubstring: `spec.clusterTrust.caBundleRefs[0] "corp-ca" is not declared`,
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
    clusterInstall: none

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

func TestEnvironmentProxyDefaultsMustBeUnique(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = strings.Replace(files["environment.yaml"], "    artifactServers:\n", `    proxies:
      - name: one
        default: true
        type: external
        connection:
          httpProxy: http://proxy-one.bootwright.test:3128
      - name: two
        default: true
        type: external
        connection:
          httpProxy: http://proxy-two.bootwright.test:3128
    artifactServers:
`, 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected duplicate proxy default error, got nil")
	}
	want := "spec.infraComponents.proxies must not mark more than one entry default"
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

func TestEnvironmentResourcesDoNotHideInvalidImportedFiles(t *testing.T) {
	cases := []struct {
		name           string
		unselectedYAML string
		wantSubstring  string
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
			wantSubstring: "field retiredField not found",
		},
		{
			name: "malformed-yaml",
			unselectedYAML: `apiVersion: bootwright.io/v1alpha1
kind: Host
metadata: [bad
`,
			wantSubstring: "unselected.yaml document 1",
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
			wantSubstring: `Host/spare-host spec.ssh.keyRef "missing-secret" is not declared`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newBaselineFiles()
			files["environment.yaml"] = newEnvironmentYAMLWithResources("hosts.yaml", "network.yaml", "provider.yaml", "infra-component.yaml", "cluster.yaml")
			files["unselected.yaml"] = tc.unselectedYAML
			writeFiles(t, dir, files)
			_, err := LoadNormalizeValidateInputFiles([]string{dir})
			if err == nil {
				t.Fatal("expected invalid unselected input to fail")
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
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
			name: "external-vips-accepted",
		},
		{
			name: "sno-openshift-vips-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      vip: 192.168.132.10
    apiInt:
      vip: 192.168.132.10
    ingress:
      vip: 192.168.132.11
`)
			},
			wantSubstring: "single-node clusters forbid ClusterInfra/sno spec.endpoints.",
		},
		{
			name: "single-bind-address-shortcut-accepted",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      providedBy:
        componentRef: { name: control-plane }
    apiInt:
      providedBy:
        componentRef: { name: control-plane }
    ingress:
      externalVip: 192.168.132.11
`)
				addLoadBalancerInfraComponent(files, "control-plane", "- ip: 192.168.132.10\n")
			},
		},
		{
			name: "named-bind-address-selection-accepted",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      providedBy: { componentRef: { name: load-balancer }, address: control-plane }
    apiInt:
      providedBy: { componentRef: { name: load-balancer }, address: control-plane }
    ingress:
      providedBy: { componentRef: { name: load-balancer }, address: apps }
`)
				addLoadBalancerInfraComponent(files, "load-balancer", "- { name: control-plane, ip: 192.168.132.10 }\n    - { name: apps, ip: 192.168.132.11 }\n")
			},
		},
		{
			name: "missing-bind-address-names-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      providedBy: { componentRef: { name: load-balancer }, address: control-plane }
    apiInt:
      providedBy: { componentRef: { name: load-balancer }, address: control-plane }
    ingress:
      providedBy: { componentRef: { name: load-balancer }, address: apps }
`)
				addLoadBalancerInfraComponent(files, "load-balancer", "- { ip: 192.168.132.10 }\n    - { ip: 192.168.132.11 }\n")
			},
			wantSubstring: "bindAddresses[0].name is required",
		},
		{
			name: "bad-loadbalancer-reference-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      providedBy:
        componentRef: { name: missing }
    apiInt:
      externalVip: 192.168.132.10
    ingress:
      externalVip: 192.168.132.11
`)
			},
			wantSubstring: `providedBy.componentRef.name "missing" does not resolve to an InfraComponent loadBalancer`,
		},
		{
			name: "bad-bind-address-reference-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      providedBy: { componentRef: { name: load-balancer }, address: missing }
    apiInt:
      externalVip: 192.168.132.10
    ingress:
      externalVip: 192.168.132.11
`)
				addLoadBalancerInfraComponent(files, "load-balancer", "- { name: control-plane, ip: 192.168.132.10 }\n")
			},
			wantSubstring: `providedBy.address "missing" does not match`,
		},
		{
			name: "vip-outside-selected-machine-network-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      externalVip: 192.168.140.10
    apiInt:
      externalVip: 192.168.132.10
    ingress:
      externalVip: 192.168.132.11
`)
			},
			wantSubstring: `spec.endpoints.api effective VIP "192.168.140.10" is outside selected NetworkConfig machine networks`,
		},
		{
			name: "multiple-owners-rejected",
			mutate: func(files map[string]string) {
				files["cluster.yaml"] = replaceBaselineEndpoints(t, files["cluster.yaml"], `    api:
      vip: 192.168.132.10
      externalVip: 192.168.132.10
    apiInt:
      externalVip: 192.168.132.10
    ingress:
      externalVip: 192.168.132.11
`)
			},
			wantSubstring: "spec.endpoints.api must set exactly one of {vip, externalVip, providedBy}",
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

func newVSphereFiles(nodeNetworking string) map[string]string {
	return map[string]string{
		"environment.yaml": `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata: { name: env }
spec:
  baseDomain: bootwright.test

  secrets:
    openshift-pull-secret:
    cluster-admin-pub-key: { file: ~/ssh.pub }
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
  vsphere: { portgroup: VM_Network_1 }
`,
		"provider.yaml": `apiVersion: bootwright.io/v1alpha1
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
` + nodeNetworking,
		"cluster.yaml": `apiVersion: bootwright.io/v1alpha1
kind: ClusterInfra
metadata: { name: vsphere-infra }
spec:
  platform:
    type: vsphere
  endpoints:
    api:
      externalVip: 192.168.133.10
    apiInt:
      externalVip: 192.168.133.10
    ingress:
      externalVip: 192.168.133.11
  components:
    machines:
      - name: master-0
        from: { provider: vsphere, profile: control-plane }
        networkConfig:
          ref: { name: vsphere-net }
          addresses:
            - interface: ens192
              ipv4:
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
    pullSecretRef: { name: openshift-pull-secret }
    sshKeyRef: { name: cluster-admin-pub-key }
  controlPlane: { name: master, replicas: 1 }
  compute:
    - { name: worker, replicas: 0 }
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
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func replaceBaselineEndpoints(t *testing.T, clusterYAML, endpoints string) string {
	t.Helper()
	old := `    api:
      externalVip: 192.168.132.10
    apiInt:
      externalVip: 192.168.132.10
    ingress:
      externalVip: 192.168.132.11
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
        default: true
        componentRef:
          name: artifact-server
        routes:
          redfishVirtualMedia:
            endpoint: bmc

  secrets:
    openshift-pull-secret:
    cluster-admin-pub-key: { file: ~/ssh.pub }
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
        default: true
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
    cluster-admin-pub-key: { file: ~/ssh.pub }
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
  physical: {}
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

const newClusterYAML = `apiVersion: bootwright.io/v1alpha1
kind: ClusterInfra
metadata: { name: sno }
spec:
  platform:
    type: baremetal
    baremetal: { provisioningNetwork: disabled }
  endpoints:
    api:
      externalVip: 192.168.132.10
    apiInt:
      externalVip: 192.168.132.10
    ingress:
      externalVip: 192.168.132.11
  components:
    machines:
      - name: master-0
        from: { provider: rack, name: srv1 }
        networkConfig:
          ref: { name: cluster-net }
          addresses:
            - interface: primary
              ipv4:
                - { ip: 192.168.132.20, prefix-length: 24 }
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
