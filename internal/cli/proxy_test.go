package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/runtime/secrets"
)

func TestProxyCredentialAuthorityUsesUserinfoEscaping(t *testing.T) {
	got := proxyCredentialAuthority(secret.UserPassword{
		Username: "u@x",
		Password: "p$:s/q?",
	})
	want := "u%40x:p$%3As%2Fq%3F@"
	if got != want {
		t.Fatalf("proxyCredentialAuthority = %q, want %q", got, want)
	}
}

func TestInjectProxyAuthorityPreservesDollarInReplacement(t *testing.T) {
	got := injectProxyAuthority("http://proxy.example.test:3128", "user:p$ecret@")
	want := "http://user:p$ecret@proxy.example.test:3128"
	if got != want {
		t.Fatalf("injectProxyAuthority = %q, want %q", got, want)
	}
}

func TestAmbientProxyEnvCapturesNonEmptyProxyVars(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("http_proxy", "http://lower-proxy:3128")
	t.Setenv("NO_PROXY", "localhost,127.0.0.1")

	got := ambientProxyEnv()
	if got["http_proxy"] != "http://lower-proxy:3128" {
		t.Fatalf("http_proxy = %q", got["http_proxy"])
	}
	if got["NO_PROXY"] != "localhost,127.0.0.1" {
		t.Fatalf("NO_PROXY = %q", got["NO_PROXY"])
	}
	if _, ok := got["HTTP_PROXY"]; ok {
		t.Fatalf("empty HTTP_PROXY should be ignored: %v", got)
	}
}

func TestAmbientProxyEnvReturnsNilWhenUnset(t *testing.T) {
	clearProxyEnv(t)

	if got := ambientProxyEnv(); got != nil {
		t.Fatalf("ambientProxyEnv = %v, want nil", got)
	}
}

func TestProxySummaryUsesLowercaseFallbacks(t *testing.T) {
	got := proxySummary(map[string]string{
		"https_proxy": "http://user:pass@proxy.example.test:3128",
		"no_proxy":    "localhost,127.0.0.1",
	})

	for _, want := range []string{"configured", "https", "no_proxy: 2 entries"} {
		if !strings.Contains(got, want) {
			t.Fatalf("proxySummary = %q, missing %q", got, want)
		}
	}
}

func TestResolveProxyEnvHonorsProxyForNone(t *testing.T) {
	state := v1alpha1.State{Environments: []v1alpha1.Environment{{
		Spec: v1alpha1.EnvironmentSpec{
			ProxyFor: v1alpha1.EnvironmentProxyForSpec{Bootwright: v1alpha1.EnvironmentComponentNone},
			InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
				Proxies: []v1alpha1.EnvironmentProxyComponent{{
					Name: "default",
					Type: v1alpha1.EnvironmentComponentExternal,
					Connection: &v1alpha1.EnvironmentProxyConnection{
						HTTPProxy: "http://proxy.example.test:3128",
					},
				}},
			},
		},
	}}}

	got, err := resolveProxyEnv(state, t.TempDir())
	if err != nil {
		t.Fatalf("resolveProxyEnv: %v", err)
	}
	if got != nil {
		t.Fatalf("resolveProxyEnv got %v, want nil", got)
	}
}

func TestBastionApplyDryRunShowsContextProxy(t *testing.T) {
	clearProxyEnv(t)
	initTestContext(t, "002-sno-emul-baremetal")
	_, stderr, code := runCLI(t, "secret", "set", "proxy-credentials", "--username", "proxy", "--password", "secret")
	if code != 0 {
		t.Fatalf("secret set exited %d, stderr=%q", code, stderr)
	}

	stdout, stderr, code := runCLI(t, "apply", "bastion", "--dry-run")
	if code != 0 {
		t.Fatalf("apply bastion --dry-run exited %d, stderr=%q", code, stderr)
	}
	want := "proxy: configured (https, http,"
	if !strings.Contains(stdout, want) {
		t.Fatalf("stdout missing %q:\n%s", want, stdout)
	}
}

func TestBastionApplyDryRunShowsNoProxyWhenUnset(t *testing.T) {
	clearProxyEnv(t)
	initTestContext(t, "001-sno-libvirt")

	stdout, stderr, code := runCLI(t, "apply", "bastion", "--dry-run")
	if code != 0 {
		t.Fatalf("apply bastion --dry-run exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "proxy: none\n") {
		t.Fatalf("stdout does not show no-proxy status:\n%s", stdout)
	}
}

func TestBastionApplyDryRunShowsHighLevelActions(t *testing.T) {
	clearProxyEnv(t)
	initTestContext(t, "001-sno-libvirt")

	stdout, stderr, code := runCLI(t, "apply", "bastion", "--dry-run")
	if code != 0 {
		t.Fatalf("apply bastion --dry-run exited %d, stderr=%q", code, stderr)
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

func TestBastionApplyDryRunShowsLocalSudoAuth(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv(localRootSudoAuthEnv, localSudoAuthNonInteractive)
	initTestContext(t, "001-sno-libvirt")

	stdout, stderr, code := runCLI(t, "apply", "bastion", "--dry-run")
	if code != 0 {
		t.Fatalf("apply bastion --dry-run exited %d, stderr=%q", code, stderr)
	}
	want := "sudo: ready (non-interactive)"
	if !strings.Contains(stdout, want) {
		t.Fatalf("stdout missing %q:\n%s", want, stdout)
	}
}

func clearProxyEnv(t *testing.T) {
	t.Helper()
	for _, key := range proxyEnvKeys {
		t.Setenv(key, "")
	}
}
