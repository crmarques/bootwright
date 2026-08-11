package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

func TestKubeVirtSubstrateApplyRequiresExactNonThrowingProbeEvidence(t *testing.T) {
	tasks := readAnsibleTasks(t, kubeVirtSubstrateTasks)
	vmProbeIdx := findAnsibleTask(t, tasks, "Read existing KubeVirt VirtualMachine identity")
	vmClassifyIdx := findAnsibleTask(t, tasks, "Classify existing KubeVirt VirtualMachine probe payload")
	vmConclusiveIdx := findAnsibleTask(t, tasks, "Require a conclusive KubeVirt VirtualMachine probe")
	dvProbeIdx := findAnsibleTask(t, tasks, "Read existing KubeVirt root DataVolume identity")
	dvClassifyIdx := findAnsibleTask(t, tasks, "Classify existing KubeVirt root DataVolume probe payload")
	dvConclusiveIdx := findAnsibleTask(t, tasks, "Require a conclusive KubeVirt root DataVolume probe")
	documentsIdx := findAnsibleTask(t, tasks, "Resolve KubeVirt live resource documents")
	ownershipIdx := findAnsibleTask(t, tasks, "Resolve KubeVirt live resource ownership")
	firstMutationIdx := findAnsibleTask(t, tasks, "Stop KubeVirt VirtualMachine for authorized rebuild")
	if !(vmProbeIdx < vmClassifyIdx && vmClassifyIdx < vmConclusiveIdx &&
		dvProbeIdx < dvClassifyIdx && dvClassifyIdx < dvConclusiveIdx &&
		vmConclusiveIdx < documentsIdx && dvConclusiveIdx < documentsIdx &&
		documentsIdx < ownershipIdx && ownershipIdx < firstMutationIdx) {
		t.Fatalf("KubeVirt substrate apply must safely classify every probe before identity resolution or mutation: vm=%d/%d/%d dv=%d/%d/%d docs=%d ownership=%d mutation=%d", vmProbeIdx, vmClassifyIdx, vmConclusiveIdx, dvProbeIdx, dvClassifyIdx, dvConclusiveIdx, documentsIdx, ownershipIdx, firstMutationIdx)
	}

	requireKubeVirtClassifier(t, tasks[vmClassifyIdx], "bootwright_kubevirt_vm_payload")
	requireKubeVirtClassifier(t, tasks[dvClassifyIdx], "bootwright_kubevirt_root_dv_payload")
	requireKubeVirtProbeVerdict(t, tasks[vmConclusiveIdx], "bootwright_kubevirt_vm_probe", []string{
		"bootwright_kubevirt_vm_payload.valid",
		"kubevirt.io/v1",
		"VirtualMachine",
		"bootwright_kubevirt_vm_name",
		"bootwright_kubevirt_namespace",
		"bootwright_kubevirt_vm_payload.reason",
	})
	requireKubeVirtProbeVerdict(t, tasks[dvConclusiveIdx], "bootwright_kubevirt_root_dv_probe", []string{
		"bootwright_kubevirt_root_dv_payload.valid",
		"cdi.kubevirt.io/v1beta1",
		"DataVolume",
		"bootwright_kubevirt_root_dv_name",
		"bootwright_kubevirt_namespace",
		"bootwright_kubevirt_root_dv_payload.reason",
	})

	documents := fmt.Sprint(tasks[documentsIdx]["ansible.builtin.set_fact"])
	for _, want := range []string{"bootwright_kubevirt_vm_payload.value", "bootwright_kubevirt_root_dv_payload.value"} {
		if !strings.Contains(documents, want) {
			t.Fatalf("validated live document resolution missing %q: %s", want, documents)
		}
	}
	if strings.Contains(readRepoFile(t, kubeVirtSubstrateTasks), "from_json") {
		t.Fatal("KubeVirt substrate apply must not parse a successful live probe through throwing from_json")
	}
}

func TestKubeVirtSubstrateDestroyRequiresNonzeroMissingEvidence(t *testing.T) {
	top := readAnsibleTasks(t, kubeVirtSubstrateDestroyTasks)
	tasks := nestedAnsibleTasks(t, top[findAnsibleTask(t, top, "Tear down KubeVirt guest on the reachable host cluster")], "block")
	requireKubeVirtProbeVerdict(t, tasks[findAnsibleTask(t, tasks, "Require a conclusive KubeVirt VirtualMachine probe")], "bootwright_kubevirt_vm_probe", []string{
		"bootwright_kubevirt_vm_payload.valid",
		"kubevirt.io/v1",
		"VirtualMachine",
		"bootwright_kubevirt_vm_name",
		"bootwright_kubevirt_namespace",
	})
	requireKubeVirtProbeVerdict(t, tasks[findAnsibleTask(t, tasks, "Require conclusive KubeVirt DataVolume probes")], "item", []string{
		"bootwright_kubernetes_object_probe",
		"cdi.kubevirt.io/v1beta1",
		"DataVolume",
		"item.item.name",
		"bootwright_kubevirt_namespace",
	})
	requireKubeVirtProbeVerdict(t, tasks[findAnsibleTask(t, tasks, "Require conclusive KubeVirt PersistentVolumeClaim probes")], "item", []string{
		"bootwright_kubernetes_object_probe",
		"apiVersion', '') == 'v1'",
		"PersistentVolumeClaim",
		"item.item.name",
		"bootwright_kubevirt_namespace",
	})
}

func requireKubeVirtClassifier(t *testing.T, task map[string]any, fact string) {
	t.Helper()
	facts, ok := task["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("KubeVirt probe classifier must be a set_fact, got %v", task)
	}
	if got := fmt.Sprint(facts[fact]); !strings.Contains(got, "bootwright_kubernetes_object_probe") {
		t.Fatalf("KubeVirt probe classifier %s must use the non-throwing object classifier, got %s", fact, got)
	}
}

func requireKubeVirtProbeVerdict(t *testing.T, task map[string]any, rcExpr string, required []string) {
	t.Helper()
	assertion, ok := task["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("KubeVirt probe verdict must be an assert, got %v", task)
	}
	that := fmt.Sprint(assertion["that"])
	for _, want := range append([]string{
		rcExpr + ".rc is defined",
		"(" + rcExpr + ".rc | int) == 0",
		"(" + rcExpr + ".rc | int) != 0",
		"NotFound",
	}, required...) {
		if !strings.Contains(that, want) && !strings.Contains(fmt.Sprint(assertion["fail_msg"]), want) {
			t.Fatalf("KubeVirt probe verdict missing %q: %v", want, assertion)
		}
	}
	notFound := strings.Index(that, "NotFound")
	if notFound < 0 {
		t.Fatalf("KubeVirt probe verdict has no explicit NotFound arm: %s", that)
	}
	branch := that[:notFound]
	if strings.LastIndex(branch, "("+rcExpr+".rc | int) != 0") < strings.LastIndex(branch, "or (") {
		t.Fatalf("KubeVirt NotFound arm is not bound to an explicit nonzero rc, so rc=0 plus misleading stderr could authorize absence: %s", that)
	}
	message := fmt.Sprint(assertion["fail_msg"])
	if !strings.Contains(message, "bootwright_mutating_invocation") {
		t.Fatalf("KubeVirt inconclusive probe refusal must render the exact retry command: %s", message)
	}
}
