package desiredstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRejectsYAMLSymlink(t *testing.T) {
	source := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("apiVersion: bootwright.io/v1alpha1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "linked.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load([]string{source}); err == nil {
		t.Fatal("Load accepted a YAML symlink")
	}
}

func TestLoadRejectsResourceSymlink(t *testing.T) {
	source := t.TempDir()
	env := `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: lab
spec:
  baseDomain: example.test
  bastion:
    hostRef: bastion
  resources:
    - linked.yaml
`
	if err := os.WriteFile(filepath.Join(source, "environment.yaml"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("apiVersion: bootwright.io/v1alpha1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "linked.yaml")); err != nil {
		t.Fatal(err)
	}

	_, err := Load([]string{filepath.Join(source, "environment.yaml")})
	if err == nil {
		t.Fatal("Load accepted a spec.resources symlink")
	}
	if !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("error does not reject resource symlink: %v", err)
	}
}
