package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestDestroyClustersScopeGatesStorageWorkSet is the end-to-end regression guard
// for the scoped-destroy bug: a `destroy --stage clusters --clusters <names>`
// must carry the centralized storage-teardown allowlist
// (bootwright_destroy_storage_scope) so a render-reference managed StorageCluster
// pulled into the render-inclusive state by a container cluster's
// data-foundation attachment is never wiped. The gate is composed once
// (converge.ApplyDestroyScopeExtraVars from clusteraccess.Selection) so the
// dry-run command faithfully previews what the real task-graph run tears down.
func TestDestroyClustersScopeGatesStorageWorkSet(t *testing.T) {
	setTestHomeAndRoot(t)
	example := filepath.Join("..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")
	if _, stderr, code := runCLI(t, "context", "init", "--name", "scope-gate", "-f", example); code != 0 {
		t.Fatalf("context init exited %d, stderr=%q", code, stderr)
	}

	t.Run("container-only tears down no storage", func(t *testing.T) {
		report := destroyDryRunJSON(t, "dc1-metal-ocp")
		// Empty allowlist => the storage destroy playbook end_hosts every rendered
		// storage node, so the render-reference ceph-storage is never wiped.
		if !slices.Contains(report.ExtraVars, "bootwright_destroy_storage_scope=") {
			t.Fatalf("container-only destroy must carry an empty storage allowlist; extraVars=%v", report.ExtraVars)
		}
		if !slices.Contains(report.Command, "bootwright_destroy_storage_scope=") {
			t.Fatalf("container-only destroy command must carry the storage gate; command=%v", report.Command)
		}
		// State stays render-inclusive (the ODF attachment must still render).
		if !renderedVarsContain(t, report, "name: ceph-storage") {
			t.Fatalf("render-inclusive state must still render ceph-storage for the attachment")
		}
	})

	t.Run("mixed selection tears down only the named storage root", func(t *testing.T) {
		report := destroyDryRunJSON(t, "dc1-metal-ocp,ceph-storage")
		if !slices.Contains(report.ExtraVars, "bootwright_destroy_storage_scope=ceph-storage") {
			t.Fatalf("mixed destroy must restrict storage teardown to ceph-storage; extraVars=%v", report.ExtraVars)
		}
	})
}

func destroyDryRunJSON(t *testing.T, clusters string) scopeDryRunReport {
	t.Helper()
	stdout, stderr, code := runCLI(t,
		"destroy", "--stage", "clusters", "--clusters", clusters,
		"--dry-run", "--output", "json", "--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("destroy dry-run (--clusters %s) exited %d, stderr=%q", clusters, code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode dry-run json: %v\n%s", err, stdout)
	}
	return report
}

func renderedVarsContain(t *testing.T, report scopeDryRunReport, want string) bool {
	t.Helper()
	if report.Render.VarsPath == "" {
		t.Fatalf("dry-run report has no rendered vars path")
	}
	b, err := os.ReadFile(report.Render.VarsPath)
	if err != nil {
		t.Fatalf("read rendered vars: %v", err)
	}
	return strings.Contains(string(b), want)
}
