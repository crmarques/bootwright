package clusteraccess

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	secretstore "github.com/crmarques/bootwright/internal/secrets"
)

func TestRevealClusterSecretDecryptsEncryptedMaterial(t *testing.T) {
	clustersDir := t.TempDir()
	cluster := "ceph-libvirt"
	secretsDir := workflow.ClusterSecretsDir(clustersDir, cluster)
	store := secretstore.NewContextStore("test", secretsDir)
	if err := store.Write(secretstore.MaterialKey{Name: "dashboard-password", Role: secretstore.MaterialPrimary}, []byte("hunter2\n")); err != nil {
		t.Fatalf("seed encrypted dashboard-password: %v", err)
	}

	value, err := RevealClusterSecret("test", clustersDir, cluster, "dashboard-password")
	if err != nil {
		t.Fatalf("RevealClusterSecret: %v", err)
	}
	if value != "hunter2" {
		t.Fatalf("revealed value = %q, want %q", value, "hunter2")
	}
}

func TestRevealClusterSecretRejectsPlaintextMaterial(t *testing.T) {
	clustersDir := t.TempDir()
	cluster := "ceph-libvirt"
	secretsDir := workflow.ClusterSecretsDir(clustersDir, cluster)
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatalf("mkdir secrets dir: %v", err)
	}
	path := filepath.Join(secretsDir, "dashboard-password")
	if err := os.WriteFile(path, []byte("hunter2\n"), 0o600); err != nil {
		t.Fatalf("seed plaintext dashboard-password: %v", err)
	}

	_, err := RevealClusterSecret("test", clustersDir, cluster, "dashboard-password")
	if err == nil || !strings.Contains(err.Error(), "not encrypted") {
		t.Fatalf("RevealClusterSecret error = %v, want plaintext refusal", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(raw) != "hunter2\n" {
		t.Fatalf("plaintext material changed during rejected read: %q", raw)
	}
}

func TestKubeconfigDecryptsEncryptedMaterial(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	clustersDir := filepath.Join(t.TempDir(), "clusters")
	cluster := "sno-libvirt"
	secretsDir := workflow.ClusterSecretsDir(clustersDir, cluster)
	store := secretstore.NewContextStore("test", secretsDir)
	if err := store.Write(secretstore.MaterialKey{Name: "kubeconfig", Role: secretstore.MaterialPrimary}, []byte("apiVersion: v1\n")); err != nil {
		t.Fatalf("seed encrypted kubeconfig: %v", err)
	}

	data, err := Kubeconfig(state, "test", clustersDir, cluster)
	if err != nil {
		t.Fatalf("Kubeconfig: %v", err)
	}
	if string(data) != "apiVersion: v1\n" {
		t.Fatalf("kubeconfig content = %q, want %q", data, "apiVersion: v1\n")
	}
}
