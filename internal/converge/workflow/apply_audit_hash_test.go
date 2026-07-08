package workflow

import (
	"encoding/json"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func structuralJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal projection: %v", err)
	}
	return string(data)
}

// Promoting an installed worker to role: infra must leave the install structural
// hash byte-identical (the promotion is a reconfigure-only day-2 op), while
// master<->worker stays a genuine reinstall.
func TestInstallStructuralHashRoleProjection(t *testing.T) {
	mk := func(role string) v1alpha1.State {
		return v1alpha1.State{ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "demo"},
			Spec: v1alpha1.ContainerClusterSpec{Hosts: []v1alpha1.OCPHostSpec{{
				Hostname:   "n1",
				Role:       role,
				MachineRef: v1alpha1.LocalObjectReference{Name: "m1"},
			}}},
		}}}
	}
	worker := structuralJSON(t, containerClusterInstallStructuralHashVars(mk(v1alpha1.NodeRoleWorker)))
	infra := structuralJSON(t, containerClusterInstallStructuralHashVars(mk(v1alpha1.NodeRoleInfra)))
	master := structuralJSON(t, containerClusterInstallStructuralHashVars(mk(v1alpha1.NodeRoleMaster)))
	if worker != infra {
		t.Fatalf("worker<->infra promotion must not move the install structural hash:\n worker=%s\n infra=%s", worker, infra)
	}
	if worker == master {
		t.Fatalf("worker<->master must stay a structural change, but the projection matched")
	}
}

// Ceph day-2 reconfigures (config keys, mgr modules, monitoring tuning) and fabric
// edits (a node's BMC address) must not move the StorageCluster structural hash —
// none re-bootstraps a running cluster. A change to real cluster identity (the seed
// host) must move it.
func TestStorageStructuralHashReconfigureProjection(t *testing.T) {
	base := func() v1alpha1.State {
		return v1alpha1.State{
			Environments: []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}},
			Machines:     []v1alpha1.Machine{{Metadata: v1alpha1.Metadata{Name: "m1"}}},
			StorageClusters: []v1alpha1.StorageCluster{{
				Metadata: v1alpha1.Metadata{Name: "ceph"},
				Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
					Cephadm:    v1alpha1.StorageCephadmSpec{Bootstrap: v1alpha1.StorageCephadmBootstrap{Host: "seed-a"}},
					MgrModules: []string{"dashboard"},
					Config:     map[string]map[string]string{"global": {"mon_max_pg_per_osd": "300"}},
				}},
			}},
		}
	}
	want := structuralJSON(t, storageClusterStructuralHashVars(base(), "ceph"))

	// Reconfigure edits — must not move the structural hash.
	cfg := base()
	cfg.StorageClusters[0].Spec.Ceph.Config["global"]["mon_max_pg_per_osd"] = "400"
	cfg.StorageClusters[0].Spec.Ceph.MgrModules = []string{"dashboard", "balancer"}
	cfg.Machines[0].Spec.Hardware.Management.BMC.Address = "10.9.9.9"
	if got := structuralJSON(t, storageClusterStructuralHashVars(cfg, "ceph")); got != want {
		t.Fatalf("ceph reconfigure/fabric edit must not move the structural hash:\n base=%s\n edit=%s", want, got)
	}

	// Identity edit — the seed host — must move the structural hash.
	id := base()
	id.StorageClusters[0].Spec.Ceph.Cephadm.Bootstrap.Host = "seed-b"
	if got := structuralJSON(t, storageClusterStructuralHashVars(id, "ceph")); got == want {
		t.Fatalf("changing the Ceph bootstrap seed host must move the structural hash")
	}
}

// The machine prepare/finalize tasks must carry a structural projection so a
// reconcilable day-2 edit does not falsely refuse the run as a machine disk wipe.
func TestMachinePrepareFinalizeTasksCarryStructuralHash(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	seen := 0
	for _, task := range tasks {
		if task.Entry.Kind != ApplyTaskKindMachineInfraPrepare && task.Entry.Kind != ApplyTaskKindMachineInfraFinalize {
			continue
		}
		seen++
		hash, err := ApplyTaskStructuralHash(task)
		if err != nil {
			t.Fatalf("structural hash for %s: %v", task.Entry.ID, err)
		}
		if hash == "" {
			t.Fatalf("task %s (%s) has no structural projection; a day-2 edit would falsely refuse as a disk wipe", task.Entry.ID, task.Entry.Kind)
		}
	}
	if seen == 0 {
		t.Fatalf("fixture planned no machine prepare/finalize tasks to assert on")
	}
}
