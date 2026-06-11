package repocheck

import (
	"strings"
	"testing"
)

const vsphereSubstrateTaskRoot = "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks"

// TestVSphereSubstrateDestroyRequiresOwnershipMarker pins the destroy
// safety contract: a VM addressed by computed name is deleted only after
// its annotation proves the Bootwright ownership marker for this context,
// cluster, and machine — mirroring the libvirt domain-XML marker check.
func TestVSphereSubstrateDestroyRequiresOwnershipMarker(t *testing.T) {
	tasks := readAnsibleTasks(t, vsphereSubstrateTaskRoot+"/destroy.yml")
	assertIdx := findAnsibleTask(t, tasks, "Refuse to delete a non-Bootwright vSphere VM")
	deleteIdx := findAnsibleTask(t, tasks, "Delete vSphere VM")
	if assertIdx >= deleteIdx {
		t.Fatalf("ownership assertion (task %d) must run before VM deletion (task %d)", assertIdx, deleteIdx)
	}
	body := readRepoFile(t, vsphereSubstrateTaskRoot+"/destroy.yml")
	for _, marker := range []string{"bootwright:context=", "bootwright:cluster=", "bootwright:machine="} {
		if !strings.Contains(body, marker) {
			t.Fatalf("vsphere destroy ownership assertion missing marker %q", marker)
		}
	}
	deleteTask := tasks[deleteIdx]
	when, _ := deleteTask["when"].(string)
	if !strings.Contains(when, "bootwright_vsphere_vm_present") {
		t.Fatalf("Delete vSphere VM must be gated on VM presence so destroy stays idempotent, got when=%v", deleteTask["when"])
	}
}

// TestVSphereSubstrateRecordsOwnershipAttributesAtCreate pins the
// recorded-before-rename contract: the apply path records the vCenter
// identity and ISO staging attributes the destroy path reads back.
func TestVSphereSubstrateRecordsOwnershipAttributesAtCreate(t *testing.T) {
	body := readRepoFile(t, vsphereSubstrateTaskRoot+"/main.yml")
	if !strings.Contains(body, "bootwright_ownership_kind: vsphere-machine") {
		t.Fatal("vsphere apply must record a vsphere-machine ownership resource")
	}
	for _, attr := range []string{"vmName:", "moid:", "uuid:", "isoDatastore:", "isoFolder:"} {
		if !strings.Contains(body, attr) {
			t.Fatalf("vsphere apply ownership attributes missing %q", attr)
		}
	}
	destroy := readRepoFile(t, vsphereSubstrateTaskRoot+"/destroy.yml")
	if !strings.Contains(destroy, "vsphere-vmedia") {
		t.Fatal("vsphere destroy must clean recorded vsphere-vmedia resources")
	}
}

// TestVSphereTasksPinVenvInterpreterAndRedactCredentials pins two
// cross-cutting contracts on every community.vmware task: the module must
// import pyvmomi from the interpreter that runs ansible-playbook (the
// managed venv) instead of the discovered system python, and tasks that
// carry vCenter credentials must not log them.
func TestVSphereTasksPinVenvInterpreterAndRedactCredentials(t *testing.T) {
	for _, rel := range []string{
		vsphereSubstrateTaskRoot + "/main.yml",
		vsphereSubstrateTaskRoot + "/destroy.yml",
	} {
		tasks := readAnsibleTasks(t, rel)
		for _, task := range tasks {
			module := ""
			for key := range task {
				if strings.HasPrefix(key, "community.vmware.") {
					module = key
				}
			}
			if module == "" {
				continue
			}
			name, _ := task["name"].(string)
			vars, _ := task["vars"].(map[string]any)
			interpreter, _ := vars["ansible_python_interpreter"].(string)
			if !strings.Contains(interpreter, "bootwright_vsphere_python") {
				t.Fatalf("%s: task %q (%s) must pin ansible_python_interpreter to the managed venv via bootwright_vsphere_python", rel, name, module)
			}
			if noLog, _ := task["no_log"].(bool); !noLog {
				t.Fatalf("%s: task %q (%s) passes vCenter credentials and must set no_log: true", rel, name, module)
			}
		}
	}
}
