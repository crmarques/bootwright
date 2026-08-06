package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

const (
	powerGateRedfishMain  = "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/main.yml"
	powerGateRedfishTasks = "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/validation/power.yml"
	powerGateVsphereMain  = "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/main.yml"
	powerGateVsphereTasks = "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/power_gate.yml"
	powerGateKubevirtMain = "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml"
)

func TestRedfishBootRequiresPoweredOffMachine(t *testing.T) {
	root := readAnsibleTasks(t, powerGateRedfishMain)
	tasks := nestedAnsibleTasks(t, root[0], "block")

	gate := findAnsibleTask(t, tasks, "Require a powered-off machine before installer boot")
	assertIncludeTasksFile(t, tasks[gate], "validation/power.yml")
	when := fmt.Sprint(tasks[gate]["when"])
	for _, want := range []string{"bootwright_vmedia_action_effective == 'boot'", "bootwright_component.osManaged | default(true) | bool"} {
		if !strings.Contains(when, want) {
			t.Fatalf("redfish power gate when %q must contain %q", when, want)
		}
	}
	if strings.Contains(when, "setBootSource") || strings.Contains(when, "reinstall") {
		t.Fatalf("redfish power gate must apply to every installer boot with no setBootSource or reinstall escape, got when %q", when)
	}
	prepare := findAnsibleTask(t, tasks, "Prepare Redfish virtual media")
	boot := findAnsibleTask(t, tasks, "Boot node from Redfish virtual media")
	if !(gate < prepare && prepare < boot) {
		t.Fatalf("redfish power gate must run before media prepare and boot, got gate=%d prepare=%d boot=%d", gate, prepare, boot)
	}
	if idx := findAnsibleTaskIndex(tasks, "Guard against wiping an occupied host"); idx >= 0 {
		t.Fatalf("the BootProgress occupancy guard is superseded by the power-off gate and must stay removed")
	}

	gateTasks := readAnsibleTasks(t, powerGateRedfishTasks)
	probe := findAnsibleTask(t, gateTasks, "Query Redfish system power state before installer boot")
	if failedWhen, ok := gateTasks[probe]["failed_when"].(bool); !ok || failedWhen {
		t.Fatalf("redfish power probe must tolerate probe errors and leave the verdict to the assert, got failed_when=%v", gateTasks[probe]["failed_when"])
	}
	assertRedactsByDefault(t, "redfish power probe", gateTasks[probe]["no_log"])
	refuse := findAnsibleTask(t, gateTasks, "Refuse to boot install media on a machine that is not powered off")
	assertPowerGateRefusal(t, powerGateRedfishTasks, gateTasks[refuse], []string{"== 200", "== 'Off'"})
}

func TestVsphereBootRequiresPoweredOffMachine(t *testing.T) {
	tasks := readAnsibleTasks(t, powerGateVsphereMain)
	boot := findAnsibleTask(t, tasks, "Boot machine from vSphere virtual media")
	block := nestedAnsibleTasks(t, tasks[boot], "block")

	gate := findAnsibleTask(t, block, "Require a powered-off machine before installer boot")
	assertIncludeTasksFile(t, block[gate], "power_gate.yml")
	if when := fmt.Sprint(block[gate]["when"]); !strings.Contains(when, "bootwright_component.osManaged | default(true) | bool") {
		t.Fatalf("vsphere power gate when %q must key off osManaged with a managed default", when)
	}
	stage := findAnsibleTask(t, block, "Stage generated agent ISO")
	if gate >= stage {
		t.Fatalf("vsphere power gate must run before any media staging, got gate=%d stage=%d", gate, stage)
	}

	gateTasks := readAnsibleTasks(t, powerGateVsphereTasks)
	probe := findAnsibleTask(t, gateTasks, "Query vSphere machine power state before installer boot")
	if failedWhen, ok := gateTasks[probe]["failed_when"].(bool); !ok || failedWhen {
		t.Fatalf("vsphere power probe must tolerate probe errors and leave the verdict to the assert, got failed_when=%v", gateTasks[probe]["failed_when"])
	}
	refuse := findAnsibleTask(t, gateTasks, "Refuse to boot install media on a machine that is not powered off")
	assertPowerGateRefusal(t, powerGateVsphereTasks, gateTasks[refuse], []string{"== 'poweredOff'"})
}

func TestKubevirtBootRequiresPoweredOffMachine(t *testing.T) {
	tasks := readAnsibleTasks(t, powerGateKubevirtMain)

	probe := findAnsibleTask(t, tasks, "Query KubeVirt VirtualMachineInstance before installer boot")
	verdict := findAnsibleTask(t, tasks, "Resolve KubeVirt machine power gate verdict")
	refuse := findAnsibleTask(t, tasks, "Refuse to boot install media on a machine that is not powered off")
	stop := findAnsibleTask(t, tasks, "Stop KubeVirt VirtualMachine before replacing the agent ISO")
	if !(probe < verdict && verdict < refuse && refuse < stop) {
		t.Fatalf("kubevirt power gate must refuse before the role stops the VM, got probe=%d verdict=%d refuse=%d stop=%d", probe, verdict, refuse, stop)
	}
	for _, mutation := range []string{
		"Upgrade legacy KubeVirt agent ISO DataVolume ownership labels",
		"Remove stale KubeVirt agent ISO source DataVolume",
		"Upload KubeVirt agent ISO source DataVolume",
	} {
		if idx := findAnsibleTask(t, tasks, mutation); refuse >= idx {
			t.Fatalf("kubevirt power gate must refuse before %q, or re-applying a running cluster rebuilds shared media it then refuses to use, got refuse=%d mutation=%d", mutation, refuse, idx)
		}
	}
	wait := findAnsibleTask(t, tasks, "Wait for the shared KubeVirt agent ISO source DataVolume")
	if until := fmt.Sprint(tasks[wait]["until"]); !strings.Contains(until, "bootwright_kubevirt_iso_generation") {
		t.Fatalf("the peer wait must give up on a source stuck at another generation, or a refusing elected machine strands its powered-off peers for the full media budget, got until=%q", until)
	}
	for _, idx := range []int{probe, verdict, refuse} {
		if when := fmt.Sprint(tasks[idx]["when"]); !strings.Contains(when, "bootwright_component.osManaged | default(true) | bool") {
			t.Fatalf("kubevirt power gate task %q when %q must key off osManaged with a managed default", tasks[idx]["name"], when)
		}
	}
	verdictExpr := fmt.Sprint(tasks[verdict]["ansible.builtin.set_fact"])
	for _, want := range []string{"!= 0", "'not found' in"} {
		if !strings.Contains(verdictExpr, want) {
			t.Fatalf("kubevirt power verdict %q must accept only a proven-absent VMI, missing %q", verdictExpr, want)
		}
	}
	assertPowerGateRefusal(t, powerGateKubevirtMain, tasks[refuse], []string{"bootwright_kubevirt_power_gate_off | bool"})
}

func assertPowerGateRefusal(t *testing.T, rel string, task map[string]any, conditions []string) {
	t.Helper()
	assertBody, ok := task["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s refusal must be an ansible.builtin.assert", rel)
	}
	that := fmt.Sprint(assertBody["that"])
	for _, want := range conditions {
		if !strings.Contains(that, want) {
			t.Fatalf("%s refusal condition %q must contain %q", rel, that, want)
		}
	}
	failMsg := fmt.Sprint(assertBody["fail_msg"])
	for _, want := range []string{
		"refusing to boot install media",
		"disk-wipes",
		"no",
		"--authorize token stands in for it",
		"fails",
		"closed",
		"`bootwright apply`",
		"interrupted while",
		"loses nothing",
	} {
		if !strings.Contains(failMsg, want) {
			t.Fatalf("%s refusal message must contain %q, got %q", rel, want, failMsg)
		}
	}
}
