package ceph_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render/ceph"
)

func batchExecCluster() (v1alpha1.State, v1alpha1.StorageCluster) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Type: v1alpha1.StorageClusterTypeCeph, Ceph: &v1alpha1.StorageClusterCephSpec{
			Networks:   v1alpha1.StorageCephNetworks{PublicCIDRs: []string{"10.0.0.0/24"}},
			MgrModules: []string{"rbd_support"},
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
				Name:       "ceph-0",
				MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
				Roles:      []string{v1alpha1.StorageCephRoleMON, v1alpha1.StorageCephRoleOSD},
			}}},
		}},
	}
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{cluster},
		StoragePools: []v1alpha1.StoragePool{{
			Metadata: v1alpha1.Metadata{Name: "existing"},
			Spec: v1alpha1.StoragePoolSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Ceph:              v1alpha1.StoragePoolCephSpec{Type: v1alpha1.StoragePoolTypeReplicated},
			},
		}, {
			Metadata: v1alpha1.Metadata{Name: "missing"},
			Spec: v1alpha1.StoragePoolSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Ceph:              v1alpha1.StoragePoolCephSpec{Type: v1alpha1.StoragePoolTypeReplicated},
			},
		}},
	}
	return state, cluster
}

func stageBatch(t *testing.T, dir string, doc map[string]any) []string {
	t.Helper()
	files, _ := doc["batchFiles"].([]map[string]any)
	if len(files) == 0 {
		t.Fatalf("rendered operations carry no batch files: %v", doc)
	}
	var scripts []string
	for _, file := range files {
		name := file["file"].(string)
		content := strings.ReplaceAll(file["content"].(string), ceph.CephBatchMountRoot+"/", dir+"/")
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o700); err != nil {
			t.Fatalf("stage %s: %v", name, err)
		}
		if strings.HasPrefix(name, "apply-ops-") {
			scripts = append(scripts, filepath.Join(dir, name))
		}
	}
	if len(scripts) == 0 {
		t.Fatalf("rendered operations carry no batch script: %v", files)
	}
	return scripts
}

func stubCephBin(t *testing.T, dir, failing string) string {
	t.Helper()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", bin, err)
	}
	stub := `#!/bin/bash
log="${BW_STUB_LOG}"
printf '%s\n' "$*" >>"$log"
if [[ -n "${BW_STUB_FAIL:-}" && "$*" == *"${BW_STUB_FAIL}"* ]]; then
  echo "stub failure for $*" >&2
  exit 22
fi
case "$*" in
  "osd pool ls --format json") echo '["existing"]' ;;
  "mon dump --format json") echo '{"stretch_mode": false}' ;;
  "osd crush rule dump --format json") echo '[]' ;;
  "osd erasure-code-profile ls --format json") echo '[]' ;;
esac
exit 0
`
	for _, name := range []string{"ceph", "radosgw-admin", "rbd"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(stub), 0o755); err != nil {
			t.Fatalf("write stub %s: %v", name, err)
		}
	}
	return bin
}

func runBatch(t *testing.T, script, bin, logPath, failing string) (int, string) {
	t.Helper()
	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"BW_STUB_LOG="+logPath,
		"BW_STUB_FAIL="+failing,
	)
	out, err := cmd.CombinedOutput()
	code := 0
	var exitErr *exec.ExitError
	if err != nil {
		if !asExitError(err, &exitErr) {
			t.Fatalf("run %s: %v\n%s", script, err, out)
		}
		code = exitErr.ExitCode()
	}
	return code, string(out)
}

func asExitError(err error, target **exec.ExitError) bool {
	if e, ok := err.(*exec.ExitError); ok {
		*target = e
		return true
	}
	return false
}

func TestCephBatchScriptGuardsSkipLiveResourcesAndRunMissingOnes(t *testing.T) {
	requireBatchTools(t)
	state, cluster := batchExecCluster()
	dir := t.TempDir()
	scripts := stageBatch(t, dir, ceph.CephOperations(state, cluster))
	bin := stubCephBin(t, dir, "")
	logPath := filepath.Join(dir, "calls.log")

	for _, script := range scripts {
		if code, out := runBatch(t, script, bin, logPath, ""); code != 0 {
			t.Fatalf("batch %s exited %d:\n%s", filepath.Base(script), code, out)
		}
	}
	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read stub log: %v", err)
	}
	log := string(calls)
	if strings.Contains(log, "osd pool create existing") {
		t.Fatalf("a pool the live cluster already holds must be skipped by the batch guard:\n%s", log)
	}
	if !strings.Contains(log, "osd pool create missing") {
		t.Fatalf("a pool the live cluster lacks must be created by the batch:\n%s", log)
	}
	if !strings.Contains(log, "mgr module enable rbd_support") {
		t.Fatalf("mgr modules must converge through Ceph's native idempotent enable command:\n%s", log)
	}
	if strings.Contains(log, "mgr module ls") {
		t.Fatalf("mgr module convergence must not run the detailed module-list probe:\n%s", log)
	}
}

func TestCephBatchScriptTreatsAnUnreadableProbeAsMissing(t *testing.T) {
	requireBatchTools(t)
	state, cluster := batchExecCluster()
	dir := t.TempDir()
	scripts := stageBatch(t, dir, ceph.CephOperations(state, cluster))
	bin := stubCephBin(t, dir, "")
	logPath := filepath.Join(dir, "calls.log")

	for _, script := range scripts {
		code, out := runBatch(t, script, bin, logPath, "osd pool ls")
		if code != 0 {
			t.Fatalf("a failed existence probe must not fail the batch, got %d:\n%s", code, out)
		}
	}
	log := readFile(t, logPath)
	for _, want := range []string{"osd pool create existing", "osd pool create missing"} {
		if !strings.Contains(log, want) {
			t.Fatalf("an unreadable existence probe must fall through to the create command (%q):\n%s", want, log)
		}
	}
}

func TestCephBatchScriptNamesTheFailingOperationAndStops(t *testing.T) {
	requireBatchTools(t)
	state, cluster := batchExecCluster()
	dir := t.TempDir()
	scripts := stageBatch(t, dir, ceph.CephOperations(state, cluster))
	bin := stubCephBin(t, dir, "")
	logPath := filepath.Join(dir, "calls.log")

	code, out := runBatch(t, scripts[0], bin, logPath, "config set global public_network")
	if code == 0 {
		t.Fatalf("a failing operation must fail the batch:\n%s", out)
	}
	if !strings.Contains(out, "BOOTWRIGHT_CEPH_OP_FAILED set-public-network") {
		t.Fatalf("the batch must name the failing operation on a machine-readable marker line:\n%s", out)
	}
	if strings.Contains(out, "BOOTWRIGHT_CEPH_BATCH_COMPLETE") {
		t.Fatalf("the batch must stop at the first failing operation:\n%s", out)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func requireBatchTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"bash", "python3"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available", tool)
		}
	}
}
