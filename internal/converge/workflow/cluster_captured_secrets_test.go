package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	secretstore "github.com/crmarques/bootwright/internal/secrets"
)

func TestEncryptCapturedClusterSecretsStorageClusterEncryptsInPlace(t *testing.T) {
	clustersDir := t.TempDir()
	cluster := "demo"
	secretsDir := ClusterSecretsDir(clustersDir, cluster)
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatalf("mkdir secrets dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(secretsDir, "addons", "storage"), 0o700); err != nil {
		t.Fatalf("mkdir add-on secrets: %v", err)
	}
	path := filepath.Join(secretsDir, "dashboard-password")
	if err := os.WriteFile(path, []byte("hunter2\n"), 0o600); err != nil {
		t.Fatalf("seed plaintext dashboard-password: %v", err)
	}

	opts := RunOptions{ContextName: "test", ClustersDir: clustersDir}
	task := ApplyTask{Entry: TaskLedgerEntry{ID: "storage." + cluster, Kind: ApplyTaskKindStorageCluster, Cluster: cluster}}
	if err := encryptCapturedClusterSecrets(opts, task); err != nil {
		t.Fatalf("encryptCapturedClusterSecrets: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(raw) == "hunter2\n" {
		t.Fatalf("dashboard-password must not remain plaintext on disk after encryptCapturedClusterSecrets")
	}
	if !strings.Contains(string(raw), `"algorithm"`) {
		t.Fatalf("dashboard-password does not look like an AES-256-GCM envelope: %s", raw)
	}

	store := secretstore.NewContextStore("test", secretsDir)
	data, err := store.Read(secretstore.MaterialKey{Name: "dashboard-password", Role: secretstore.MaterialPrimary})
	if err != nil {
		t.Fatalf("decrypt dashboard-password: %v", err)
	}
	if string(data) != "hunter2\n" {
		t.Fatalf("decrypted dashboard-password = %q, want %q", data, "hunter2\n")
	}

	if err := encryptCapturedClusterSecrets(opts, task); err != nil {
		t.Fatalf("encryptCapturedClusterSecrets must be idempotent on an already-encrypted store: %v", err)
	}
}

func TestEncryptCapturedClusterSecretsInstallWaitEncryptsKubeconfigAndPassword(t *testing.T) {
	clustersDir := t.TempDir()
	cluster := "demo"
	secretsDir := ClusterSecretsDir(clustersDir, cluster)
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatalf("mkdir secrets dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(secretsDir, "addons", "storage"), 0o700); err != nil {
		t.Fatalf("mkdir add-on secrets: %v", err)
	}
	seed := map[string]string{
		"kubeconfig":         "apiVersion: v1\n",
		"kubeadmin-password": "hunter2\n",
	}
	for name, content := range seed {
		if err := os.WriteFile(filepath.Join(secretsDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	opts := RunOptions{ContextName: "test", ClustersDir: clustersDir}
	task := ApplyTask{Entry: TaskLedgerEntry{ID: "wait." + cluster, Kind: ApplyTaskKindInstallWait, Cluster: cluster}}
	if err := encryptCapturedClusterSecrets(opts, task); err != nil {
		t.Fatalf("encryptCapturedClusterSecrets: %v", err)
	}

	store := secretstore.NewContextStore("test", secretsDir)
	for name, want := range seed {
		data, err := store.Read(secretstore.MaterialKey{Name: name, Role: secretstore.MaterialPrimary})
		if err != nil {
			t.Fatalf("decrypt %s: %v", name, err)
		}
		if string(data) != want {
			t.Fatalf("decrypted %s = %q, want %q", name, data, want)
		}
	}
}

func TestWithMaterializedClusterKubeconfigDecryptsAndCleansUp(t *testing.T) {
	clustersDir := t.TempDir()
	cluster := "demo"
	secretsDir := ClusterSecretsDir(clustersDir, cluster)
	store := secretstore.NewContextStore("test", secretsDir)
	if err := store.Write(secretstore.MaterialKey{Name: "kubeconfig", Role: secretstore.MaterialPrimary}, []byte("apiVersion: v1\n")); err != nil {
		t.Fatalf("seed encrypted kubeconfig: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(secretsDir, "addons", "storage"), 0o700); err != nil {
		t.Fatalf("mkdir add-on secrets: %v", err)
	}

	var seenPath string
	var seenContent []byte
	err := withMaterializedClusterKubeconfig("test", clustersDir, cluster, func(kubeconfigPath string) error {
		seenPath = kubeconfigPath
		data, readErr := os.ReadFile(kubeconfigPath)
		seenContent = data
		return readErr
	})
	if err != nil {
		t.Fatalf("withMaterializedClusterKubeconfig: %v", err)
	}
	if string(seenContent) != "apiVersion: v1\n" {
		t.Fatalf("materialized kubeconfig content = %q", seenContent)
	}
	if _, err := os.Stat(seenPath); !os.IsNotExist(err) {
		t.Fatalf("materialized kubeconfig scratch file must be removed after use, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Dir(seenPath)); !os.IsNotExist(err) {
		t.Fatalf("materialized kubeconfig scratch dir must be removed after use, stat err=%v", err)
	}
}

func TestWithMaterializedClusterKubeconfigMigratesLegacyPlaintext(t *testing.T) {
	clustersDir := t.TempDir()
	cluster := "demo"
	secretsDir := ClusterSecretsDir(clustersDir, cluster)
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatalf("mkdir secrets dir: %v", err)
	}
	path := filepath.Join(secretsDir, "kubeconfig")
	if err := os.WriteFile(path, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("seed legacy plaintext kubeconfig: %v", err)
	}

	var seenContent []byte
	err := withMaterializedClusterKubeconfig("test", clustersDir, cluster, func(kubeconfigPath string) error {
		data, readErr := os.ReadFile(kubeconfigPath)
		seenContent = data
		return readErr
	})
	if err != nil {
		t.Fatalf("withMaterializedClusterKubeconfig: %v", err)
	}
	if string(seenContent) != "apiVersion: v1\n" {
		t.Fatalf("materialized legacy kubeconfig content = %q", seenContent)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read on-disk kubeconfig: %v", err)
	}
	if string(raw) == "apiVersion: v1\n" {
		t.Fatalf("legacy plaintext kubeconfig was not encrypted in place by withMaterializedClusterKubeconfig")
	}
}

func TestRemoveClusterInstallStateRemovesEncryptedKubeconfigAndPassword(t *testing.T) {
	clustersDir := t.TempDir()
	cluster := "demo"
	secretsDir := ClusterSecretsDir(clustersDir, cluster)
	store := secretstore.NewContextStore("test", secretsDir)
	if err := store.Write(secretstore.MaterialKey{Name: "kubeconfig", Role: secretstore.MaterialPrimary}, []byte("apiVersion: v1\n")); err != nil {
		t.Fatalf("seed encrypted kubeconfig: %v", err)
	}
	if err := store.Write(secretstore.MaterialKey{Name: "kubeadmin-password", Role: secretstore.MaterialPrimary}, []byte("hunter2\n")); err != nil {
		t.Fatalf("seed encrypted kubeadmin-password: %v", err)
	}

	if err := RemoveClusterInstallState(clustersDir, "test", cluster); err != nil {
		t.Fatalf("RemoveClusterInstallState: %v", err)
	}

	for _, name := range []string{"kubeconfig", "kubeadmin-password"} {
		if _, err := os.Stat(filepath.Join(secretsDir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s must be removed, stat err=%v", name, err)
		}
	}
}
