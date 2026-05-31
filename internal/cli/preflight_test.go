package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/locality"
)

func TestPythonVersionCheckUsesInjectedDeps(t *testing.T) {
	check := pythonVersionCheck(preflightDeps{
		lookPath: func(name string, _ []string) (string, error) {
			if name == "python3" {
				return "/bin/python3", nil
			}
			return "", errors.New("not found")
		},
		commandOutput: func(name string, args ...string) ([]byte, error) {
			return []byte("Python 3.12.4"), nil
		},
		uid: func() int { return 1000 },
	})
	if check.Status != "OK" {
		t.Fatalf("python check failed: %+v", check)
	}
	if check.Evidence != "/bin/python3 3.12" {
		t.Fatalf("evidence = %q", check.Evidence)
	}
}

func TestPythonVersionCheckRejectsOldPython(t *testing.T) {
	check := pythonVersionCheck(preflightDeps{
		lookPath: func(name string, _ []string) (string, error) {
			return "/bin/" + name, nil
		},
		commandOutput: func(name string, args ...string) ([]byte, error) {
			return []byte("Python 3.11.9"), nil
		},
		uid: func() int { return 1000 },
	})
	if check.Status == "OK" {
		t.Fatalf("old Python accepted: %+v", check)
	}
}

func TestCollectBastionChecksUsesInjectedUID(t *testing.T) {
	deps := preflightDeps{
		lookPath: func(name string, _ []string) (string, error) {
			return "/bin/" + name, nil
		},
		statPath: func(path string) (os.FileInfo, error) {
			return nil, errors.New("not used")
		},
		commandOutput: func(name string, args ...string) ([]byte, error) {
			return []byte("Python 3.12.4"), nil
		},
		uid: func() int { return 0 },
	}
	checks := collectBastionChecks(deps)
	for _, check := range checks {
		if check.Name == "sudo" {
			t.Fatalf("sudo check should be skipped for injected root UID: %+v", checks)
		}
	}
}

func TestClusterPreflightDoesNotRequireLocalInstallerTools(t *testing.T) {
	deps := preflightDeps{
		lookPath: func(name string, _ []string) (string, error) {
			return "/bin/" + name, nil
		},
		statPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
		commandOutput: func(name string, args ...string) ([]byte, error) {
			return []byte("Python 3.12.4"), nil
		},
		uid: func() int { return 1000 },
	}
	checks := collectPreflightChecks(loadFixtureState(t, "001-sno-libvirt"), []Phase{{Name: "container-cluster"}}, true, "/context/secrets", "/host-state", deps)
	var tools []string
	for _, check := range checks {
		if check.Group == checkGroupInstallerTools {
			tools = append(tools, check.Name)
		}
	}
	if len(tools) != 0 {
		t.Fatalf("installer tools = %#v, want none; installer CLIs run on the bastion host", tools)
	}
}

func TestKubeVirtHostClusterPreflightChecksKubeconfigAndAPI(t *testing.T) {
	clustersDir := t.TempDir()
	kubeconfig := filepath.Join(clustersDir, "metal-ocp", "secrets", "kubeconfig")
	if err := os.MkdirAll(filepath.Dir(kubeconfig), 0o700); err != nil {
		t.Fatalf("mkdir kubeconfig dir: %v", err)
	}
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	state := v1alpha1.State{InfraProviders: []v1alpha1.InfraProvider{{
		Metadata: v1alpha1.Metadata{Name: "child-provider"},
		Spec: v1alpha1.InfraProviderSpec{MachineProfiles: []v1alpha1.MachineProfileCapability{{
			Name: "sno",
			KubeVirt: &v1alpha1.MachineProfileKubeVirtProvisioner{
				HostContainerClusterRef: &v1alpha1.LocalObjectReference{Name: "metal-ocp"},
				Namespace:               "bootwright-child-ocp",
			},
		}}},
	}}}
	deps := preflightDeps{
		lookPath: func(name string, _ []string) (string, error) {
			return "/bin/" + name, nil
		},
		statPath: os.Stat,
		commandOutput: func(name string, args ...string) ([]byte, error) {
			if name == "kubectl" {
				return []byte("customresourcedefinition.apiextensions.k8s.io/virtualmachines.kubevirt.io\n"), nil
			}
			return []byte("Python 3.12.4"), nil
		},
		uid: func() int { return 1000 },
	}

	checks := collectPreflightChecks(state, []Phase{{Name: "cluster-infra"}}, true, "/context/secrets", clustersDir, deps)
	assertPreflightCheckStatus(t, checks, "metal-ocp kubeconfig", "OK")
	assertPreflightCheckStatus(t, checks, "metal-ocp KubeVirt API", "OK")
}

func TestKubeVirtHostClusterPreflightRejectsMissingAPI(t *testing.T) {
	clustersDir := t.TempDir()
	kubeconfig := filepath.Join(clustersDir, "metal-ocp", "secrets", "kubeconfig")
	if err := os.MkdirAll(filepath.Dir(kubeconfig), 0o700); err != nil {
		t.Fatalf("mkdir kubeconfig dir: %v", err)
	}
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	check := kubeVirtAPIReadyCheck("metal-ocp", kubeconfig, preflightDeps{
		commandOutput: func(name string, args ...string) ([]byte, error) {
			return []byte("Error from server (NotFound): customresourcedefinitions.apiextensions.k8s.io \"virtualmachines.kubevirt.io\" not found\n"), errors.New("not found")
		},
	})
	if check.Status == "OK" {
		t.Fatalf("missing KubeVirt API accepted: %+v", check)
	}
	if !strings.Contains(check.Remediation, "bootwright apply addons --scope metal-ocp --yes") {
		t.Fatalf("remediation = %q", check.Remediation)
	}
}

func TestSecretRefChecksAcceptContextAndGeneratedMaterial(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	deps := preflightDeps{
		statPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}
	checks := secretRefChecks(state, "/context/secrets", []Phase{{Name: "container-cluster"}}, deps)

	var pullSecret, generatedBMC *preflightCheck
	for i := range checks {
		check := &checks[i]
		switch {
		case check.Name == "sno-libvirt pullSecretRef":
			pullSecret = check
		case strings.Contains(check.Name, "bmcEmulationDefaults credentialRef"):
			generatedBMC = check
		}
	}
	if pullSecret == nil {
		t.Fatalf("missing pull secret check: %+v", checks)
	}
	if !strings.Contains(pullSecret.Evidence, "/context/secrets/openshift-pull-secret missing") {
		t.Fatalf("pull secret check evidence = %q", pullSecret.Evidence)
	}
	if !strings.Contains(pullSecret.Remediation, "bootwright secret set openshift-pull-secret --pull-secret") {
		t.Fatalf("pull secret remediation = %q", pullSecret.Remediation)
	}
	if generatedBMC == nil {
		t.Fatalf("missing generated BMC check: %+v", checks)
	}
	if !strings.Contains(generatedBMC.Remediation, "bootwright secret generate or bootwright secret set bmc-credentials") {
		t.Fatalf("generated BMC remediation = %q", generatedBMC.Remediation)
	}
}

func TestSecretRefChecksRequireInstallTrustCABundle(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	state.Environments[0].Spec.Secrets["corp-ca"] = v1alpha1.EnvironmentSecretSpec{}
	state.Environments[0].Spec.InstallTrust = &v1alpha1.EnvironmentInstallTrustSpec{
		CABundleRefs: []v1alpha1.SecretRef{{Name: "corp-ca"}},
	}
	checks := secretRefChecks(state, "/context/secrets", []Phase{{Name: "container-cluster"}}, preflightDeps{
		statPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	})

	var caCheck *preflightCheck
	for i := range checks {
		check := &checks[i]
		if check.Name == "environment installTrust caBundleRefs[0]" {
			caCheck = check
			break
		}
	}
	if caCheck == nil {
		t.Fatalf("missing install trust CA check: %+v", checks)
	}
	if !strings.Contains(caCheck.Evidence, "/context/secrets/corp-ca missing") {
		t.Fatalf("install trust CA evidence = %q", caCheck.Evidence)
	}
}

func TestSecretRefChecksRequireImportedCephExternalDetails(t *testing.T) {
	state := importedCephSecretState(v1alpha1.EnvironmentSecretSpec{})
	checks := secretRefChecks(state, "/context/secrets", []Phase{{Name: "addons"}}, preflightDeps{
		statPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	})

	var detailsCheck *preflightCheck
	for i := range checks {
		check := &checks[i]
		if check.Name == "shared-ceph-binding storage[ceph] dataFoundation externalDetailsRef" {
			detailsCheck = check
			break
		}
	}
	if detailsCheck == nil {
		t.Fatalf("missing imported Ceph details check: %+v", checks)
	}
	if !strings.Contains(detailsCheck.Evidence, "/context/secrets/shared-ceph-external-details missing") {
		t.Fatalf("external details evidence = %q", detailsCheck.Evidence)
	}
}

func TestSecretListReportsImportedCephExternalDetailsFile(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "shared-ceph-external-cluster-details.json")
	if err := os.WriteFile(secretPath, []byte("[]\n"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	state := importedCephSecretState(v1alpha1.EnvironmentSecretSpec{File: secretPath})
	entries, err := declaredSecretEntries(t.TempDir(), state)
	if err != nil {
		t.Fatalf("declaredSecretEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want one", entries)
	}
	entry := entries[0]
	if entry.Name != "shared-ceph-external-details" || entry.Type != "file" || !entry.Present {
		t.Fatalf("secret list entry = %+v", entry)
	}
	if len(entry.Paths) != 1 || entry.Paths[0] != secretPath {
		t.Fatalf("secret list paths = %+v, want %s", entry.Paths, secretPath)
	}
}

func TestSecretRefChecksRequireGeneratedSSHKeyPairFiles(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	state.Environments[0].Spec.Secrets[v1alpha1.DefaultNodeSSHKeyName] = v1alpha1.EnvironmentSecretSpec{
		Generated: &v1alpha1.EnvironmentSecretGenerated{
			SSHKeyPair: &v1alpha1.GeneratedSSHKeyPairSpec{Type: v1alpha1.SSHKeyPairTypeEd25519},
		},
	}
	checks := secretRefChecks(state, "/context/secrets", []Phase{{Name: "container-cluster"}}, preflightDeps{
		statPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	})

	var privateCheck, publicCheck *preflightCheck
	for i := range checks {
		check := &checks[i]
		switch check.Name {
		case "sno-libvirt nodeSSH keyPairRef private":
			privateCheck = check
		case "sno-libvirt nodeSSH keyPairRef public":
			publicCheck = check
		}
	}
	if privateCheck == nil || publicCheck == nil {
		t.Fatalf("missing generated SSH key pair checks: %+v", checks)
	}
	if !strings.Contains(privateCheck.Evidence, "/context/secrets/cluster-admin-ssh-key missing") {
		t.Fatalf("private evidence = %q", privateCheck.Evidence)
	}
	if !strings.Contains(publicCheck.Evidence, "/context/secrets/cluster-admin-ssh-key.pub missing") {
		t.Fatalf("public evidence = %q", publicCheck.Evidence)
	}
}

func importedCephSecretState(secretSpec v1alpha1.EnvironmentSecretSpec) v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Spec: v1alpha1.EnvironmentSpec{Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
				"shared-ceph-external-details": secretSpec,
			}},
		}},
		StorageExports: []v1alpha1.StorageExport{{
			Metadata: v1alpha1.Metadata{Name: "shared-ceph-data-foundation"},
			Spec: v1alpha1.StorageExportSpec{
				Type:              v1alpha1.StorageExportTypeDataFoundation,
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "shared-ceph"},
			},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
			Metadata: v1alpha1.Metadata{Name: "shared-ceph-binding"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ClusterRef: v1alpha1.LocalObjectReference{Name: "demo"},
				Storage: []v1alpha1.ClusterAddonBindingStorage{{
					Name:      "ceph",
					ExportRef: v1alpha1.LocalObjectReference{Name: "shared-ceph-data-foundation"},
					DataFoundation: v1alpha1.ClusterAddonBindingStorageDataFoundation{
						ExternalDetailsRef: v1alpha1.SecretRef{Name: "shared-ceph-external-details"},
					},
				}},
			},
		}},
	}
}

func TestClusterPreflightRequiresProviderHostSSHKeyMaterial(t *testing.T) {
	state := loadFixtureState(t, "005-3nodes-baremetal")
	checks := secretRefChecksWithLocalityPolicy(state, "/context/secrets", []Phase{{Name: "container-cluster"}}, preflightDeps{
		statPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}, locality.Policy{Deps: locality.Deps{
		Hostname: func() (string, error) {
			return "controller", nil
		},
	}})

	for _, check := range checks {
		if check.Name == "host bastion keyRef" {
			if check.Status == "OK" {
				t.Fatalf("host SSH key check unexpectedly passed: %+v", check)
			}
			return
		}
	}
	t.Fatalf("missing provider-host SSH key check for container-cluster phase: %+v", checks)
}

func TestClusterPreflightSkipsLoopbackHostSSHKeyMaterial(t *testing.T) {
	state := loadFixtureState(t, "002-sno-emul-baremetal")
	checks := secretRefChecks(state, "/context/secrets", []Phase{{Name: "container-cluster"}}, preflightDeps{
		statPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	})

	for _, check := range checks {
		if check.Name == "host services-host keyRef" {
			t.Fatalf("loopback host SSH key should not be required: %+v", checks)
		}
	}
}

func TestClusterPreflightSkipsControllerHostnameSSHKeyMaterial(t *testing.T) {
	state := loadFixtureState(t, "005-3nodes-baremetal")
	checks := secretRefChecksWithLocalityPolicy(state, "/context/secrets", []Phase{{Name: "container-cluster"}}, preflightDeps{
		statPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}, locality.Policy{Deps: locality.Deps{
		Hostname: func() (string, error) {
			return "bastion", nil
		},
	}})

	for _, check := range checks {
		if check.Name == "host bastion keyRef" {
			t.Fatalf("controller-local host SSH key should not be required: %+v", checks)
		}
	}
}

func TestClusterPreflightRequiresTLSPairMaterial(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	state.Environments[0].Spec.Secrets["api-tls"] = v1alpha1.EnvironmentSecretSpec{}
	state.ContainerClusters[0].Spec.Install.ServingCertificates = &v1alpha1.ServingCertificatesSpec{
		APIServer: &v1alpha1.APIServerServingCertificateSpec{
			NamedCertificates: []v1alpha1.APIServerNamedCertificateSpec{{
				Names:     []string{"api.sno-libvirt.bootwright.test"},
				SecretRef: v1alpha1.SecretRef{Name: "api-tls"},
			}},
		},
	}
	checks := secretRefChecks(state, "/context/secrets", []Phase{{Name: "container-cluster"}}, preflightDeps{
		statPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	})

	var certCheck, keyCheck *preflightCheck
	for i := range checks {
		check := &checks[i]
		switch check.Name {
		case "sno-libvirt apiServer namedCertificates[0] secretRef tls.crt":
			certCheck = check
		case "sno-libvirt apiServer namedCertificates[0] secretRef tls.key":
			keyCheck = check
		}
	}
	if certCheck == nil || keyCheck == nil {
		t.Fatalf("missing TLS pair checks: %+v", checks)
	}
	if !strings.Contains(certCheck.Evidence, "/context/secrets/api-tls missing") {
		t.Fatalf("cert check evidence = %q", certCheck.Evidence)
	}
	if !strings.Contains(keyCheck.Evidence, "/context/secrets/api-tls.key missing") {
		t.Fatalf("key check evidence = %q", keyCheck.Evidence)
	}
}

func TestClusterPreflightDoesNotCheckLocalOCPCLIRelease(t *testing.T) {
	deps := preflightDeps{
		lookPath: func(name string, _ []string) (string, error) {
			return "/usr/local/bin/" + name, nil
		},
		statPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
		commandOutput: func(name string, args ...string) ([]byte, error) {
			switch filepath.Base(name) {
			case "python3":
				return []byte("Python 3.12.4"), nil
			case "oc":
				return []byte("Client Version: 4.21.11\n"), nil
			case "openshift-install":
				return []byte("openshift-install 4.21.10\n"), nil
			default:
				return []byte(""), nil
			}
		},
		uid: func() int { return 1000 },
	}
	checks := collectPreflightChecks(loadFixtureState(t, "001-sno-libvirt"), []Phase{{Name: "container-cluster"}}, true, "/context/secrets", "/host-state", deps)

	for _, name := range []string{"oc", "openshift-install"} {
		for _, check := range checks {
			if check.Name == name {
				t.Fatalf("local %s preflight check still present; installer CLIs run on the bastion host: %+v", name, checks)
			}
		}
	}
}

func TestParseOpenShiftInstallVersionAcceptsAbsoluteBinaryPath(t *testing.T) {
	out := `/usr/local/bin/openshift-install 4.21.15
built from commit a8bea03c72112c0f859af3694676da9483baec99
release image quay.io/openshift-release-dev/ocp-release@sha256:05e69ed54453e3d306b136f52493073073b207f57d0562fe1c8a555bde61aa49
release architecture amd64
`
	if got := parseOpenShiftInstallVersion(out); got != "4.21.15" {
		t.Fatalf("parseOpenShiftInstallVersion = %q, want 4.21.15", got)
	}
}

func TestDefaultLookPathPrefersExtraDirs(t *testing.T) {
	pathDir := t.TempDir()
	extraDir := t.TempDir()
	writeExecutable(t, filepath.Join(pathDir, "openshift-install"))
	want := filepath.Join(extraDir, "openshift-install")
	writeExecutable(t, want)
	t.Setenv("PATH", pathDir)

	got, err := defaultLookPath("openshift-install", []string{extraDir})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("defaultLookPath = %q, want %q", got, want)
	}
}

func TestClustersCheckLimitIncludesInfraAndBootHosts(t *testing.T) {
	limit := ansibleLimitForScope("clusters")
	for _, want := range []string{"bootwright_infra_hosts", "bootwright_ocp_hosts", "bootwright_boot_hosts"} {
		if !strings.Contains(limit, want) {
			t.Fatalf("clusters limit %q missing %q", limit, want)
		}
	}
}

func assertPreflightCheckStatus(t *testing.T, checks []preflightCheck, name, status string) {
	t.Helper()
	for _, check := range checks {
		if check.Name == name {
			if string(check.Status) != status {
				t.Fatalf("%s status = %s, want %s: %+v", name, check.Status, status, check)
			}
			return
		}
	}
	t.Fatalf("preflight check %q not found: %+v", name, checks)
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
