package preflight

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
	check := pythonVersionCheck(Deps{
		LookPath: func(name string, _ []string) (string, error) {
			if name == "python3" {
				return "/bin/python3", nil
			}
			return "", errors.New("not found")
		},
		CommandOutput: func(name string, args ...string) ([]byte, error) {
			return []byte("Python 3.12.4"), nil
		},
		UID: func() int { return 1000 },
	})
	if check.Status != "OK" {
		t.Fatalf("python check failed: %+v", check)
	}
	if check.Evidence != "/bin/python3 3.12" {
		t.Fatalf("evidence = %q", check.Evidence)
	}
}

func TestPythonVersionCheckRejectsOldPython(t *testing.T) {
	check := pythonVersionCheck(Deps{
		LookPath: func(name string, _ []string) (string, error) {
			return "/bin/" + name, nil
		},
		CommandOutput: func(name string, args ...string) ([]byte, error) {
			return []byte("Python 3.11.9"), nil
		},
		UID: func() int { return 1000 },
	})
	if check.Status == "OK" {
		t.Fatalf("old Python accepted: %+v", check)
	}
}

func TestCollectBastionChecksUsesInjectedUID(t *testing.T) {
	deps := Deps{
		LookPath: func(name string, _ []string) (string, error) {
			return "/bin/" + name, nil
		},
		StatPath: func(path string) (os.FileInfo, error) {
			return nil, errors.New("not used")
		},
		CommandOutput: func(name string, args ...string) ([]byte, error) {
			return []byte("Python 3.12.4"), nil
		},
		UID: func() int { return 0 },
	}
	checks := CollectBastionChecks(deps)
	for _, check := range checks {
		if check.Name == "sudo" {
			t.Fatalf("sudo check should be skipped for injected root UID: %+v", checks)
		}
	}
}

func TestClusterPreflightDoesNotRequireLocalInstallerTools(t *testing.T) {
	deps := Deps{
		LookPath: func(name string, _ []string) (string, error) {
			return "/bin/" + name, nil
		},
		StatPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
		CommandOutput: func(name string, args ...string) ([]byte, error) {
			return []byte("Python 3.12.4"), nil
		},
		UID: func() int { return 1000 },
	}
	checks := CollectChecks(loadFixtureState(t, "001-sno-libvirt"), []Phase{{Name: "base"}}, true, "test", "/context/secrets", "/host-state", deps, nil, nil)
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
		Spec: v1alpha1.InfraProviderSpec{
			Type: v1alpha1.ProvisionerKubeVirt,
			KubeVirt: &v1alpha1.InfraProviderKubeVirt{
				HostClusterRef: &v1alpha1.LocalObjectReference{Name: "metal-ocp"},
				Namespace:      "bootwright-child-ocp",
				MachineProfiles: []v1alpha1.MachineProfile{{
					Name: "sno",
				}},
			},
		},
	}}}
	deps := Deps{
		LookPath: func(name string, _ []string) (string, error) {
			return "/bin/" + name, nil
		},
		StatPath: os.Stat,
		CommandOutput: func(name string, args ...string) ([]byte, error) {
			return []byte("Python 3.12.4"), nil
		},
		CommandOutputLocalRoot: func(name string, args ...string) ([]byte, error) {
			if name == "kubectl" {
				return []byte("customresourcedefinition.apiextensions.k8s.io/virtualmachines.kubevirt.io\n"), nil
			}
			return []byte("Python 3.12.4"), nil
		},
		UID: func() int { return 1000 },
	}

	checks := CollectChecks(state, []Phase{{Name: "machines"}}, true, "test", "/context/secrets", clustersDir, deps, nil, nil)
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
	check := kubeVirtAPIReadyCheck("metal-ocp", kubeconfig, Deps{
		CommandOutputLocalRoot: func(name string, args ...string) ([]byte, error) {
			return []byte("Error from server (NotFound): customresourcedefinitions.apiextensions.k8s.io \"virtualmachines.kubevirt.io\" not found\n"), errors.New("not found")
		},
	})
	if check.Status == "OK" {
		t.Fatalf("missing KubeVirt API accepted: %+v", check)
	}
	if !strings.Contains(check.Remediation, "bootwright apply --stage clusters --clusters metal-ocp --yes") {
		t.Fatalf("remediation = %q", check.Remediation)
	}
}

// TestKubeVirtHostClusterSelfProvisionedDefersMissingKubeconfig pins the
// self-substrate fix: when the KubeVirt host cluster is itself a ContainerCluster
// this run installs (present in the plan state, base phase in scope), a
// not-yet-produced kubeconfig is expected — the scheduler installs the host before
// the child boots — so the check is INFO, not a FAIL that blocks a full apply.
func TestKubeVirtHostClusterSelfProvisionedDefersMissingKubeconfig(t *testing.T) {
	clustersDir := t.TempDir() // no kubeconfig on disk: host not installed yet
	state := v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "metal-ocp"},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "child-provider"},
			Spec: v1alpha1.InfraProviderSpec{
				Type: v1alpha1.ProvisionerKubeVirt,
				KubeVirt: &v1alpha1.InfraProviderKubeVirt{
					HostClusterRef: &v1alpha1.LocalObjectReference{Name: "metal-ocp"},
					Namespace:      "bootwright-child-ocp",
				},
			},
		}},
	}
	deps := Deps{StatPath: os.Stat}
	checks := kubeVirtHostClusterChecks(state, []Phase{{Name: "machines"}, {Name: "base"}}, clustersDir, "", deps)
	assertPreflightCheckStatus(t, checks, "metal-ocp kubeconfig", "INFO")
	for _, check := range checks {
		if check.Name == "metal-ocp KubeVirt API" {
			t.Fatalf("KubeVirt API must not be probed for a not-yet-provisioned host: %+v", check)
		}
	}
}

// TestKubeVirtHostClusterExternalStillRequiresKubeconfig pins the other side of
// the gate: a host cluster that is NOT a ContainerCluster this run installs
// (external/pre-existing, or a base-out-of-scope run) must still have its
// kubeconfig on disk, so a missing one stays a FAIL.
func TestKubeVirtHostClusterExternalStillRequiresKubeconfig(t *testing.T) {
	clustersDir := t.TempDir()
	state := v1alpha1.State{InfraProviders: []v1alpha1.InfraProvider{{
		Metadata: v1alpha1.Metadata{Name: "child-provider"},
		Spec: v1alpha1.InfraProviderSpec{
			Type: v1alpha1.ProvisionerKubeVirt,
			KubeVirt: &v1alpha1.InfraProviderKubeVirt{
				HostClusterRef: &v1alpha1.LocalObjectReference{Name: "metal-ocp"},
				Namespace:      "bootwright-child-ocp",
			},
		},
	}}}
	deps := Deps{StatPath: os.Stat}
	checks := kubeVirtHostClusterChecks(state, []Phase{{Name: "machines"}, {Name: "base"}}, clustersDir, "", deps)
	assertPreflightCheckStatus(t, checks, "metal-ocp kubeconfig", "FAIL")
}

// TestKubeVirtHostClusterSelfProvisionedMachinesOnlyStillRequiresKubeconfig pins
// that the deferral is tied to the kubeconfig-producing base phase: a machines-only
// run does not install the host cluster, so its missing kubeconfig stays a FAIL
// even though the host is a ContainerCluster in state.
func TestKubeVirtHostClusterSelfProvisionedMachinesOnlyStillRequiresKubeconfig(t *testing.T) {
	clustersDir := t.TempDir()
	state := v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "metal-ocp"},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "child-provider"},
			Spec: v1alpha1.InfraProviderSpec{
				Type: v1alpha1.ProvisionerKubeVirt,
				KubeVirt: &v1alpha1.InfraProviderKubeVirt{
					HostClusterRef: &v1alpha1.LocalObjectReference{Name: "metal-ocp"},
					Namespace:      "bootwright-child-ocp",
				},
			},
		}},
	}
	deps := Deps{StatPath: os.Stat}
	checks := kubeVirtHostClusterChecks(state, []Phase{{Name: "machines"}}, clustersDir, "", deps)
	assertPreflightCheckStatus(t, checks, "metal-ocp kubeconfig", "FAIL")
}

func TestKubeVirtNetworkRefCheckProbesDerivedNAD(t *testing.T) {
	ref := v1alpha1.KubeVirtNetworkRef{
		Kind:      v1alpha1.KubeVirtNetworkKindCUDN,
		Name:      "child-net",
		Namespace: "bootwright-child-ocp",
	}
	present := kubeVirtNetworkRefCheck("child-net", ref, "/kc", Deps{
		CommandOutputLocalRoot: func(_ string, _ ...string) ([]byte, error) {
			return []byte("networkattachmentdefinition.k8s.cni.cncf.io/child-net\n"), nil
		},
	})
	if present.Status != StatusOK {
		t.Fatalf("present NAD rejected: %+v", present)
	}
	missing := kubeVirtNetworkRefCheck("child-net", ref, "/kc", Deps{
		CommandOutputLocalRoot: func(_ string, _ ...string) ([]byte, error) {
			return []byte("Error from server (NotFound): networkattachmentdefinitions.k8s.cni.cncf.io \"child-net\" not found\n"), errors.New("not found")
		},
	})
	if missing.Status != StatusFail {
		t.Fatalf("missing NAD accepted: %+v", missing)
	}
	if !strings.Contains(missing.Remediation, "ClusterUserDefinedNetwork") {
		t.Fatalf("remediation should name the referenced kind: %q", missing.Remediation)
	}
}

// TestKubeVirtHostClusterChecksRunAsLocalRoot pins the privilege fix: the host
// cluster kubeconfig lives in the 0700 root-owned managed workspace, so the
// probes must run as local root (CommandOutputLocalRoot), not de-escalated to
// the caller (CommandOutput). A deps whose caller-routed CommandOutput fails but
// whose local-root CommandOutputLocalRoot succeeds must still produce OK.
func TestKubeVirtHostClusterChecksRunAsLocalRoot(t *testing.T) {
	callerDenied := func(_ string, _ ...string) ([]byte, error) {
		return []byte(`error loading config file "/var/lib/bootwright/.../kubeconfig": permission denied`), errors.New("exit status 1")
	}

	// The API check now os.Opens the kubeconfig (as local root) before shelling
	// out, so give it a real readable file; the probe must then still resolve
	// through CommandOutputLocalRoot, not the de-escalated CommandOutput.
	kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	api := kubeVirtAPIReadyCheck("metal-ocp", kubeconfig, Deps{
		CommandOutput: callerDenied,
		CommandOutputLocalRoot: func(_ string, _ ...string) ([]byte, error) {
			return []byte("customresourcedefinition.apiextensions.k8s.io/virtualmachines.kubevirt.io\n"), nil
		},
	})
	if api.Status != StatusOK {
		t.Fatalf("KubeVirt API check did not run as local root: %+v", api)
	}

	ref := v1alpha1.KubeVirtNetworkRef{Kind: v1alpha1.KubeVirtNetworkKindCUDN, Name: "child-net", Namespace: "bootwright-child-ocp"}
	net := kubeVirtNetworkRefCheck("child-net", ref, "/kc", Deps{
		CommandOutput: callerDenied,
		CommandOutputLocalRoot: func(_ string, _ ...string) ([]byte, error) {
			return []byte("networkattachmentdefinition.k8s.cni.cncf.io/child-net\n"), nil
		},
	})
	if net.Status != StatusOK {
		t.Fatalf("network ref check did not run as local root: %+v", net)
	}
}

// TestKubeVirtChecksClassifyUnreadableKubeconfig pins the remediation fix: an
// EACCES on the kubeconfig must not be misreported as "KubeVirt not ready" or
// "create the network" — it names the unreadable kubeconfig instead.
func TestKubeVirtChecksClassifyUnreadableKubeconfig(t *testing.T) {
	eacces := func(_ string, _ ...string) ([]byte, error) {
		return []byte(`error loading config file "/var/lib/bootwright/contexts/x/clusters/metal-ocp/secrets/kubeconfig": permission denied`), errors.New("exit status 1")
	}

	api := kubeVirtAPIReadyCheck("metal-ocp", "/var/lib/bootwright/contexts/x/clusters/metal-ocp/secrets/kubeconfig", Deps{CommandOutputLocalRoot: eacces})
	if api.Status != StatusFail {
		t.Fatalf("unreadable kubeconfig accepted: %+v", api)
	}
	if strings.Contains(api.Remediation, "apply --stage clusters") {
		t.Fatalf("EACCES misreported as KubeVirt-not-ready: %q", api.Remediation)
	}
	if !strings.Contains(api.Remediation, "readable, valid kubeconfig") {
		t.Fatalf("remediation should name the unreadable kubeconfig: %q", api.Remediation)
	}

	ref := v1alpha1.KubeVirtNetworkRef{Kind: v1alpha1.KubeVirtNetworkKindCUDN, Name: "child-net", Namespace: "bootwright-child-ocp"}
	net := kubeVirtNetworkRefCheck("child-net", ref, "/var/lib/bootwright/contexts/x/clusters/metal-ocp/secrets/kubeconfig", Deps{CommandOutputLocalRoot: eacces})
	if net.Status != StatusFail {
		t.Fatalf("unreadable kubeconfig accepted: %+v", net)
	}
	if strings.Contains(net.Remediation, "ClusterUserDefinedNetwork") {
		t.Fatalf("EACCES misreported as missing network: %q", net.Remediation)
	}
	if !strings.Contains(net.Remediation, "readable, valid kubeconfig") {
		t.Fatalf("remediation should name the unreadable kubeconfig: %q", net.Remediation)
	}
}

// TestVSpherePyvmomiCheckRunsAsLocalRoot pins that the managed-venv interpreter
// probe runs as local root, since the venv lives in the root-owned workspace.
func TestVSpherePyvmomiCheckRunsAsLocalRoot(t *testing.T) {
	check := vspherePyvmomiCheck(Deps{
		CommandOutput: func(_ string, _ ...string) ([]byte, error) {
			return nil, errors.New("permission denied")
		},
		CommandOutputLocalRoot: func(_ string, _ ...string) ([]byte, error) {
			return []byte(""), nil
		},
	})
	if check.Status != StatusOK {
		t.Fatalf("pyvmomi check did not run as local root: %+v", check)
	}
}

// TestVirtctlPreflightGatedOnDepsProvisioning pins the deps-aware virtctl gate:
// the deps stage provisions a version-matched virtctl from the host cluster, so
// the hard binary check is required only for a base run without deps; a full
// apply (no --stage) or any run that includes deps skips it.
func TestVirtctlPreflightGatedOnDepsProvisioning(t *testing.T) {
	state := v1alpha1.State{
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "kv"},
			Spec: v1alpha1.InfraProviderSpec{
				Type:     v1alpha1.ProvisionerKubeVirt,
				KubeVirt: &v1alpha1.InfraProviderKubeVirt{HostClusterRef: &v1alpha1.LocalObjectReference{Name: "host"}, Namespace: "ns"},
			},
		}},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "m0"},
			Spec:     v1alpha1.MachineSpec{Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "kv"}}},
		}},
	}
	deps := Deps{
		LookPath: func(name string, _ []string) (string, error) {
			if name == "virtctl" {
				return "", errors.New("not found")
			}
			return "/bin/" + name, nil
		},
		StatPath:               func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		CommandOutput:          func(string, ...string) ([]byte, error) { return []byte("Python 3.12.4"), nil },
		CommandOutputLocalRoot: func(string, ...string) ([]byte, error) { return []byte("Python 3.12.4"), nil },
		UID:                    func() int { return 1000 },
	}
	hasVirtctl := func(checks []Check) bool {
		for _, c := range checks {
			if c.Name == "virtctl" {
				return true
			}
		}
		return false
	}
	baseOnly := CollectChecks(state, []Phase{{Name: "base"}}, true, "test", "/context/secrets", "/clusters", deps, nil, nil)
	if !hasVirtctl(baseOnly) {
		t.Fatalf("base-without-deps must require virtctl up front: %+v", baseOnly)
	}
	withDeps := CollectChecks(state, []Phase{{Name: "deps"}, {Name: "base"}}, true, "test", "/context/secrets", "/clusters", deps, nil, nil)
	if hasVirtctl(withDeps) {
		t.Fatalf("deps in scope provisions virtctl, so the hard gate must be skipped")
	}
	fullApply := CollectChecks(state, nil, true, "test", "/context/secrets", "/clusters", deps, nil, nil)
	if hasVirtctl(fullApply) {
		t.Fatalf("a full apply (no --stage) includes deps, so the hard gate must be skipped")
	}
}

func TestSecretRefChecksAcceptContextAndGeneratedMaterial(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	deps := Deps{
		StatPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}
	checks := secretRefChecks(state, "/context/secrets", []Phase{{Name: "base"}}, deps)

	var pullSecret, generatedBMC *Check
	for i := range checks {
		check := &checks[i]
		switch {
		case check.Name == "sno-libvirt pullSecretRef":
			pullSecret = check
		case strings.Contains(check.Name, "bmcEmulationDefaults credentialsRef"):
			generatedBMC = check
		}
	}
	if pullSecret == nil {
		t.Fatalf("missing pull secret check: %+v", checks)
	}
	if !strings.Contains(pullSecret.Evidence, "/context/secrets/openshift-pull-secret missing") {
		t.Fatalf("pull secret check evidence = %q", pullSecret.Evidence)
	}
	if !strings.Contains(pullSecret.Remediation, "bootwright secret set --name openshift-pull-secret --pull-secret") {
		t.Fatalf("pull secret remediation = %q", pullSecret.Remediation)
	}
	if generatedBMC == nil {
		t.Fatalf("missing generated BMC check: %+v", checks)
	}
	if !strings.Contains(generatedBMC.Remediation, "bootwright secret generate or bootwright secret set --name bmc-credentials") {
		t.Fatalf("generated BMC remediation = %q", generatedBMC.Remediation)
	}
}

func TestSecretRefChecksRequireInstallTrustCABundle(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	state.Secrets = append(state.Secrets, v1alpha1.Secret{
		Metadata: v1alpha1.Metadata{Name: "corp-ca"},
		Spec:     v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeCABundle},
	})
	state.Environments[0].Spec.InstallTrust = &v1alpha1.EnvironmentInstallTrustSpec{
		CABundleRefs: []v1alpha1.SecretRef{{Name: "corp-ca"}},
	}
	checks := secretRefChecks(state, "/context/secrets", []Phase{{Name: "base"}}, Deps{
		StatPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	})

	var caCheck *Check
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

func TestSecretRefChecksStatFileSourcesAsCallerOwned(t *testing.T) {
	sourceDir := t.TempDir()
	pullSecret := filepath.Join(sourceDir, "pull-secret.json")
	if err := os.WriteFile(pullSecret, []byte(`{"auths":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			SourcePath: filepath.Join(sourceDir, "environment.yaml"),
			Spec:       v1alpha1.EnvironmentSpec{},
		}},
		Secrets: []v1alpha1.Secret{{
			Metadata:   v1alpha1.Metadata{Name: "openshift-pull-secret"},
			SourcePath: filepath.Join(sourceDir, "environment.yaml"),
			Spec: v1alpha1.SecretSpec{
				Type:   v1alpha1.SecretTypeDockerConfigJSON,
				Source: v1alpha1.SecretSource{File: &v1alpha1.SecretFileSource{Path: pullSecret}},
			},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster"},
			Spec: v1alpha1.ContainerClusterSpec{Install: v1alpha1.OCPInstallSpec{
				PullSecretRef: v1alpha1.SecretRef{Name: "openshift-pull-secret"},
			}},
		}},
	}
	var externalStats int
	checks := secretRefChecks(state, "/context/secrets", []Phase{{Name: "base"}}, Deps{
		StatPath: func(path string) (os.FileInfo, error) {
			t.Fatalf("file source %s was statted through root-managed path", path)
			return nil, os.ErrInvalid
		},
		StatExternalPath: func(path string) (os.FileInfo, error) {
			externalStats++
			return os.Stat(path)
		},
	})
	if externalStats != 1 {
		t.Fatalf("external stat calls = %d, want 1", externalStats)
	}
	assertPreflightCheckStatus(t, checks, "cluster pullSecretRef", "OK")
}

func TestSecretRefChecksRequireImportedCephExternalDetails(t *testing.T) {
	state := importedCephSecretState(v1alpha1.SecretSource{})
	checks := secretRefChecks(state, "/context/secrets", []Phase{{Name: "add-ons"}}, Deps{
		StatPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	})

	var detailsCheck *Check
	for i := range checks {
		check := &checks[i]
		if check.Name == "StorageExport/shared-ceph-data-foundation externalDetails.fromSecretRef" {
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

func TestStoragePreflightChecksManagedCephRuntimeAndRegistrySecret(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Spec: v1alpha1.EnvironmentSpec{},
		}},
		Entitlements: []v1alpha1.Entitlement{{
			Metadata: v1alpha1.Metadata{Name: "rhcs"},
			Spec: v1alpha1.EntitlementSpec{
				Type: v1alpha1.EntitlementTypeRedHatCeph,
				RHSM: &v1alpha1.EntitlementRHSM{
					OrganizationRef:  v1alpha1.SecretRef{Name: "redhat-org"},
					ActivationKeyRef: v1alpha1.SecretRef{Name: "redhat-activation-key"},
				},
				Registry: &v1alpha1.EntitlementRegistry{
					CredentialsRef: v1alpha1.SecretRef{Name: "ceph-registry-credentials"},
				},
			},
		}},
		Secrets: []v1alpha1.Secret{
			{Metadata: v1alpha1.Metadata{Name: "ceph-node-ssh"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeSSHKeyPair, Source: v1alpha1.SecretSource{Generated: &v1alpha1.SecretGeneratedSource{}}}},
			{Metadata: v1alpha1.Metadata{Name: "ceph-known-hosts"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque}},
			{Metadata: v1alpha1.Metadata{Name: "ceph-registry-credentials"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeUsernamePassword}},
			{Metadata: v1alpha1.Metadata{Name: "redhat-org"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque}},
			{Metadata: v1alpha1.Metadata{Name: "redhat-activation-key"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque}},
		},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "ceph-0"},
			Spec: v1alpha1.MachineSpec{
				Capabilities: []string{v1alpha1.MachineCapabilityCephNode},
				OS: v1alpha1.MachineOSSpec{
					Provided: v1alpha1.BoolPtr(true),
				},
				Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "ceph-0.example.test"}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{
						AddressRef:    v1alpha1.LocalObjectReference{Name: "ssh"},
						KeyRef:        v1alpha1.SecretRef{Name: "ceph-node-ssh"},
						KnownHostsRef: v1alpha1.SecretRef{Name: "ceph-known-hosts"},
					},
				},
			},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Distribution:   v1alpha1.StorageCephDistributionRedHat,
					EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhcs"},
					Topology: v1alpha1.StorageCephTopology{
						Hosts: []v1alpha1.StorageCephHost{{
							Hostname:   "ceph-0",
							MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
							Site:       "dc1",
							Roles:      []string{v1alpha1.StorageCephRoleMON},
						}},
					},
				},
			},
		}},
	}
	checks := CollectChecks(state, []Phase{{Name: "base"}}, true, "test", "/context/secrets", "/host-state", Deps{
		LookPath: func(name string, _ []string) (string, error) {
			return "/bin/" + name, nil
		},
		StatPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}, nil, nil)

	for _, name := range []string{"ansible-playbook", "python3", "ssh", "scp"} {
		assertPreflightCheckStatus(t, checks, name, "OK")
	}
	assertPreflightCheckStatus(t, checks, "StorageCluster/ceph ceph entitlementRef registry credentialsRef", "FAIL")
}

// A scoped apply pulls a managed StorageCluster into the plan state only as a
// render reference for a container cluster's data-foundation attachment. Its
// bootstrap secrets and cephadm SSH/scp tooling belong to the storage cluster's
// own lifecycle, so a SecretScope that omits it must drop those checks while the
// in-scope container cluster's secrets stay required.
func TestPreflightSecretScopeDropsRenderReferenceStorage(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Spec: v1alpha1.EnvironmentSpec{},
		}},
		Entitlements: []v1alpha1.Entitlement{{
			Metadata: v1alpha1.Metadata{Name: "rhcs"},
			Spec: v1alpha1.EntitlementSpec{
				Type: v1alpha1.EntitlementTypeRedHatCeph,
				RHSM: &v1alpha1.EntitlementRHSM{
					OrganizationRef:  v1alpha1.SecretRef{Name: "redhat-org"},
					ActivationKeyRef: v1alpha1.SecretRef{Name: "redhat-activation-key"},
				},
				Registry: &v1alpha1.EntitlementRegistry{
					CredentialsRef: v1alpha1.SecretRef{Name: "ceph-registry-credentials"},
				},
			},
		}},
		Secrets: []v1alpha1.Secret{
			{Metadata: v1alpha1.Metadata{Name: "ceph-node-ssh"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeSSHKeyPair, Source: v1alpha1.SecretSource{Generated: &v1alpha1.SecretGeneratedSource{}}}},
			{Metadata: v1alpha1.Metadata{Name: "redhat-org"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque}},
			{Metadata: v1alpha1.Metadata{Name: "redhat-activation-key"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque}},
			{Metadata: v1alpha1.Metadata{Name: "ceph-registry-credentials"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeUsernamePassword}},
		},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "ceph-0"},
			Spec: v1alpha1.MachineSpec{
				Capabilities: []string{v1alpha1.MachineCapabilityCephNode},
				OS:           v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(true)},
				Addresses:    []v1alpha1.MachineAddress{{Name: "ssh", Address: "ceph-0.example.test"}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{
						AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
						KeyRef:     v1alpha1.SecretRef{Name: "ceph-node-ssh"},
					},
				},
			},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "ocp"},
			Spec: v1alpha1.ContainerClusterSpec{Install: v1alpha1.OCPInstallSpec{
				PullSecretRef: v1alpha1.SecretRef{Name: "openshift-pull-secret"},
			}},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Distribution:   v1alpha1.StorageCephDistributionRedHat,
					EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhcs"},
					Topology: v1alpha1.StorageCephTopology{
						Hosts: []v1alpha1.StorageCephHost{{
							Hostname:   "ceph-0",
							MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
							Site:       "dc1",
							Roles:      []string{v1alpha1.StorageCephRoleMON},
						}},
					},
				},
			},
		}},
	}
	deps := Deps{
		LookPath: func(name string, _ []string) (string, error) { return "/bin/" + name, nil },
		StatPath: func(path string) (os.FileInfo, error) { return nil, os.ErrNotExist },
	}
	// Scope to the container cluster only: the Ceph cluster and its node are
	// render references, so neither is a work object.
	scope := &SecretScope{Machines: map[string]bool{}, StorageClusters: map[string]bool{}}
	checks := CollectChecks(state, []Phase{{Name: "base"}}, true, "test", "/context/secrets", "/host-state", deps, nil, scope)

	for _, name := range []string{
		"StorageCluster/ceph ceph entitlementRef rhsm organizationRef",
		"StorageCluster/ceph ceph entitlementRef registry credentialsRef",
		"StorageCluster/ceph node/ceph-0 Machine/ceph-0 keyRef private",
		"machine ceph-0 keyRef",
		"ssh",
		"scp",
	} {
		if checkPresent(checks, name) {
			t.Errorf("render-reference Ceph check %q must be dropped by the secret scope: %+v", name, checkNames(checks))
		}
	}
	// The in-scope container cluster still requires its pull secret.
	assertPreflightCheckStatus(t, checks, "ocp pullSecretRef", "FAIL")
}

func checkPresent(checks []Check, name string) bool {
	for _, check := range checks {
		if check.Name == name {
			return true
		}
	}
	return false
}

func checkNames(checks []Check) []string {
	out := make([]string, 0, len(checks))
	for _, check := range checks {
		out = append(out, check.Name)
	}
	return out
}

func TestPreflightChecksAddonsSSHExecutionNeedsAnsible(t *testing.T) {
	state := importedCephSecretState(v1alpha1.SecretSource{})
	state.Secrets = append(state.Secrets,
		v1alpha1.Secret{Metadata: v1alpha1.Metadata{Name: "ceph-known-hosts"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque}},
		v1alpha1.Secret{Metadata: v1alpha1.Metadata{Name: "ceph-admin-ssh"}, Spec: v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeSSHKeyPair}},
	)
	state.Machines = []v1alpha1.Machine{{
		Metadata: v1alpha1.Metadata{Name: "ceph-admin-01"},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{v1alpha1.MachineCapabilityCephAdmin},
			OS: v1alpha1.MachineOSSpec{
				Provided: v1alpha1.BoolPtr(true),
			},
			Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "ceph-admin-01.example.test"}},
			Access: v1alpha1.MachineAccess{
				SSH: &v1alpha1.MachineSSHSpec{
					AddressRef:    v1alpha1.LocalObjectReference{Name: "ssh"},
					KeyRef:        v1alpha1.SecretRef{Name: "ceph-admin-ssh"},
					KnownHostsRef: v1alpha1.SecretRef{Name: "ceph-known-hosts"},
				},
			},
		},
	}}
	state.StorageExports[0].Spec.ExternalDetails = &v1alpha1.StorageExportExternalDetailsSpec{
		SSHExecution: &v1alpha1.StorageExportExternalDetailsSSHExecution{
			MachineRefs: []v1alpha1.LocalObjectReference{{Name: "ceph-admin-01"}},
			Exporter: v1alpha1.StorageExportExternalDetailsExporter{
				Source: v1alpha1.StorageExportExternalDetailsExporterBoundDataFoundationAddon,
			},
			Config: v1alpha1.StorageExportExternalDetailsExporterConfig{RBDDataPoolName: "rbdpool"},
		},
	}
	checks := CollectChecks(state, []Phase{{Name: "add-ons"}}, true, "test", "/context/secrets", "/host-state", Deps{
		LookPath: func(name string, _ []string) (string, error) {
			return "/bin/" + name, nil
		},
		StatPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}, nil, nil)

	assertPreflightCheckStatus(t, checks, "oc", "OK")
	assertPreflightCheckStatus(t, checks, "ansible-playbook", "OK")
	assertPreflightCheckStatus(t, checks, "python3", "OK")
}

func TestSecretRefChecksRequireGeneratedSSHKeyPairFiles(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	keyName := v1alpha1.ClusterAdminSSHKeyName("sno-libvirt")
	upsertSecret(&state, v1alpha1.Secret{
		Metadata: v1alpha1.Metadata{Name: keyName},
		Spec: v1alpha1.SecretSpec{
			Type:   v1alpha1.SecretTypeSSHKeyPair,
			Source: v1alpha1.SecretSource{Generated: &v1alpha1.SecretGeneratedSource{KeyType: v1alpha1.SSHKeyPairTypeEd25519}},
		},
	})
	checks := secretRefChecks(state, "/context/secrets", []Phase{{Name: "base"}}, Deps{
		StatPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	})

	var privateCheck, publicCheck *Check
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
	if !strings.Contains(privateCheck.Evidence, "/context/secrets/"+keyName+" missing") {
		t.Fatalf("private evidence = %q", privateCheck.Evidence)
	}
	if !strings.Contains(publicCheck.Evidence, "/context/secrets/"+keyName+".pub missing") {
		t.Fatalf("public evidence = %q", publicCheck.Evidence)
	}
}

func importedCephSecretState(source v1alpha1.SecretSource) v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Spec: v1alpha1.EnvironmentSpec{},
		}},
		Secrets: []v1alpha1.Secret{{
			Metadata: v1alpha1.Metadata{Name: "shared-ceph-external-details"},
			Spec:     v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque, Source: source},
		}},
		StorageExports: []v1alpha1.StorageExport{{
			Metadata: v1alpha1.Metadata{Name: "shared-ceph-data-foundation"},
			Spec: v1alpha1.StorageExportSpec{
				Type:              v1alpha1.StorageExportTypeDataFoundation,
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "shared-ceph"},
				ExternalDetails: &v1alpha1.StorageExportExternalDetailsSpec{
					FromSecretRef: v1alpha1.SecretRef{Name: "shared-ceph-external-details"},
				},
			},
		}},
		ClusterAddons: []v1alpha1.ClusterAddon{{
			Metadata: v1alpha1.Metadata{Name: "odf"},
			Spec: v1alpha1.ClusterAddonSpec{
				Type:     v1alpha1.ClusterAddonTypeManifestSet,
				Provides: []string{v1alpha1.ClusterAddonProvidesDataFoundation},
				Accepts:  dataFoundationAccepts(),
			},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
			Metadata: v1alpha1.Metadata{Name: "shared-ceph-binding"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ClusterRef: v1alpha1.LocalObjectReference{Name: "demo"},
				Addons:     []v1alpha1.ClusterAddonBindingAddon{dataFoundationBindingAddon("shared-ceph-data-foundation")},
			},
		}},
	}
}

func dataFoundationAccepts() v1alpha1.ClusterAddonAccepts {
	return v1alpha1.ClusterAddonAccepts{Inputs: []v1alpha1.ClusterAddonAcceptedInput{{
		Name: "external-storage",
		Schema: v1alpha1.ClusterAddonInputSchema{
			Type:     v1alpha1.ClusterAddonInputSchemaTypeObject,
			Required: []string{"exportRef"},
			Properties: map[string]v1alpha1.ClusterAddonInputProperty{
				"exportRef": {RefKind: v1alpha1.KindStorageExport},
			},
		},
		Effects: []v1alpha1.ClusterAddonInputEffect{{
			Type:     v1alpha1.ClusterAddonInputEffectStorageExportAttachment,
			Provider: v1alpha1.ClusterAddonProvidesDataFoundation,
		}},
	}}}
}

func dataFoundationBindingAddon(export string) v1alpha1.ClusterAddonBindingAddon {
	values := map[string]any{
		"exportRef": export,
	}
	return v1alpha1.ClusterAddonBindingAddon{
		AddonRef: v1alpha1.LocalObjectReference{Name: "odf"},
		Inputs: []v1alpha1.ClusterAddonBindingInput{{
			Name:   "external-storage",
			Values: values,
		}},
	}
}

func TestClusterPreflightRequiresProviderHostSSHKeyMaterial(t *testing.T) {
	state := loadFixtureState(t, "005-3nodes-baremetal")
	checks := secretRefChecksWithLocalityPolicy(state, "/context/secrets", []Phase{{Name: "base"}}, Deps{
		StatPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}, locality.Policy{Deps: locality.Deps{
		Hostname: func() (string, error) {
			return "controller", nil
		},
	}}, nil)

	for _, check := range checks {
		if check.Name == "machine bastion keyRef" {
			if check.Status == "OK" {
				t.Fatalf("machine SSH key check unexpectedly passed: %+v", check)
			}
			return
		}
	}
	t.Fatalf("missing provider-host SSH key check for container-cluster phase: %+v", checks)
}

func TestClusterPreflightSkipsLoopbackHostSSHKeyMaterial(t *testing.T) {
	state := loadFixtureState(t, "002-sno-emul-baremetal")
	checks := secretRefChecks(state, "/context/secrets", []Phase{{Name: "base"}}, Deps{
		StatPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	})

	for _, check := range checks {
		if check.Name == "machine services-host keyRef" {
			t.Fatalf("loopback machine SSH key should not be required: %+v", checks)
		}
	}
}

func TestClusterPreflightSkipsControllerHostnameSSHKeyMaterial(t *testing.T) {
	state := loadFixtureState(t, "005-3nodes-baremetal")
	checks := secretRefChecksWithLocalityPolicy(state, "/context/secrets", []Phase{{Name: "base"}}, Deps{
		StatPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}, locality.Policy{Deps: locality.Deps{
		Hostname: func() (string, error) {
			return "bastion", nil
		},
	}}, nil)

	for _, check := range checks {
		if check.Name == "machine bastion keyRef" {
			t.Fatalf("controller-local machine SSH key should not be required: %+v", checks)
		}
	}
}

func TestClusterPreflightRequiresTLSPairMaterial(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	upsertSecret(&state, v1alpha1.Secret{
		Metadata: v1alpha1.Metadata{Name: "api-tls"},
		Spec:     v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeTLSCertificate},
	})
	state.ContainerClusters[0].Spec.Install.ServingCertificates = &v1alpha1.ServingCertificatesSpec{
		APIServer: &v1alpha1.APIServerServingCertificateSpec{
			NamedCertificates: []v1alpha1.APIServerNamedCertificateSpec{{
				Names:     []string{"api.sno-libvirt.bootwright.test"},
				SecretRef: v1alpha1.SecretRef{Name: "api-tls"},
			}},
		},
	}
	checks := secretRefChecks(state, "/context/secrets", []Phase{{Name: "base"}}, Deps{
		StatPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	})

	var certCheck, keyCheck *Check
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
	deps := Deps{
		LookPath: func(name string, _ []string) (string, error) {
			return "/usr/local/bin/" + name, nil
		},
		StatPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
		CommandOutput: func(name string, args ...string) ([]byte, error) {
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
		UID: func() int { return 1000 },
	}
	checks := CollectChecks(loadFixtureState(t, "001-sno-libvirt"), []Phase{{Name: "base"}}, true, "test", "/context/secrets", "/host-state", deps, nil, nil)

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

	got, err := DefaultLookPath("openshift-install", []string{extraDir})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("defaultLookPath = %q, want %q", got, want)
	}
}

func assertPreflightCheckStatus(t *testing.T, checks []Check, name, status string) {
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

// TestSecretFileCheckAllowsOwnerOnlyModes pins that a hardened read-only secret
// (0400) passes while any group/other access (0640, 0604) still fails: the check
// guards against over-broad permissions, not tighter-than-0600 ones.
func TestSecretFileCheckAllowsOwnerOnlyModes(t *testing.T) {
	deps := Deps{StatPath: os.Stat}
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, tc := range []struct {
		mode os.FileMode
		want Status
	}{
		{0o600, StatusOK},
		{0o400, StatusOK},
		{0o640, StatusFail},
		{0o604, StatusFail},
	} {
		if err := os.Chmod(path, tc.mode); err != nil {
			t.Fatalf("chmod %04o: %v", tc.mode, err)
		}
		if got := secretFileCheck("ref", path, "credentialsRef", false, false, false, deps).Status; got != tc.want {
			t.Fatalf("secret file mode %04o = %s, want %s", tc.mode, got, tc.want)
		}
	}
}

// TestSecretsDirCheckAllowsOwnerOnlyModes pins the same relaxation for the
// secrets directory: 0500 passes, group/other bits fail.
func TestSecretsDirCheckAllowsOwnerOnlyModes(t *testing.T) {
	deps := Deps{StatPath: os.Stat}
	for _, tc := range []struct {
		mode os.FileMode
		want Status
	}{
		{0o700, StatusOK},
		{0o500, StatusOK},
		{0o750, StatusFail},
		{0o705, StatusFail},
	} {
		dir := t.TempDir()
		if err := os.Chmod(dir, tc.mode); err != nil {
			t.Fatalf("chmod %04o: %v", tc.mode, err)
		}
		if got := secretsDirCheck(dir, deps).Status; got != tc.want {
			t.Fatalf("secrets dir mode %04o = %s, want %s", tc.mode, got, tc.want)
		}
		_ = os.Chmod(dir, 0o700) // restore so t.TempDir cleanup can remove it
	}
}

// TestAddonsStageGatesMissingKubeconfig pins that a stage-scoped add-ons run
// (base out of scope, so no cluster install produces the kubeconfig) gates the
// per-cluster kubeconfig up front, while a run that also installs the cluster
// (base in scope) does not — the kubeconfig is produced mid-run.
func TestAddonsStageGatesMissingKubeconfig(t *testing.T) {
	state := v1alpha1.State{ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
		Metadata: v1alpha1.Metadata{Name: "b1"},
		Spec:     v1alpha1.ClusterAddonBindingSpec{ClusterRef: v1alpha1.LocalObjectReference{Name: "dc1"}},
	}}}
	deps := Deps{
		LookPath: func(name string, _ []string) (string, error) { return "/bin/" + name, nil },
		StatPath: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		UID:      func() int { return 0 },
	}
	checks := CollectChecks(state, []Phase{{Name: "add-ons"}}, true, "test", "/context/secrets", "/host-state", deps, nil, nil)
	assertPreflightCheckStatus(t, checks, "dc1 kubeconfig", "FAIL")

	checks = CollectChecks(state, []Phase{{Name: "add-ons"}, {Name: "base"}}, true, "test", "/context/secrets", "/host-state", deps, nil, nil)
	if _, ok := findCheckByName(checks, "dc1 kubeconfig"); ok {
		t.Fatalf("a base-in-scope run must not gate the add-on kubeconfig: %+v", checks)
	}
}

// TestKubeVirtKubeconfigRefProbesAPI pins that a provider pointing at an
// external hub via kubeconfigRef (its kubeconfig already on disk) gets the live
// KubeVirt API/CRD and networkRef probes, not just a secret-file existence check.
func TestKubeVirtKubeconfigRefProbesAPI(t *testing.T) {
	sourceDir := t.TempDir()
	kubeconfig := filepath.Join(sourceDir, "hub-kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata:   v1alpha1.Metadata{Name: "env"},
			SourcePath: filepath.Join(sourceDir, "environment.yaml"),
			Spec:       v1alpha1.EnvironmentSpec{},
		}},
		Secrets: []v1alpha1.Secret{{
			Metadata:   v1alpha1.Metadata{Name: "hub-kubeconfig"},
			SourcePath: filepath.Join(sourceDir, "environment.yaml"),
			Spec: v1alpha1.SecretSpec{
				Type:   v1alpha1.SecretTypeOpaque,
				Source: v1alpha1.SecretSource{File: &v1alpha1.SecretFileSource{Path: "hub-kubeconfig"}},
			},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "ext-kv"},
			Spec: v1alpha1.InfraProviderSpec{
				Type: v1alpha1.ProvisionerKubeVirt,
				KubeVirt: &v1alpha1.InfraProviderKubeVirt{
					KubeconfigRef: &v1alpha1.SecretRef{Name: "hub-kubeconfig"},
					Namespace:     "child",
				},
				NetworkAttachments: []v1alpha1.NetworkAttachmentCapability{{
					Name: "vmnet",
					KubeVirt: &v1alpha1.NetworkAttachmentKubeVirt{
						NetworkRef: v1alpha1.KubeVirtNetworkRef{
							Kind: v1alpha1.KubeVirtNetworkKindNAD, Name: "vmnet", Namespace: "child",
						},
					},
				}},
			},
		}},
	}
	var probedCRD, probedNAD bool
	deps := Deps{
		StatPath:         os.Stat,
		StatExternalPath: os.Stat,
		CommandOutputLocalRoot: func(name string, args ...string) ([]byte, error) {
			for _, a := range args {
				if a == "crd" {
					probedCRD = true
					return []byte("virtualmachines.kubevirt.io\n"), nil
				}
			}
			probedNAD = true
			return []byte("networkattachmentdefinition.k8s.cni.cncf.io/vmnet\n"), nil
		},
	}
	checks := kubeVirtHostClusterChecks(state, []Phase{{Name: "machines"}}, "/clusters", sourceDir, deps)
	assertPreflightCheckStatus(t, checks, "hub-kubeconfig KubeVirt API", "OK")
	assertPreflightCheckStatus(t, checks, "vmnet network", "OK")
	if !probedCRD || !probedNAD {
		t.Fatalf("kubeconfigRef arm did not run both probes: crd=%v nad=%v", probedCRD, probedNAD)
	}
}

// upsertSecret replaces the like-named first-class Secret in state, or appends
// it when absent, mirroring the overwrite semantics of the former
// Environment.spec.secrets map.
func upsertSecret(state *v1alpha1.State, secret v1alpha1.Secret) {
	for i := range state.Secrets {
		if state.Secrets[i].Metadata.Name == secret.Metadata.Name {
			state.Secrets[i] = secret
			return
		}
	}
	state.Secrets = append(state.Secrets, secret)
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
