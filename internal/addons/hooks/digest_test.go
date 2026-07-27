package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func writeHookContent(t *testing.T, dir, rel, body string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestContentDigestChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "add-on.yaml")
	writeHookContent(t, dir, "playbooks/run.yaml", "- hosts: all")
	hook := v1alpha1.ClusterAddonHook{Name: "h", Playbook: "playbooks/run.yaml"}

	first, err := ContentDigest(source, hook)
	if err != nil {
		t.Fatalf("first digest: %v", err)
	}
	writeHookContent(t, dir, "playbooks/run.yaml", "- hosts: none")
	second, err := ContentDigest(source, hook)
	if err != nil {
		t.Fatalf("second digest: %v", err)
	}
	if first == second {
		t.Fatalf("digest did not change with content: %s", first)
	}
}

func TestContentDigestFailsClosedOnUnreadableContent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "add-on.yaml")
	writeHookContent(t, dir, "playbooks/run.yaml", "- hosts: all")
	writeHookContent(t, dir, "manifests/object.yaml", "kind: ConfigMap")
	if err := os.Chmod(filepath.Join(dir, "manifests", "object.yaml"), 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	hook := v1alpha1.ClusterAddonHook{
		Name:      "h",
		Playbook:  "playbooks/run.yaml",
		Manifests: []v1alpha1.ClusterAddonHookManifest{{Path: "manifests/object.yaml"}},
	}
	if _, err := ContentDigest(source, hook); err == nil {
		t.Fatal("expected an error for unreadable hook content")
	} else if !strings.Contains(err.Error(), "object.yaml") {
		t.Fatalf("error should name the unreadable path, got: %v", err)
	}
}

func TestContentDigestToleratesAbsentOptionalPaths(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "add-on.yaml")
	writeHookContent(t, dir, "playbooks/run.yaml", "- hosts: all")

	hook := v1alpha1.ClusterAddonHook{Name: "h", Playbook: "playbooks/run.yaml"}
	withRoles := hook
	withRoles.RolesPath = "roles"
	bare, err := ContentDigest(source, hook)
	if err != nil {
		t.Fatalf("bare digest: %v", err)
	}
	absent, err := ContentDigest(source, withRoles)
	if err != nil {
		t.Fatalf("absent rolesPath digest: %v", err)
	}
	if bare != absent {
		t.Fatal("an absent optional path should not change the digest")
	}
}
