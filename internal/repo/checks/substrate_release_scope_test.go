package repocheck

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoFilesContaining(t *testing.T, root, ext, needle string) []string {
	t.Helper()
	base := filepath.Join(repoRoot(t), root)
	var out []string
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ext {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if !strings.Contains(string(body), needle) {
			return nil
		}
		rel, relErr := filepath.Rel(repoRoot(t), path)
		if relErr != nil {
			return relErr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

var substrateResetConsumers = []string{
	"ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml",
	"ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml",
	"ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/layout.yml",
	"ansible/collections/ansible_collections/bootwright/core/roles/machine_os_identity/tasks/probe_existing.yml",
}

func TestEverySubstrateResetConsumerIsMachineScoped(t *testing.T) {
	for _, rel := range substrateResetConsumers {
		body := readRepoFile(t, rel)
		if !strings.Contains(body, "bootwright_substrate_reset_clusters") {
			t.Errorf("%s is listed as a substrate-reset consumer but no longer reads bootwright_substrate_reset_clusters; drop it from substrateResetConsumers or restore the gate", rel)
			continue
		}
		if !strings.Contains(body, "bootwright_substrate_reset_machines") {
			t.Errorf("%s decides a destructive substrate reset from bootwright_substrate_reset_clusters alone; a `destroy --machines` release is machine-granular, so the cluster name authorizes rebuilding every sibling machine of a live cluster. Narrow the predicate with bootwright_substrate_reset_machines the way machine_os_identity/tasks/probe_existing.yml does", rel)
		}
	}
}

func TestNoUnlistedSubstrateResetConsumer(t *testing.T) {
	listed := map[string]bool{}
	for _, rel := range substrateResetConsumers {
		listed[rel] = true
	}
	for _, rel := range repoFilesContaining(t, "ansible", ".yml", "bootwright_substrate_reset_clusters") {
		if !listed[rel] {
			t.Errorf("%s reads bootwright_substrate_reset_clusters but is not in substrateResetConsumers; every consumer of the substrate release must be machine-scoped and listed here, or a new provider inherits the cluster-wide blast radius unguarded", rel)
		}
	}
}
