package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretShowPublicPartAndDeleteGeneratedSSHKeyPair(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")

	stdout, stderr, code := runCLI(t, "secret", "generate")
	if code != 0 {
		t.Fatalf("secret generate exited %d (want 0; missing context secrets do not fail generate), stderr=%q", code, stderr)
	}
	for _, want := range []string{"request(s) handled", "Needs secret set", "openshift-pull-secret"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("secret generate stdout missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "OPENSSH PRIVATE KEY") {
		t.Fatalf("secret generate leaked private key:\n%s", stdout)
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

// TestSecretCheckGatesOnMissingSecrets verifies `secret check` fails while a
// declared context secret is unset and passes once it is provided.
func TestSecretCheckGatesOnMissingSecrets(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")

	stdout, stderr, code := runCLI(t, "secret", "check")
	if code != 1 {
		t.Fatalf("secret check exited %d (want 1 while openshift-pull-secret is unset), stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "openshift-pull-secret") || !strings.Contains(stdout, "missing") {
		t.Fatalf("secret check stdout missing gap report:\n%s", stdout)
	}

	if _, stderr, code = runCLI(t, "secret", "generate"); code != 0 {
		t.Fatalf("secret generate exited %d, stderr=%q", code, stderr)
	}
	// 001-sno-libvirt also declares a file:-sourced bastion SSH key; provide it
	// alongside the pull secret so check reports a complete set.
	sshDir := filepath.Join(os.Getenv("HOME"), ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "bootwright-ssh-key"), []byte("FAKE PRIVATE KEY FOR TESTS\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "bootwright-ssh-key.pub"), []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForTests\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pullSecret := filepath.Join(t.TempDir(), "pull-secret.json")
	if err := os.WriteFile(pullSecret, []byte(`{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code = runCLI(t, "secret", "set", "openshift-pull-secret", "--pull-secret", pullSecret); code != 0 {
		t.Fatalf("secret set exited %d, stderr=%q", code, stderr)
	}
	stdout, stderr, code = runCLI(t, "secret", "check")
	if code != 0 {
		t.Fatalf("secret check exited %d after providing all secrets, stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "all") || !strings.Contains(stdout, "present") {
		t.Fatalf("secret check stdout missing success summary:\n%s", stdout)
	}
}

// TestSecretGenerateRenewReplacesGeneratedMaterial verifies `--renew` rewrites
// existing generated material instead of reusing it.
func TestSecretGenerateRenewReplacesGeneratedMaterial(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	pubPath := filepath.Join(ctx.SecretsDir, "sno-libvirt-cluster-admin-ssh-key.pub")

	if _, stderr, code := runCLI(t, "secret", "generate"); code != 0 {
		t.Fatalf("secret generate exited %d, stderr=%q", code, stderr)
	}
	first := readTestFile(t, pubPath)

	out, stderr, code := runCLI(t, "secret", "generate")
	if code != 0 {
		t.Fatalf("secret generate (rerun) exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(out, "reused existing SSH key pair") {
		t.Fatalf("second generate should reuse existing material:\n%s", out)
	}
	if readTestFile(t, pubPath) != first {
		t.Fatalf("public key changed without --renew")
	}

	out, stderr, code = runCLI(t, "secret", "generate", "--renew")
	if code != 0 {
		t.Fatalf("secret generate --renew exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(out, "regenerated") {
		t.Fatalf("--renew should report regenerated material:\n%s", out)
	}
	if readTestFile(t, pubPath) == first {
		t.Fatalf("public key did not change after --renew")
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
