package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/infra/proxy"
)

func TestBastionSetupDryRunShowsContextProxy(t *testing.T) {
	clearProxyEnv(t)
	initTestContext(t, "002-sno-emul-baremetal")
	_, stderr, code := runCLIWithInput(t, "secret\n", "secret", "set", "--name", "proxy-credentials", "--username", "proxy", "--password-stdin")
	if code != 0 {
		t.Fatalf("secret set exited %d, stderr=%q", code, stderr)
	}

	stdout, stderr, code := runCLI(t, "bastion", "setup", "--dry-run")
	if code != 0 {
		t.Fatalf("bastion setup --dry-run exited %d, stderr=%q", code, stderr)
	}
	want := "proxy: configured (https, http,"
	if !strings.Contains(stdout, want) {
		t.Fatalf("stdout missing %q:\n%s", want, stdout)
	}
}

func TestBastionSetupDryRunShowsNoProxyWhenUnset(t *testing.T) {
	clearProxyEnv(t)
	initTestContext(t, "001-sno-libvirt")

	stdout, stderr, code := runCLI(t, "bastion", "setup", "--dry-run")
	if code != 0 {
		t.Fatalf("bastion setup --dry-run exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "proxy: none\n") {
		t.Fatalf("stdout does not show no-proxy status:\n%s", stdout)
	}
}

func TestBastionSetupDryRunShowsHighLevelActions(t *testing.T) {
	clearProxyEnv(t)
	initTestContext(t, "001-sno-libvirt")

	stdout, stderr, code := runCLI(t, "bastion", "setup", "--dry-run")
	if code != 0 {
		t.Fatalf("bastion setup --dry-run exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"runtime: managed Ansible environment",
		"Actions",
		"Ansible runtime",
		"OpenShift CLIs",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
	for _, reject := range []string{"Bootwright prerequisites", "$ ", "ansible-playbook"} {
		if strings.Contains(stdout, reject) {
			t.Fatalf("stdout should not contain %q:\n%s", reject, stdout)
		}
	}
}

func TestBastionSetupDryRunShowsLocalSudoAuth(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv(localRootSudoAuthEnv, localSudoAuthNonInteractive)
	initTestContext(t, "001-sno-libvirt")

	stdout, stderr, code := runCLI(t, "bastion", "setup", "--dry-run")
	if code != 0 {
		t.Fatalf("bastion setup --dry-run exited %d, stderr=%q", code, stderr)
	}
	want := "sudo: ready (non-interactive)"
	if !strings.Contains(stdout, want) {
		t.Fatalf("stdout missing %q:\n%s", want, stdout)
	}
}

func clearProxyEnv(t *testing.T) {
	t.Helper()
	for _, key := range proxy.EnvKeys {
		t.Setenv(key, "")
	}
}
