package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if check.Evidence != "python3 3.12" {
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
	checks := collectBastionChecks(loadFixtureState(t, "001-sno-libvirt"), "/host-state", deps)
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
	checks := collectPreflightChecks(loadFixtureState(t, "001-sno-libvirt"), []Phase{{Name: "clusters"}}, true, "/context/secrets", "/host-state", deps)
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

func TestSecretRefChecksAcceptContextAndGeneratedMaterial(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	deps := preflightDeps{
		statPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}
	checks := secretRefChecks(state, "/context/secrets", []Phase{{Name: "clusters"}}, deps)

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

func TestClusterPreflightRequiresProviderHostSSHKeyMaterial(t *testing.T) {
	state := loadFixtureState(t, "005-3nodes-baremetal")
	checks := secretRefChecks(state, "/context/secrets", []Phase{{Name: "clusters"}}, preflightDeps{
		statPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	})

	for _, check := range checks {
		if check.Name == "host bastion sshKeyRef" {
			if check.Status == "OK" {
				t.Fatalf("host SSH key check unexpectedly passed: %+v", check)
			}
			return
		}
	}
	t.Fatalf("missing provider-host SSH key check for clusters phase: %+v", checks)
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
	checks := collectPreflightChecks(loadFixtureState(t, "001-sno-libvirt"), []Phase{{Name: "clusters"}}, true, "/context/secrets", "/host-state", deps)

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

func TestClusterCheckLimitIncludesBootHosts(t *testing.T) {
	limit := ansibleLimitForScope("cluster")
	for _, want := range []string{"bootwright_ocp_hosts", "bootwright_boot_hosts"} {
		if !strings.Contains(limit, want) {
			t.Fatalf("cluster limit %q missing %q", limit, want)
		}
	}
}

func findPreflightCheck(t *testing.T, checks []preflightCheck, name string) preflightCheck {
	t.Helper()
	for _, check := range checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("missing preflight check %q: %+v", name, checks)
	return preflightCheck{}
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
