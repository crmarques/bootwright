package repocheck

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/crmarques/bootwright/internal/ownership"
)

func TestOwnershipKindsRegisteredInGo(t *testing.T) {
	known := map[string]bool{}
	for _, kind := range ownership.KnownKinds() {
		known[kind] = true
	}
	kindRE := regexp.MustCompile(`bootwright_ownership_kind:\s*([A-Za-z0-9_.-]+)`)
	rolesRoot := filepath.Join(repoRoot(t), "ansible/collections/ansible_collections/bootwright/core/roles")
	seen := map[string]string{}
	err := filepath.WalkDir(rolesRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".yml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repoRoot(t), path)
		for _, match := range kindRE.FindAllStringSubmatch(string(data), -1) {
			seen[match[1]] = rel
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan ownership_record kinds: %v", err)
	}
	if len(seen) == 0 {
		t.Fatal("found no literal bootwright_ownership_kind assignments; scan path or regex is stale")
	}
	for kind, file := range seen {
		if !known[kind] {
			t.Fatalf("Ansible role %s emits ownership kind %q not registered in ownership.KnownKinds; add a Kind constant and classification in internal/ownership/kinds.go", file, kind)
		}
	}
}
