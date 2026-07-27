package desiredstate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func externalSourceDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "playbooks"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "playbooks", "site.yml"), []byte("---\n- hosts: all\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

func TestValidatePlaybookAcceptsExternalSourcePath(t *testing.T) {
	dir := externalSourceDir(t)
	p := basePlaybook("external")
	p.Spec.Source = &v1alpha1.PlaybookSource{Path: dir}
	p.Spec.Playbook = "playbooks/site.yml"
	if errs := validatePlaybooks(provisioningState(p)); len(errs) != 0 {
		t.Fatalf("external source reported errors: %v", errs)
	}
}

func TestValidatePlaybookExternalSourceRejectsEscape(t *testing.T) {
	dir := externalSourceDir(t)
	p := basePlaybook("external")
	p.Spec.Source = &v1alpha1.PlaybookSource{Path: dir}
	p.Spec.Playbook = "../evil.yml"
	if errs := validatePlaybooks(provisioningState(p)); !containsSubstring(errs, "must stay within the source root") {
		t.Fatalf("escaping playbook not reported: %v", errs)
	}
}

func TestValidatePlaybookExternalSourceRejectsRelativeAndMissing(t *testing.T) {
	relative := basePlaybook("relative")
	relative.Spec.Source = &v1alpha1.PlaybookSource{Path: "ansible/os-hardening"}
	if errs := validatePlaybooks(provisioningState(relative)); !containsSubstring(errs, "must be an absolute directory") {
		t.Fatalf("relative source path not reported: %v", errs)
	}

	missing := basePlaybook("missing")
	missing.Spec.Source = &v1alpha1.PlaybookSource{Path: filepath.Join(t.TempDir(), "absent")}
	if errs := validatePlaybooks(provisioningState(missing)); !containsSubstring(errs, "does not exist") {
		t.Fatalf("missing source path not reported: %v", errs)
	}
}

func TestValidatePlaybookExternalSourceSkipsPlaybooksDirRule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "site.yml"), []byte("---\n- hosts: all\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	p := basePlaybook("flat")
	p.Spec.Source = &v1alpha1.PlaybookSource{Path: dir}
	p.Spec.Playbook = "site.yml"
	if errs := validatePlaybooks(provisioningState(p)); len(errs) != 0 {
		t.Fatalf("external content outside playbooks/ should be allowed: %v", errs)
	}
}
