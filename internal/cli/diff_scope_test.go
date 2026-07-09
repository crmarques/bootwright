package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestDiffClustersScopeExcludesRenderReferenceStorage(t *testing.T) {
	setTestHomeAndRoot(t)
	example := filepath.Join("..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")
	if _, stderr, code := runCLI(t, "context", "init", "--name", "sc-scope", "-f", example); code != 0 {
		t.Fatalf("context init exited %d, stderr=%q", code, stderr)
	}

	report := diffJSON(t, "dc1-metal-ocp")
	for _, root := range report.Roots {
		if root.Kind == workflow.ApplyClusterKindStorage && root.Name == "ceph-storage" {
			t.Fatalf("render-reference StorageCluster ceph-storage must not be a diff root for a container-only scope; roots=%+v", report.Roots)
		}
		for _, res := range root.Resources {
			if strings.HasPrefix(res.ResourceID, "StoragePool/ceph-storage.") ||
				strings.HasPrefix(res.ResourceID, "StorageExport/ceph-storage.") {
				t.Fatalf("render-reference storage sub-object %q must not be reported for a container-only scope; roots=%+v", res.ResourceID, report.Roots)
			}
		}
	}
}

func TestDiffStorageScopeReportsNamedStorageRoot(t *testing.T) {
	setTestHomeAndRoot(t)
	example := filepath.Join("..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")
	if _, stderr, code := runCLI(t, "context", "init", "--name", "sc-scope", "-f", example); code != 0 {
		t.Fatalf("context init exited %d, stderr=%q", code, stderr)
	}

	report := diffJSON(t, "ceph-storage")
	found := false
	for _, root := range report.Roots {
		if root.Kind == workflow.ApplyClusterKindStorage && root.Name == "ceph-storage" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a directly-named storage root must be a diff root; roots=%+v", report.Roots)
	}
}

func diffJSON(t *testing.T, clusters string) workflow.StateCheckReport {
	t.Helper()
	stdout, stderr, code := runCLI(t, "diff", "--recorded", "--clusters", clusters, "--output", "json")
	if code == 1 || code == 2 {
		t.Fatalf("diff --clusters %s exited %d, stderr=%q\n%s", clusters, code, stderr, stdout)
	}
	var report workflow.StateCheckReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode diff json: %v\n%s", err, stdout)
	}
	return report
}
