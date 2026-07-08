package secret

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestMaterializeGeneratedSSHKeyPair(t *testing.T) {
	secretsDir := t.TempDir()
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}},
		Secrets: []v1alpha1.Secret{{
			Metadata: v1alpha1.Metadata{Name: "demo-cluster-admin-ssh-key"},
			Spec: v1alpha1.SecretSpec{
				Type: v1alpha1.SecretTypeSSHKeyPair,
				Source: v1alpha1.SecretSource{Generated: &v1alpha1.SecretGeneratedSource{
					KeyType: v1alpha1.SSHKeyPairTypeEd25519,
					Comment: "bootwright-demo-cluster-admin",
				}},
			},
		}},
	}

	results, err := MaterializeForContext("test", secretsDir, state, MaterializeOptions{Generated: true})
	if err != nil {
		t.Fatalf("MaterializeForContext: %v", err)
	}
	if len(results) != 1 || results[0].Name != "demo-cluster-admin-ssh-key" {
		t.Fatalf("results = %+v", results)
	}
	privatePath := filepath.Join(secretsDir, "demo-cluster-admin-ssh-key")
	publicPath := privatePath + ".pub"
	privateBody := readTestSecret(t, secretsDir, "demo-cluster-admin-ssh-key", MaterialSSHPrivate)
	publicBody := readTestSecret(t, secretsDir, "demo-cluster-admin-ssh-key", MaterialSSHPublic)
	if !strings.Contains(privateBody, "OPENSSH PRIVATE KEY") {
		t.Fatalf("private key missing OpenSSH header:\n%s", privateBody)
	}
	if !strings.HasPrefix(publicBody, "ssh-ed25519 ") || !strings.Contains(publicBody, " bootwright-demo-cluster-admin\n") {
		t.Fatalf("public key = %q", publicBody)
	}
	assertTestFileMode(t, privatePath, 0o600)
	assertTestFileMode(t, publicPath, 0o600)

	results, err = MaterializeForContext("test", secretsDir, state, MaterializeOptions{Generated: true})
	if err != nil {
		t.Fatalf("MaterializeForContext second run: %v", err)
	}
	if len(results) != 1 || !strings.Contains(results[0].Action, "reused existing SSH key pair") {
		t.Fatalf("second results = %+v", results)
	}
	if got := readTestSecret(t, secretsDir, "demo-cluster-admin-ssh-key", MaterialSSHPrivate); got != privateBody {
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
			},
		}},
		Secrets: []v1alpha1.Secret{{
			Metadata:   v1alpha1.Metadata{Name: "cluster-cluster-admin-ssh-key"},
			SourcePath: filepath.Join(sourceDir, "environment.yaml"),
			Spec:       v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeSSHKeyPair, Source: v1alpha1.SecretSource{File: &v1alpha1.SecretFileSource{PrivateKey: privateSource}}},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster"},
			Spec: v1alpha1.ContainerClusterSpec{Install: v1alpha1.OCPInstallSpec{
				NodeSSH: v1alpha1.NodeSSHSpec{KeyPairRef: v1alpha1.SecretRef{Name: "cluster-cluster-admin-ssh-key"}},
			}},
		}},
	}

	results, err := MaterializeForContext("test", secretsDir, state, MaterializeOptions{FileSources: true})
	if err != nil {
		t.Fatalf("MaterializeForContext: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want two SSH key copies", results)
	}
	if got := readTestSecret(t, secretsDir, "cluster-cluster-admin-ssh-key", MaterialSSHPrivate); got != "PRIVATE\n" {
		t.Fatalf("private copy = %q", got)
	}
	if got := readTestSecret(t, secretsDir, "cluster-cluster-admin-ssh-key", MaterialSSHPublic); got != "ssh-ed25519 AAAA test\n" {
		t.Fatalf("public copy = %q", got)
	}
	assertTestFileMode(t, filepath.Join(secretsDir, "cluster-cluster-admin-ssh-key"), 0o600)
	assertTestFileMode(t, filepath.Join(secretsDir, "cluster-cluster-admin-ssh-key.pub"), 0o600)
}

func TestMaterializeCopiesSplitNodeSSHFileSources(t *testing.T) {
	sourceDir := t.TempDir()
	privateSource := filepath.Join(sourceDir, "id_ed25519")
	publicSource := filepath.Join(sourceDir, "admin.pub")
	if err := os.WriteFile(privateSource, []byte("PRIVATE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicSource, []byte("ssh-ed25519 AAAA split\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	secretsDir := t.TempDir()
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata:   v1alpha1.Metadata{Name: "env"},
			SourcePath: filepath.Join(sourceDir, "environment.yaml"),
			Spec: v1alpha1.EnvironmentSpec{
				SecretStorage: v1alpha1.EnvironmentSecretStorageSpec{Mode: v1alpha1.SecretStorageModeContext},
			},
		}},
		Secrets: []v1alpha1.Secret{
			{
				Metadata:   v1alpha1.Metadata{Name: "cluster-admin-public"},
				SourcePath: filepath.Join(sourceDir, "environment.yaml"),
				Spec:       v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeSSHKeyPair, Source: v1alpha1.SecretSource{File: &v1alpha1.SecretFileSource{PublicKey: publicSource}}},
			},
			{
				Metadata:   v1alpha1.Metadata{Name: "cluster-admin-private"},
				SourcePath: filepath.Join(sourceDir, "environment.yaml"),
				Spec:       v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeSSHKeyPair, Source: v1alpha1.SecretSource{File: &v1alpha1.SecretFileSource{PrivateKey: privateSource}}},
			},
		},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster"},
			Spec: v1alpha1.ContainerClusterSpec{Install: v1alpha1.OCPInstallSpec{
				NodeSSH: v1alpha1.NodeSSHSpec{
					PublicKeyRef:  v1alpha1.SecretRef{Name: "cluster-admin-public"},
					PrivateKeyRef: v1alpha1.SecretRef{Name: "cluster-admin-private"},
				},
			}},
		}},
	}

	results, err := MaterializeForContext("test", secretsDir, state, MaterializeOptions{FileSources: true})
	if err != nil {
		t.Fatalf("MaterializeForContext: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want two SSH key copies", results)
	}
	if got := readTestSecret(t, secretsDir, "cluster-admin-private", MaterialSSHPrivate); got != "PRIVATE\n" {
		t.Fatalf("private copy = %q", got)
	}
	if got := readTestSecret(t, secretsDir, "cluster-admin-public", MaterialSSHPublic); got != "ssh-ed25519 AAAA split\n" {
		t.Fatalf("public copy = %q", got)
	}
}

func readTestSecret(t *testing.T, secretsDir, name string, role MaterialRole) string {
	t.Helper()
	data, err := NewContextStore("test", secretsDir).Read(MaterialKey{Name: name, Role: role})
	if err != nil {
		t.Fatalf("read encrypted %s/%s: %v", name, role, err)
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
