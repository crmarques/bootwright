package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestMaterializeGeneratedSSHKeyPair(t *testing.T) {
	secretsDir := t.TempDir()
	state := v1alpha1.State{Environments: []v1alpha1.Environment{{
		Metadata: v1alpha1.Metadata{Name: "env"},
		Spec: v1alpha1.EnvironmentSpec{
			Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
				"cluster-admin-pub-key": {
					Generated: &v1alpha1.EnvironmentSecretGenerated{
						SSHKeyPair: &v1alpha1.GeneratedSSHKeyPairSpec{
							Type:    v1alpha1.SSHKeyPairTypeEd25519,
							Comment: "bootwright-cluster-admin",
						},
					},
				},
			},
		},
	}}}

	results, err := materializeSecrets(secretsDir, state, secretMaterializeOptions{Generated: true})
	if err != nil {
		t.Fatalf("materializeSecrets: %v", err)
	}
	if len(results) != 1 || results[0].name != "cluster-admin-pub-key" {
		t.Fatalf("results = %+v", results)
	}
	privatePath := filepath.Join(secretsDir, "cluster-admin-pub-key")
	publicPath := privatePath + ".pub"
	privateBody := readTestFile(t, privatePath)
	publicBody := readTestFile(t, publicPath)
	if !strings.Contains(privateBody, "OPENSSH PRIVATE KEY") {
		t.Fatalf("private key missing OpenSSH header:\n%s", privateBody)
	}
	if !strings.HasPrefix(publicBody, "ssh-ed25519 ") || !strings.Contains(publicBody, " bootwright-cluster-admin\n") {
		t.Fatalf("public key = %q", publicBody)
	}
	assertTestFileMode(t, privatePath, 0o600)
	assertTestFileMode(t, publicPath, 0o600)

	results, err = materializeSecrets(secretsDir, state, secretMaterializeOptions{Generated: true})
	if err != nil {
		t.Fatalf("materializeSecrets second run: %v", err)
	}
	if len(results) != 1 || !strings.Contains(results[0].action, "reused existing SSH key pair") {
		t.Fatalf("second results = %+v", results)
	}
	if got := readTestFile(t, privatePath); got != privateBody {
		t.Fatal("second materialize rewrote private key")
	}
}

func TestMaterializeCopiesSSHFileSourcesInContextMode(t *testing.T) {
	sourceDir := t.TempDir()
	privateSource := filepath.Join(sourceDir, "id_ed25519")
	publicSource := privateSource + ".pub"
	if err := os.WriteFile(privateSource, []byte("PRIVATE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicSource, []byte("ssh-ed25519 AAAA test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	secretsDir := t.TempDir()
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata:   v1alpha1.Metadata{Name: "env"},
			SourcePath: filepath.Join(sourceDir, "environment.yaml"),
			Spec: v1alpha1.EnvironmentSpec{
				SecretStorage: v1alpha1.EnvironmentSecretStorageSpec{Mode: v1alpha1.SecretStorageModeContext},
				Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
					"cluster-admin-pub-key": {File: privateSource},
				},
			},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster"},
			Spec: v1alpha1.ContainerClusterSpec{Install: v1alpha1.OCPInstallSpec{
				SSHKeyRef: v1alpha1.SecretRef{Name: "cluster-admin-pub-key"},
			}},
		}},
	}

	results, err := materializeSecrets(secretsDir, state, secretMaterializeOptions{FileSources: true})
	if err != nil {
		t.Fatalf("materializeSecrets: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want two SSH key copies", results)
	}
	if got := readTestFile(t, filepath.Join(secretsDir, "cluster-admin-pub-key")); got != "PRIVATE\n" {
		t.Fatalf("private copy = %q", got)
	}
	if got := readTestFile(t, filepath.Join(secretsDir, "cluster-admin-pub-key.pub")); got != "ssh-ed25519 AAAA test\n" {
		t.Fatalf("public copy = %q", got)
	}
	assertTestFileMode(t, filepath.Join(secretsDir, "cluster-admin-pub-key"), 0o600)
	assertTestFileMode(t, filepath.Join(secretsDir, "cluster-admin-pub-key.pub"), 0o600)
}

func TestSecretShowPublicPartAndDeleteGeneratedSSHKeyPair(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")

	stdout, stderr, code := runCLI(t, "secret", "generate")
	if code != 0 {
		t.Fatalf("secret generate exited %d, stderr=%q", code, stderr)
	}
	if strings.Contains(stdout, "OPENSSH PRIVATE KEY") {
		t.Fatalf("secret generate leaked private key:\n%s", stdout)
	}
	public, stderr, code := runCLI(t, "secret", "show", "--name", "cluster-admin-pub-key", "--part", "public")
	if code != 0 {
		t.Fatalf("secret show public exited %d, stderr=%q", code, stderr)
	}
	if !strings.HasPrefix(public, "ssh-ed25519 ") || !strings.Contains(public, " bootwright-cluster-admin\n") {
		t.Fatalf("public part = %q", public)
	}
	_, stderr, code = runCLI(t, "secret", "delete", "cluster-admin-pub-key", "--yes")
	if code != 0 {
		t.Fatalf("secret delete exited %d, stderr=%q", code, stderr)
	}
	for _, path := range []string{
		filepath.Join(ctx.SecretsDir, "cluster-admin-pub-key"),
		filepath.Join(ctx.SecretsDir, "cluster-admin-pub-key.pub"),
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

func assertTestFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}
