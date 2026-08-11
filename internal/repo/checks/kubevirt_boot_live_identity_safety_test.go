package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

func TestKubeVirtBootProbesRequireExactNonThrowingLiveIdentity(t *testing.T) {
	tasks := readAnsibleTasks(t, kubeVirtBootTasks)
	cases := []struct {
		probe      string
		classify   string
		gate       string
		resolve    string
		payload    string
		apiVersion string
		kind       string
	}{
		{probe: "Read KubeVirt boot VirtualMachine identity", classify: "Classify KubeVirt boot VirtualMachine probe", gate: "Require a conclusive KubeVirt boot VirtualMachine probe", resolve: "Resolve KubeVirt boot VirtualMachine identity", payload: "bootwright_kubevirt_boot_vm_payload", apiVersion: "kubevirt.io/v1", kind: "VirtualMachine"},
		{probe: "Read existing KubeVirt agent ISO DataVolume identity", classify: "Classify KubeVirt agent ISO DataVolume probe", gate: "Require a conclusive KubeVirt agent ISO DataVolume probe", resolve: "Resolve KubeVirt agent ISO DataVolume identity", payload: "bootwright_kubevirt_boot_iso_payload", apiVersion: "cdi.kubevirt.io/v1beta1", kind: "DataVolume"},
		{probe: "Read existing KubeVirt agent ISO source DataVolume identity", classify: "Classify KubeVirt agent ISO source DataVolume probe", gate: "Require a conclusive KubeVirt agent ISO source DataVolume probe", resolve: "Resolve KubeVirt agent ISO source DataVolume identity", payload: "bootwright_kubevirt_iso_source_payload", apiVersion: "cdi.kubevirt.io/v1beta1", kind: "DataVolume"},
	}
	for _, tc := range cases {
		t.Run(tc.kind+"_"+tc.payload, func(t *testing.T) {
			probeIdx := findAnsibleTask(t, tasks, tc.probe)
			classifyIdx := findAnsibleTask(t, tasks, tc.classify)
			gateIdx := findAnsibleTask(t, tasks, tc.gate)
			resolveIdx := findAnsibleTask(t, tasks, tc.resolve)
			if !(probeIdx < classifyIdx && classifyIdx < gateIdx && gateIdx < resolveIdx) {
				t.Fatalf("probe must be classified and refused before its live identity is consumed: probe=%d classify=%d gate=%d resolve=%d", probeIdx, classifyIdx, gateIdx, resolveIdx)
			}
			classifier := fmt.Sprint(tasks[classifyIdx]["ansible.builtin.set_fact"])
			if !strings.Contains(classifier, "bootwright.core.bootwright_kubernetes_object_probe") || !strings.Contains(classifier, tc.payload) {
				t.Fatalf("successful probe must use the non-throwing object classifier: %s", classifier)
			}
			gate := fmt.Sprint(tasks[gateIdx]["ansible.builtin.assert"])
			for _, want := range []string{"rc is defined", "| int) == 0", "| int) != 0", tc.payload + ".valid", tc.apiVersion, tc.kind, "metadata", "name", "namespace", "NotFound", "bootwright_mutating_invocation"} {
				if !strings.Contains(gate, want) {
					t.Fatalf("conclusive live probe gate missing %q: %s", want, gate)
				}
			}
			resolved := fmt.Sprint(tasks[resolveIdx]["ansible.builtin.set_fact"])
			if strings.Contains(resolved, "from_json") || !strings.Contains(resolved, tc.payload+".value") {
				t.Fatalf("live identity must consume only validated classifier output: %s", resolved)
			}
		})
	}
}

func TestKubeVirtLegacyMediaAdoptionRequiresSafeExactMachineRecord(t *testing.T) {
	tasks := readAnsibleTasks(t, kubeVirtBootTasks)
	statIdx := findAnsibleTask(t, tasks, "Inspect KubeVirt machine ownership record")
	readIdx := findAnsibleTask(t, tasks, "Read KubeVirt machine ownership record")
	guardIdx := findAnsibleTask(t, tasks, "Require safe readable KubeVirt machine ownership evidence")
	decodeIdx := findAnsibleTask(t, tasks, "Decode KubeVirt machine ownership record")
	verifyIdx := findAnsibleTask(t, tasks, "Verify KubeVirt machine ownership record")
	adoptIdx := findAnsibleTask(t, tasks, "Resolve legacy KubeVirt agent ISO DataVolume ownership")
	removeIdx := findAnsibleTask(t, tasks, "Remove previous KubeVirt agent ISO DataVolume")
	if !(statIdx < readIdx && readIdx < guardIdx && guardIdx < decodeIdx && decodeIdx < verifyIdx && verifyIdx < adoptIdx && adoptIdx < removeIdx) {
		t.Fatalf("legacy media authority must be read safely and verified before adoption or deletion: stat=%d read=%d guard=%d decode=%d verify=%d adopt=%d remove=%d", statIdx, readIdx, guardIdx, decodeIdx, verifyIdx, adoptIdx, removeIdx)
	}
	stat := fmt.Sprint(tasks[statIdx]["ansible.builtin.stat"])
	if !strings.Contains(stat, "follow:false") && !strings.Contains(stat, "follow false") {
		t.Fatalf("machine record stat must not follow symlinks: %s", stat)
	}
	if tasks[statIdx]["failed_when"] != false || tasks[readIdx]["failed_when"] != false {
		t.Fatalf("suppressed record probes must defer to the actionable success-only guard: stat=%v read=%v", tasks[statIdx], tasks[readIdx])
	}
	guard := fmt.Sprint(tasks[guardIdx]["ansible.builtin.assert"])
	for _, want := range []string{"stat.exists is defined", "stat.isreg", "stat.islnk", "content is defined", "regular non-symlink", "bootwright_mutating_invocation"} {
		if !strings.Contains(guard, want) {
			t.Fatalf("machine record path/read guard missing %q: %s", want, guard)
		}
	}
	decode := tasks[decodeIdx]
	rescue := nestedAnsibleTasks(t, decode, "rescue")
	if message := ansibleFailureMessage(t, rescue[findAnsibleTask(t, rescue, "Refuse malformed KubeVirt machine ownership evidence")]); !strings.Contains(message, "bootwright_mutating_invocation") {
		t.Fatalf("malformed ownership refusal must name the exact retry: %s", message)
	}
	verify := fmt.Sprint(tasks[verifyIdx]["ansible.builtin.set_fact"])
	for _, want := range []string{"bootwright.io/ownership/v1alpha1", "kubevirt-machine", "bootwright", "role", "context", "host", "provider", "cluster", "machine", "namespace", "virtualMachine", "rootDataVolume"} {
		if !strings.Contains(verify, want) {
			t.Fatalf("legacy ownership identity missing %q: %s", want, verify)
		}
	}
}
