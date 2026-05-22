package cli

import "testing"

func TestPlanApplyTasksBuildsDependencies(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	tasks := planApplyTasks(allScope, state)
	if len(tasks) != 5 {
		t.Fatalf("planned %d tasks, want 5: %+v", len(tasks), tasks)
	}
	if tasks[0].entry.ID != "provider" {
		t.Fatalf("first task = %s, want provider", tasks[0].entry.ID)
	}
	if tasks[1].entry.ID != "infra.sno-libvirt" {
		t.Fatalf("second task = %s, want infra.sno-libvirt", tasks[1].entry.ID)
	}
	if len(tasks[1].entry.Dependencies) != 1 || tasks[1].entry.Dependencies[0] != "provider" {
		t.Fatalf("infra deps = %v, want provider", tasks[1].entry.Dependencies)
	}
	if tasks[2].entry.ID != "iso.sno-libvirt" {
		t.Fatalf("third task = %s, want iso.sno-libvirt", tasks[2].entry.ID)
	}
	if len(tasks[2].entry.Dependencies) != 1 || tasks[2].entry.Dependencies[0] != "infra.sno-libvirt" {
		t.Fatalf("iso deps = %v, want infra.sno-libvirt", tasks[2].entry.Dependencies)
	}
	if tasks[3].entry.ID != "boot.sno-libvirt.master-0" {
		t.Fatalf("fourth task = %s, want boot.sno-libvirt.master-0", tasks[3].entry.ID)
	}
	if len(tasks[3].entry.Dependencies) != 1 || tasks[3].entry.Dependencies[0] != "iso.sno-libvirt" {
		t.Fatalf("boot deps = %v, want iso.sno-libvirt", tasks[3].entry.Dependencies)
	}
	if tasks[4].entry.ID != "wait.sno-libvirt" {
		t.Fatalf("fifth task = %s, want wait.sno-libvirt", tasks[4].entry.ID)
	}
	if len(tasks[4].entry.Dependencies) != 1 || tasks[4].entry.Dependencies[0] != "boot.sno-libvirt.master-0" {
		t.Fatalf("wait deps = %v, want boot.sno-libvirt.master-0", tasks[4].entry.Dependencies)
	}
}

func TestPlanApplyTasksClusterScopeHasIndependentInstallTask(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	tasks := planApplyTasks(clusterScope, state)
	if len(tasks) != 3 {
		t.Fatalf("planned %d tasks, want 3: %+v", len(tasks), tasks)
	}
	if tasks[0].entry.ID != "iso.sno-libvirt" {
		t.Fatalf("task = %s, want iso.sno-libvirt", tasks[0].entry.ID)
	}
	if len(tasks[0].entry.Dependencies) != 0 {
		t.Fatalf("cluster-only iso deps = %v, want none", tasks[0].entry.Dependencies)
	}
	if tasks[1].entry.ID != "boot.sno-libvirt.master-0" {
		t.Fatalf("task = %s, want boot.sno-libvirt.master-0", tasks[1].entry.ID)
	}
	if len(tasks[1].entry.Dependencies) != 1 || tasks[1].entry.Dependencies[0] != "iso.sno-libvirt" {
		t.Fatalf("boot deps = %v, want iso.sno-libvirt", tasks[1].entry.Dependencies)
	}
	if tasks[2].entry.ID != "wait.sno-libvirt" {
		t.Fatalf("task = %s, want wait.sno-libvirt", tasks[2].entry.ID)
	}
	if len(tasks[2].entry.Dependencies) != 1 || tasks[2].entry.Dependencies[0] != "boot.sno-libvirt.master-0" {
		t.Fatalf("wait deps = %v, want boot.sno-libvirt.master-0", tasks[2].entry.Dependencies)
	}
}

func TestPlanApplyTasksBootsAllClusterMachinesBeforeWait(t *testing.T) {
	state := loadFixtureState(t, "005-3nodes-baremetal")
	tasks := planApplyTasks(clusterScope, state)
	if len(tasks) != 5 {
		t.Fatalf("planned %d tasks, want 5: %+v", len(tasks), tasks)
	}
	wantBootIDs := []string{
		"boot.3-nodes-ocp-baremetal.master-0",
		"boot.3-nodes-ocp-baremetal.master-1",
		"boot.3-nodes-ocp-baremetal.master-2",
	}
	for i, want := range wantBootIDs {
		task := tasks[i+1]
		if task.entry.ID != want {
			t.Fatalf("boot task %d = %s, want %s", i, task.entry.ID, want)
		}
		if task.entry.Kind != applyTaskKindNodeBoot {
			t.Fatalf("boot task kind = %s, want %s", task.entry.Kind, applyTaskKindNodeBoot)
		}
		if len(task.entry.Dependencies) != 1 || task.entry.Dependencies[0] != "iso.3-nodes-ocp-baremetal" {
			t.Fatalf("boot deps = %v, want iso.3-nodes-ocp-baremetal", task.entry.Dependencies)
		}
	}
	wait := tasks[4]
	if wait.entry.ID != "wait.3-nodes-ocp-baremetal" {
		t.Fatalf("wait task = %s, want wait.3-nodes-ocp-baremetal", wait.entry.ID)
	}
	if len(wait.entry.Dependencies) != len(wantBootIDs) {
		t.Fatalf("wait deps = %v, want %v", wait.entry.Dependencies, wantBootIDs)
	}
	for i, want := range wantBootIDs {
		if wait.entry.Dependencies[i] != want {
			t.Fatalf("wait dep %d = %s, want %s", i, wait.entry.Dependencies[i], want)
		}
	}
}
