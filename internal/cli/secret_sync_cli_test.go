package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretShowPublicPartAndDeleteGeneratedSSHKeyPair(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")

	stdout, stderr, code := runCLI(t, "secret", "sync")
	if code != 1 {
		t.Fatalf("secret sync exited %d (want 1 while openshift-pull-secret is unset), stderr=%q", code, stderr)
	}
	for _, want := range []string{"request(s) handled", "Still missing", "openshift-pull-secret"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("secret sync stdout missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "OPENSSH PRIVATE KEY") {
		t.Fatalf("secret sync leaked private key:\n%s", stdout)
	}
	public, stderr, code := runCLI(t, "secret", "show", "--name", "sno-libvirt-cluster-admin-ssh-key", "--part", "public")
	if code != 0 {
		t.Fatalf("secret show public exited %d, stderr=%q", code, stderr)
	}
	if !strings.HasPrefix(public, "ssh-ed25519 ") || !strings.Contains(public, " bootwright-sno-libvirt-cluster-admin\n") {
		t.Fatalf("public part = %q", public)
	}
	_, stderr, code = runCLI(t, "secret", "delete", "sno-libvirt-cluster-admin-ssh-key", "--yes")
	if code != 0 {
		t.Fatalf("secret delete exited %d, stderr=%q", code, stderr)
	}
	for _, path := range []string{
		filepath.Join(ctx.SecretsDir, "sno-libvirt-cluster-admin-ssh-key"),
		filepath.Join(ctx.SecretsDir, "sno-libvirt-cluster-admin-ssh-key.pub"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after delete: %v", path, err)
		}
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
