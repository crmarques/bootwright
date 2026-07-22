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
			idem, _ := op["idempotency"].(map[string]any)
			kind, _ := idem["kind"].(string)
			if kind == "stretch-crush-rule" {
				if !strings.Contains(script, "bw_stretch_crush_rule") {
					t.Fatalf("stretch rule op %q not emitted as bw_stretch_crush_rule", name)
				}
			} else if kind == "stretch-internal-pools" {
				if !strings.Contains(script, "bw_stretch_internal_pools") {
					t.Fatalf("stretch internal-pool op %q not emitted as bw_stretch_internal_pools", name)
				}
			} else if !strings.Contains(script, "[todo]") {
				t.Fatalf("argv-less op %q missing stub in apply.sh", name)
			}
			continue
		}
		if quoted := shellquote.Quote(cmd); !strings.Contains(script, quoted) {
			t.Fatalf("apply.sh missing native command for op %q:\n  want substring: %s", name, quoted)
		}
		if stdin, _ := op["stdin"].(string); stdin != "" {
			if !strings.Contains(script, "<<'BW_STDIN'") || !strings.Contains(script, stdin) {
				t.Fatalf("apply.sh must feed stdin via heredoc for declarative op %q", name)
			}
		}
	}
}

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
		"cephadm bootstrap --mon-ip 192.168.141.30",
		`--cluster-network 172.21.141.0/24,172.21.142.0/24`,
		`--config "$HERE/cephadm/bootstrap-ceph.conf"`,
		`bw_run ceph orch apply -i "$HERE/cephadm/bootstrap-spec.yaml"`,
		`bw_run ceph orch apply -i "$HERE/cephadm/core-services.yaml"`,
		`bw_run ceph orch apply -i "$HERE/cephadm/late-services.yaml"`,
		"bw_guarded ceph-pool odf-rbd ceph osd pool create odf-rbd",
		"bw_guarded cephfs odf-cephfs ceph fs new odf-cephfs odf-cephfs-metadata odf-cephfs-data",
		"bw_run ceph mon set_location node-01 datacenter=dc1",
		"bw_run ceph osd crush move node-01 root=default datacenter=dc1",
		"bw_stretch_crush_rule stretch-rule datacenter 2",
		"bw_guarded stretch-mode enabled ceph mon enable_stretch_mode",
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
		"BW_CEPH_PREFIX",
		"bw_stretch_crush_rule()",
	} {
		if !strings.Contains(lib, want) {
			t.Errorf("lib.sh missing %q", want)
		}
	}

	for _, forbidden := range []string{"-----BEGIN", "BOOTWRIGHT_GENERATED_AT_APPLY_TIME"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("apply.sh leaked %q", forbidden)
		}
	}
}

func TestCephApplyScriptBootstrapImageParity(t *testing.T) {
	tests := []struct {
		name         string
		distribution string
		release      string
		image        string
		callHome     string
		want         string
		wantLicense  bool
		wantCallHome []string
		reject       []string
	}{
		{
			name:         "oss version derives image",
			distribution: v1alpha1.StorageCephDistributionOSS,
			release:      "20.2.2",
			want:         "bootstrap=(cephadm --image quay.io/ceph/ceph:v20.2.2 bootstrap --mon-ip 192.0.2.10",
		},
		{
			name:         "ibm enabled reconciles call home",
			distribution: v1alpha1.StorageCephDistributionIBM,
			release:      "9.9.1",
			image:        "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-17759",
			callHome:     v1alpha1.StorageCephIBMCallHomeEnabled,
			want:         "bootstrap=(cephadm --image cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-17759 bootstrap --mon-ip 192.0.2.10",
			wantLicense:  true,
			wantCallHome: []string{
				`ibm_call_home_modules="$(_bw_exec ceph mgr module ls --format json)"`,
				`bw_run ceph mgr module enable call_home_agent`,
				`bw_run ceph orch accept call-home-enabled`,
			},
			reject: []string{`bw_run ceph orch deny call-home-enabled`},
		},
		{
			name:         "ibm disabled reconciles call home",
			distribution: v1alpha1.StorageCephDistributionIBM,
			release:      "9.9.1",
			image:        "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-17759",
			callHome:     v1alpha1.StorageCephIBMCallHomeDisabled,
			want:         "bootstrap=(cephadm --image cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-17759 bootstrap --mon-ip 192.0.2.10",
			wantLicense:  true,
			wantCallHome: []string{
				`bw_run ceph orch deny call-home-enabled`,
			},
			reject: []string{
				`ibm_call_home_modules="$(_bw_exec ceph mgr module ls --format json)"`,
				`bw_run ceph mgr module enable call_home_agent`,
				`bw_run ceph orch accept call-home-enabled`,
				`if [[ "$ibm_call_home_enabled" == "true" ]]`,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cluster := v1alpha1.StorageCluster{
				Metadata: v1alpha1.Metadata{Name: "ceph"},
				Spec: v1alpha1.StorageClusterSpec{Type: v1alpha1.StorageClusterTypeCeph, Ceph: &v1alpha1.StorageClusterCephSpec{
					Distribution: tc.distribution,
					Release:      tc.release,
					Image:        tc.image,
					Cephadm: v1alpha1.StorageCephadmSpec{
						AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
						Bootstrap: v1alpha1.StorageCephadmBootstrap{
							Node:       "ceph-0",
							AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
						},
					},
					Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
						Name: "ceph-0",
						MachineRef: v1alpha1.LocalObjectReference{
							Name: "ceph-0",
						},
						Roles: []string{v1alpha1.StorageCephRoleMON},
					}}},
				}},
			}
			if tc.distribution == v1alpha1.StorageCephDistributionIBM {
				cluster.Spec.Ceph.EntitlementRef.Name = "ibm"
				cluster.Spec.Ceph.IBM = &v1alpha1.StorageCephIBMSpec{CallHome: tc.callHome}
			}
			state := v1alpha1.State{
				Environments: []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}},
				Entitlements: []v1alpha1.Entitlement{{
					Metadata: v1alpha1.Metadata{Name: "ibm"},
					Spec: v1alpha1.EntitlementSpec{
						Type:    v1alpha1.EntitlementTypeIBMStorageCeph,
						License: &v1alpha1.EntitlementLicense{Accept: true},
					},
				}},
				Machines: []v1alpha1.Machine{{
					Metadata: v1alpha1.Metadata{Name: "ceph-0"},
					Spec: v1alpha1.MachineSpec{
						Addresses: []v1alpha1.MachineAddress{{
							Name:    "ssh",
							Address: "192.0.2.10",
						}},
						Access: v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{
							AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
							User:       "root",
						}},
					},
				}},
				StorageClusters: []v1alpha1.StorageCluster{cluster},
			}
			result, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state)
			if err != nil {
				t.Fatalf("render.All: %v", err)
			}
			script := readScript(t, result.StorageAssets[0].ApplyScriptPath)
			if !strings.Contains(script, tc.want) {
				t.Fatalf("apply.sh bootstrap command missing provider image before subcommand:\n%s", script)
			}
			if got := strings.Contains(script, "--automatically-accept-license"); got != tc.wantLicense {
				t.Fatalf("apply.sh license flag presence = %t, want %t:\n%s", got, tc.wantLicense, script)
			}
			if !strings.Contains(script, `bootstrap+=(${BW_CEPHADM_BOOTSTRAP_EXTRA})`) {
				t.Fatalf("apply.sh lost bootstrap-subcommand extras:\n%s", script)
			}
			for _, want := range tc.wantCallHome {
				if !strings.Contains(script, want) {
					t.Errorf("apply.sh missing Call Home reconcile %q:\n%s", want, script)
				}
			}
			for _, reject := range tc.reject {
				if strings.Contains(script, reject) {
					t.Errorf("apply.sh contains unwanted Call Home reconcile %q:\n%s", reject, script)
				}
			}
			if tc.callHome == "" {
				if strings.Contains(script, "IBM Call Home") {
					t.Errorf("apply.sh rendered IBM Call Home stage for %s:\n%s", tc.distribution, script)
				}
				return
			}
			bootstrapEnd := strings.Index(script, "\nfi\n")
			callHomeStage := strings.Index(script, "== stage 05: IBM Call Home ==")
			if bootstrapEnd < 0 || callHomeStage < bootstrapEnd {
				t.Errorf("IBM Call Home reconcile must run after bootstrap guard so skipped bootstrap still reconciles:\n%s", script)
			}
		})
	}
}

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
