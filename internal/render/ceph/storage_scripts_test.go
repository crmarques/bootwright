package ceph_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/host/shellquote"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/render/ceph"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

// TestCephApplyScriptReproducesEveryOperation is the anti-drift guard: for the
// canonical managed-Ceph fixture, every operation CephOperations emits must be
// represented in the generated apply.sh — either as its exact shell-quoted
// native command, or (for a structured op with no argv, like the stretch CRUSH
// rule) as a clearly marked bootwright-apply-only stub. If someone adds a new
// operation family to CephOperations and forgets the script transform, this
// fails.
func TestCephApplyScriptReproducesEveryOperation(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	result, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state)
	if err != nil {
		t.Fatalf("render.All: %v", err)
	}
	if len(result.StorageAssets) != 1 {
		t.Fatalf("storage assets got %d, want 1", len(result.StorageAssets))
	}
	asset := result.StorageAssets[0]

	if asset.ApplyScriptPath == "" || asset.ApplyLibPath == "" {
		t.Fatalf("managed cluster asset missing script paths: %#v", asset)
	}
	assertFileMode(t, asset.ApplyScriptPath, 0o755)
	assertFileMode(t, asset.ApplyLibPath, 0o755)

	script := readScript(t, asset.ApplyScriptPath)

	ops := ceph.CephOperations(state, state.StorageClusters[0])["operations"].([]map[string]any)
	if len(ops) == 0 {
		t.Fatal("CephOperations returned no operations for the fixture")
	}
	for _, op := range ops {
		name, _ := op["name"].(string)
		cmd, _ := op["command"].([]string)
		if !strings.Contains(script, "# "+name+"\n") {
			t.Fatalf("apply.sh missing comment for op %q", name)
		}
		if len(cmd) == 0 {
			// An argv-less op must still be represented: the stretch CRUSH rule
			// as a bw_stretch_crush_rule call (it is compiled into the CRUSH map,
			// not one native command), anything else as a marked stub.
			idem, _ := op["idempotency"].(map[string]any)
			if kind, _ := idem["kind"].(string); kind == "stretch-crush-rule" {
				if !strings.Contains(script, "bw_stretch_crush_rule") {
					t.Fatalf("stretch rule op %q not emitted as bw_stretch_crush_rule", name)
				}
			} else if !strings.Contains(script, "[todo]") {
				t.Fatalf("argv-less op %q missing stub in apply.sh", name)
			}
			continue
		}
		if quoted := shellquote.Quote(cmd); !strings.Contains(script, quoted) {
			t.Fatalf("apply.sh missing native command for op %q:\n  want substring: %s", name, quoted)
		}
	}
}

// TestCephApplyScriptGuardingAndRedaction pins the behavioral contract: create-
// style objects are guarded skip-if-exists, in-place reconciles run directly,
// secret-bearing output is redacted, and shell metacharacters are quoted.
func TestCephApplyScriptGuardingAndRedaction(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	result, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state)
	if err != nil {
		t.Fatalf("render.All: %v", err)
	}
	asset := result.StorageAssets[0]
	script := readScript(t, asset.ApplyScriptPath)
	lib := readScript(t, asset.ApplyLibPath)

	for _, want := range []string{
		// Bootstrap with the render-resolved mon IP + cluster network + config.
		"cephadm bootstrap --mon-ip 192.168.141.30",
		`--cluster-network 172.21.141.0/24,172.21.142.0/24`,
		`--config "$HERE/cephadm/bootstrap-ceph.conf"`,
		// Declarative service specs applied via ceph orch apply.
		`bw_run ceph orch apply -i "$HERE/cephadm/bootstrap-spec.yaml"`,
		`bw_run ceph orch apply -i "$HERE/cephadm/core-services.yaml"`,
		`bw_run ceph orch apply -i "$HERE/cephadm/late-services.yaml"`,
		// A create-style pool and cephfs are guarded skip-if-exists.
		"bw_guarded ceph-pool odf-rbd ceph osd pool create odf-rbd",
		"bw_guarded cephfs odf-cephfs ceph fs new odf-cephfs odf-cephfs-metadata odf-cephfs-data",
		// The stretch CRUSH rule is compiled into the CRUSH map (it cannot be a
		// single native command) so the dependent steps below can reference it.
		"bw_stretch_crush_rule stretch-rule datacenter 2",
		// enable_stretch_mode is guarded by the stretch-mode probe.
		"bw_guarded stretch-mode enabled ceph mon enable_stretch_mode",
		// The standalone RGW admin user is guarded AND redacted (no_log).
		"bw_guarded_quiet rgw-user bootwright-odf-rgw-admin",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("apply.sh missing %q", want)
		}
	}

	for _, want := range []string{
		"command -v jq",
		"_bw_exists()",
		"ceph-pool)",
		"nfs-export)",
		"BW_CEPH_PREFIX",
		"bw_stretch_crush_rule()",
	} {
		if !strings.Contains(lib, want) {
			t.Errorf("lib.sh missing %q", want)
		}
	}

	// The generated scripts must never carry secret material.
	for _, forbidden := range []string{"-----BEGIN", "BOOTWRIGHT_GENERATED_AT_APPLY_TIME"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("apply.sh leaked %q", forbidden)
		}
	}
}

// TestCephApplyScriptIsValidBash parses the generated script and library with
// `bash -n` so a generation bug that produces malformed bash fails the build.
func TestCephApplyScriptIsValidBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	result, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state)
	if err != nil {
		t.Fatalf("render.All: %v", err)
	}
	asset := result.StorageAssets[0]
	for _, path := range []string{asset.ApplyLibPath, asset.ApplyScriptPath} {
		out, err := exec.Command(bash, "-n", path).CombinedOutput()
		if err != nil {
			t.Fatalf("bash -n %s failed: %v\n%s", filepath.Base(path), err, out)
		}
	}
}

// TestCephApplyScriptOmittedForUnmanagedCluster confirms an external (imported)
// Ceph cluster gets no apply script — there is nothing for bootwright to
// configure and no bootstrap host to run against.
func TestCephApplyScriptOmittedForUnmanagedCluster(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			SourcePath: filepath.Join(t.TempDir(), "environment.yaml"),
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "shared-ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type:       v1alpha1.StorageClusterTypeCeph,
				Management: v1alpha1.StorageClusterManagementExternal,
			},
		}},
	}
	result, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state)
	if err != nil {
		t.Fatalf("render.All: %v", err)
	}
	if len(result.StorageAssets) != 1 {
		t.Fatalf("storage assets got %d, want 1", len(result.StorageAssets))
	}
	if got := result.StorageAssets[0].ApplyScriptPath; got != "" {
		t.Fatalf("external cluster rendered an apply script: %q", got)
	}
}

func readScript(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %#o, want %#o", filepath.Base(path), got, want)
	}
}
