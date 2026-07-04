package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

// TestStateCheckClustersScopeExcludesRenderReferenceStorage is the diff
// mirror of the scoped-apply/destroy storage work-set rule: a
// `diff --clusters <container-cluster>` whose data-foundation attachment
// pulls a managed StorageCluster into the render-inclusive state must NOT report
// that StorageCluster (or its pools/exports) as absent or drifted, because
// scoped apply would never provision it (ADR-0004). Before the fix diff
// enumerated every StorageCluster in the scoped RenderState into the apply
// target, so the render-reference ceph-storage and its sub-objects showed up as
// drift and forced exit 3.
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

// TestStateCheckStorageScopeReportsNamedStorageRoot is the symmetric positive
// case: naming the storage root directly makes it (and its sub-objects) part of
// the checked set, exactly as scoped apply provisions it.
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
	stdout, stderr, code := runCLI(t, "diff", "--clusters", clusters, "--output", "json")
	// diff exits 3 on drift (never-applied scope) and 0 in sync; both carry
	// a parsable JSON report. A load/usage error (1/2) is a test failure.
	if code == 1 || code == 2 {
		t.Fatalf("diff --clusters %s exited %d, stderr=%q\n%s", clusters, code, stderr, stdout)
	}
	var report workflow.StateCheckReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode diff json: %v\n%s", err, stdout)
	}
	return report
}
