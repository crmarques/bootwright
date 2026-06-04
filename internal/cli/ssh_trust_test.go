package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	contextstore "github.com/crmarques/bootwright/internal/runtime/context"
	"github.com/crmarques/bootwright/internal/runtime/sshtrust"
)

const (
	hostTrustKeyA = "QUJDRA=="
	hostTrustKeyB = "RkdoSQ=="
)

func TestHostTrustAddsAndReusesManagedHost(t *testing.T) {
	ctx := initHostTrustTestContext(t)
	setHostTrustTestDeps(t, map[string]string{"provider-01.example.test": hostTrustKeyA})

	stdout, stderr, code := runCLI(t, "host", "trust", "--yes")
	if code != 0 {
		t.Fatalf("host trust exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "SHA256:") {
		t.Fatalf("host trust output missing fingerprint:\n%s", stdout)
	}
	if got := readTestFile(t, sshtrust.KnownHostsPathForContext(ctx.BaseDir)); !strings.Contains(got, "provider-01.example.test ssh-ed25519 "+hostTrustKeyA) {
		t.Fatalf("known_hosts = %q", got)
	}

	stdout, stderr, code = runCLI(t, "host", "trust", "--dry-run", "--output", "json")
	if code != 0 {
		t.Fatalf("host trust dry-run json exited %d, stderr=%q", code, stderr)
	}
	var report hostTrustReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, stdout)
	}
	if len(report.Hosts) != 1 || report.Hosts[0].Action != "reuse" {
		t.Fatalf("report hosts = %+v", report.Hosts)
	}
}

func TestHostTrustRequiresReplaceForChangedKey(t *testing.T) {
	ctx := initHostTrustTestContext(t)
	setHostTrustTestDeps(t, map[string]string{"provider-01.example.test": hostTrustKeyA})
	if _, stderr, code := runCLI(t, "host", "trust", "--yes"); code != 0 {
		t.Fatalf("initial host trust failed: %s", stderr)
	}

	setHostTrustTestDeps(t, map[string]string{"provider-01.example.test": hostTrustKeyB})
	_, stderr, code := runCLI(t, "host", "trust", "--yes")
	if code == 0 {
		t.Fatal("host trust accepted changed key without --replace")
	}
	if !strings.Contains(stderr, "SSH trust changed") {
		t.Fatalf("stderr missing changed trust message: %q", stderr)
	}

	stdout, stderr, code := runCLI(t, "host", "trust", "--replace", "provider-01", "--yes")
	if code != 0 {
		t.Fatalf("host trust replace exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	store, err := sshtrust.Load(sshtrust.StorePathForContext(ctx.BaseDir))
	if err != nil {
		t.Fatalf("load trust store: %v", err)
	}
	record, ok := store.Find("provider-01")
	if !ok {
		t.Fatal("trust store missing provider-01")
	}
	if record.PublicKey != hostTrustKeyB {
		t.Fatalf("public key = %q, want %q", record.PublicKey, hostTrustKeyB)
	}
}

func TestHostTrustFiltersSelectedHosts(t *testing.T) {
	initHostTrustTestContext(t)
	setHostTrustTestDeps(t, map[string]string{"provider-01.example.test": hostTrustKeyA})

	stdout, stderr, code := runCLI(t, "host", "trust", "--hosts", "provider-01", "--dry-run", "--output", "json")
	if code != 0 {
		t.Fatalf("host trust selected dry-run exited %d, stderr=%q", code, stderr)
	}
	var report hostTrustReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, stdout)
	}
	if len(report.Hosts) != 1 || report.Hosts[0].Name != "provider-01" || report.Hosts[0].Action != "add" {
		t.Fatalf("report hosts = %+v", report.Hosts)
	}
}

func TestHostTrustPreflightFailsWhenManagedTrustMissing(t *testing.T) {
	state := hostTrustTestState()
	checks := hostTrustChecks(state, "/context/secrets", []Phase{{Name: "machine-infra"}}, preflightDeps{
		lookPath: func(name string, _ []string) (string, error) {
			if name == "ssh-keyscan" {
				return "/usr/bin/ssh-keyscan", nil
			}
			return "/usr/bin/" + name, nil
		},
		statPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	})
	if !hasCheck(checks, checkGroupHostTrust, "Machine/provider-01", "bootwright host trust") {
		t.Fatalf("checks missing host trust failure: %+v", checks)
	}
}

func initHostTrustTestContext(t *testing.T) contextstore.Context {
	t.Helper()
	setTestHomeAndRoot(t)
	inputDir := t.TempDir()
	writeHostTrustTestInput(t, inputDir)
	stdout, stderr, code := runCLI(t, "context", "init", "lab", "-f", inputDir)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := contextstore.RequireExistingContext("lab")
	if err != nil {
		t.Fatalf("RequireExistingContext: %v", err)
	}
	return ctx
}

func writeHostTrustTestInput(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"environment.yaml": `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: lab
spec:
  baseDomain: example.test
  secrets:
    - bastion-host-ssh:
        file: ~/.ssh/bootwright-ssh-key
`,
		"service-machine.yaml": `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: provider-01
spec:
  capabilities:
    - libvirt
  os:
    provided: true
  addresses:
    - name: ssh
      address: provider-01.example.test
  access:
    ssh:
      addressRef:
        name: ssh
      user: core
      keyRef:
        name: bastion-host-ssh
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func hostTrustTestState() v1alpha1.State {
	return v1alpha1.State{Machines: []v1alpha1.Machine{{
		Metadata: v1alpha1.Metadata{Name: "provider-01"},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{v1alpha1.MachineCapabilityLibvirt},
			OS: v1alpha1.MachineOSSpec{
				Provided: v1alpha1.BoolPtr(true),
			},
			Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "provider-01.example.test"}},
			Access: v1alpha1.MachineAccess{
				SSH: &v1alpha1.MachineSSHSpec{
					AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
					KeyRef:     v1alpha1.SecretRef{Name: "bastion-host-ssh"},
				},
			},
		},
	}}}
}

func setHostTrustTestDeps(t *testing.T, keys map[string]string) {
	t.Helper()
	previous := defaultHostTrustDeps
	defaultHostTrustDeps = hostTrustDeps{
		lookPath: func(name string, _ []string) (string, error) {
			if name != "ssh-keyscan" {
				return "", errors.New("unexpected lookup " + name)
			}
			return "/usr/bin/ssh-keyscan", nil
		},
		scan: func(_ context.Context, address string, _ time.Duration) ([]sshtrust.ScannedKey, error) {
			publicKey, ok := keys[address]
			if !ok {
				return nil, errors.New("unexpected scan " + address)
			}
			fingerprint, err := sshtrust.FingerprintSHA256(publicKey)
			if err != nil {
				return nil, err
			}
			return []sshtrust.ScannedKey{{
				Address:           address,
				KeyType:           "ssh-ed25519",
				PublicKey:         publicKey,
				FingerprintSHA256: fingerprint,
				KnownHostsLine:    sshtrust.KnownHostsLine(address, "ssh-ed25519", publicKey),
			}}, nil
		},
	}
	t.Cleanup(func() { defaultHostTrustDeps = previous })
}

func hasCheck(checks []preflightCheck, group, name, remediation string) bool {
	for _, check := range checks {
		if check.Group == group && check.Name == name && strings.Contains(check.Remediation, remediation) {
			return true
		}
	}
	return false
}
