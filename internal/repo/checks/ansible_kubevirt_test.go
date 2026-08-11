package repocheck

import (
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/storage/topology"
	"go.yaml.in/yaml/v3"
)

const kubeVirtBootTasks = "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml"

const kubeVirtSubstrateTasks = "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml"

const kubeVirtSubstrateDestroyTasks = "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/destroy.yml"

const kubeVirtPurgeMediaPVCTasks = "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/purge_media_pvc.yml"

func TestKubeVirtBootUploadsSharedAgentISOOncePerCluster(t *testing.T) {
	tasks := readAnsibleTasks(t, kubeVirtBootTasks)
	groupIdx := findAnsibleTask(t, tasks, "Resolve KubeVirt agent ISO sharing group")
	electIdx := findAnsibleTask(t, tasks, "Resolve KubeVirt agent ISO source election")
	probeIdx := findAnsibleTask(t, tasks, "Read existing KubeVirt agent ISO source DataVolume identity")
	conclusiveIdx := findAnsibleTask(t, tasks, "Require a conclusive KubeVirt agent ISO source DataVolume probe")
	ownedIdx := findAnsibleTask(t, tasks, "Resolve KubeVirt agent ISO source DataVolume ownership")
	currentIdx := findAnsibleTask(t, tasks, "Resolve KubeVirt agent ISO source DataVolume currency")
	gateIdx := findAnsibleTask(t, tasks, "Enforce KubeVirt agent ISO source DataVolume apply mode")
	staleIdx := findAnsibleTask(t, tasks, "Remove stale KubeVirt agent ISO source DataVolume")
	sourceUploadIdx := findAnsibleTask(t, tasks, "Upload KubeVirt agent ISO source DataVolume")
	sourceLabelIdx := findAnsibleTask(t, tasks, "Label KubeVirt agent ISO source DataVolume as Bootwright-managed")
	waitIdx := findAnsibleTask(t, tasks, "Wait for the shared KubeVirt agent ISO source DataVolume")
	readyIdx := findAnsibleTask(t, tasks, "Resolve KubeVirt agent ISO source DataVolume readiness")
	deleteIdx := findAnsibleTask(t, tasks, "Remove previous KubeVirt agent ISO DataVolume")
	cloneIdx := findAnsibleTask(t, tasks, "Clone KubeVirt agent ISO DataVolume from the shared source")
	clonePhaseIdx := findAnsibleTask(t, tasks, "Wait for the KubeVirt agent ISO DataVolume clone")
	cloneOutcomeIdx := findAnsibleTask(t, tasks, "Resolve KubeVirt agent ISO DataVolume clone outcome")
	cloneCleanupIdx := findAnsibleTask(t, tasks, "Remove the failed KubeVirt agent ISO DataVolume clone")
	uploadIdx := findAnsibleTask(t, tasks, "Upload KubeVirt agent ISO DataVolume")
	if !(groupIdx < electIdx && electIdx < probeIdx && probeIdx < conclusiveIdx &&
		conclusiveIdx < ownedIdx && ownedIdx < currentIdx && currentIdx < gateIdx &&
		gateIdx < staleIdx && staleIdx < sourceUploadIdx && sourceUploadIdx < sourceLabelIdx &&
		sourceLabelIdx < waitIdx && waitIdx < readyIdx && readyIdx < deleteIdx &&
		deleteIdx < cloneIdx && cloneIdx < clonePhaseIdx && clonePhaseIdx < cloneOutcomeIdx &&
		cloneOutcomeIdx < cloneCleanupIdx && cloneCleanupIdx < uploadIdx) {
		t.Fatalf("shared agent ISO media must be elected, probed, gated, uploaded and labelled before any clone or per-machine upload (group=%d elect=%d probe=%d conclusive=%d owned=%d current=%d gate=%d stale=%d sourceUpload=%d sourceLabel=%d wait=%d ready=%d delete=%d clone=%d clonePhase=%d cloneOutcome=%d cloneCleanup=%d upload=%d)", groupIdx, electIdx, probeIdx, conclusiveIdx, ownedIdx, currentIdx, gateIdx, staleIdx, sourceUploadIdx, sourceLabelIdx, waitIdx, readyIdx, deleteIdx, cloneIdx, clonePhaseIdx, cloneOutcomeIdx, cloneCleanupIdx, uploadIdx)
	}

	for _, task := range tasks {
		if _, ok := task["run_once"]; ok {
			t.Fatalf("%v: run_once elects the play's first host, but shared agent ISO media is elected per (bootApplyRole, kubeconfig, namespace) sharing group, so single-actor election must come from the cluster machine list", task["name"])
		}
	}

	group, ok := tasks[groupIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("agent ISO sharing group must be a set_fact, got %v", tasks[groupIdx])
	}
	machines := fmt.Sprint(group["bootwright_kubevirt_iso_source_machines"])
	for _, want := range []string{
		"bootwright_component.bootApplyRole",
		"kubevirt.kubeconfig",
		"kubevirt.namespace",
		"| sort",
	} {
		if !strings.Contains(machines, want) {
			t.Fatalf("agent ISO sharing group must be the deterministically sorted set of machines sharing this boot role, kubeconfig and namespace (missing %q): %v", want, group["bootwright_kubevirt_iso_source_machines"])
		}
	}
	for _, guard := range []string{"'bootApplyRole', 'defined'", "'kubevirt', 'defined'", "'kubevirt.kubeconfig', 'defined'", "'kubevirt.namespace', 'defined'"} {
		if !strings.Contains(machines, guard) {
			t.Fatalf("agent ISO sharing group must guard %s before matching it, or Jinja raises on machines without the key: %v", guard, group["bootwright_kubevirt_iso_source_machines"])
		}
	}

	elect, ok := tasks[electIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("agent ISO source election must be a set_fact, got %v", tasks[electIdx])
	}
	actor := fmt.Sprint(elect["bootwright_kubevirt_iso_source_actor"])
	if !strings.Contains(actor, "| first") || !strings.Contains(actor, "bootwright_component.name") {
		t.Fatalf("exactly the first machine of the sharing group may upload the shared media, got %v", elect["bootwright_kubevirt_iso_source_actor"])
	}
	if got := fmt.Sprint(elect["bootwright_kubevirt_iso_source_shared"]); !strings.Contains(got, "> 1") {
		t.Fatalf("a single-machine cluster must keep the direct upload instead of paying for a shared source, got %v", elect["bootwright_kubevirt_iso_source_shared"])
	}
	signature := fmt.Sprint(elect["bootwright_kubevirt_iso_source_signature"])
	for _, want := range []string{"bootwright|", "agent-iso-source", "bootwright_kubevirt_iso_generation", "Succeeded"} {
		if !strings.Contains(signature, want) {
			t.Fatalf("shared media signature must pin owner, role, generation and phase (missing %q): %v", want, elect["bootwright_kubevirt_iso_source_signature"])
		}
	}

	conclusive, ok := tasks[conclusiveIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("shared media probe verdict must be an assert, got %v", tasks[conclusiveIdx])
	}
	if got := fmt.Sprint(conclusive["that"]); !strings.Contains(got, "NotFound") || !strings.Contains(got, "rc is defined") || !strings.Contains(got, "| int) == 0") || !strings.Contains(got, "| int) != 0") {
		t.Fatalf("shared media probe must refuse on an unprobeable result instead of treating it as absent, got %v", conclusive["that"])
	}

	owned, ok := tasks[ownedIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("shared media ownership must be a set_fact, got %v", tasks[ownedIdx])
	}
	ownedExpr := fmt.Sprint(owned["bootwright_kubevirt_iso_source_owned"])
	for _, want := range []string{
		"bootwright.io/managed-by",
		"bootwright.io/context",
		"bootwright.io/cluster",
		"'agent-iso-source'",
		"bootwright.io/node' not in",
	} {
		if !strings.Contains(ownedExpr, want) {
			t.Fatalf("shared media ownership must accept exactly the node-less source label shape (missing %q): %v", want, owned["bootwright_kubevirt_iso_source_owned"])
		}
	}

	current, ok := tasks[currentIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("shared media currency must be a set_fact, got %v", tasks[currentIdx])
	}
	currentExpr := fmt.Sprint(current["bootwright_kubevirt_iso_source_current"])
	for _, want := range []string{"bootwright_kubevirt_iso_source_owned", "bootwright.io/agent-iso-generation", "'Succeeded'"} {
		if !strings.Contains(currentExpr, want) {
			t.Fatalf("shared media may only be reused when it is owned, carries this run's generation and imported successfully (missing %q): %v", want, current["bootwright_kubevirt_iso_source_current"])
		}
	}

	include, ok := tasks[gateIdx]["ansible.builtin.include_role"].(map[string]any)
	if !ok || fmt.Sprint(include["name"]) != "bootwright.core.ownership_record" || fmt.Sprint(include["tasks_from"]) != "apply_mode_gate.yml" {
		t.Fatalf("shared media gate must use the shared apply-mode contract, got %v", tasks[gateIdx])
	}
	for _, idx := range []int{gateIdx, staleIdx, sourceUploadIdx, sourceLabelIdx} {
		if got := fmt.Sprint(tasks[idx]["when"]); !strings.Contains(got, "bootwright_kubevirt_iso_source_actor") {
			t.Fatalf("%v must run only for the elected uploader, got when=%v", tasks[idx]["name"], tasks[idx]["when"])
		}
	}
	for _, idx := range []int{staleIdx, sourceUploadIdx, sourceLabelIdx} {
		if got := fmt.Sprint(tasks[idx]["when"]); !strings.Contains(got, "bootwright_kubevirt_iso_source_current") {
			t.Fatalf("%v must be skipped when the shared media already holds this run's ISO, got when=%v", tasks[idx]["name"], tasks[idx]["when"])
		}
	}

	sourceUploadEnv := fmt.Sprint(tasks[sourceUploadIdx]["environment"])
	if !strings.Contains(sourceUploadEnv, "SSL_CERT_FILE") || !strings.Contains(sourceUploadEnv, "bootwright_proxy_env") {
		t.Fatalf("shared media upload must keep the managed-host ingress trust and proxy env of the per-machine upload, got environment=%v", tasks[sourceUploadIdx]["environment"])
	}
	assertRedactsByDefault(t, "Upload KubeVirt agent ISO source DataVolume", tasks[sourceUploadIdx]["no_log"])

	sourceLabel := fmt.Sprint(tasks[sourceLabelIdx]["ansible.builtin.command"])
	for _, want := range []string{
		"bootwright.io/managed-by=bootwright",
		"bootwright.io/context=",
		"bootwright.io/cluster=",
		"bootwright.io/role=agent-iso-source",
		"bootwright.io/agent-iso-generation=",
		"--overwrite",
	} {
		if !strings.Contains(sourceLabel, want) {
			t.Fatalf("shared media must be labelled %q so the destroy-side ownership gate can release it, got %v", want, tasks[sourceLabelIdx])
		}
	}
	if strings.Contains(sourceLabel, "bootwright.io/node=") {
		t.Fatalf("shared media is cluster-scoped and must carry no per-node label, got %v", tasks[sourceLabelIdx])
	}

	waitUntil := fmt.Sprint(tasks[waitIdx]["until"])
	if !strings.Contains(waitUntil, "bootwright_kubevirt_iso_source_signature") {
		t.Fatalf("a non-electing machine must wait for the exact shared-media signature, never merely for the object to exist, got until=%v", tasks[waitIdx]["until"])
	}
	if !strings.Contains(waitUntil, "bootwright_kubevirt_iso_source_failed_signature") {
		t.Fatalf("a non-electing machine must stop waiting the moment the shared media fails at this generation instead of polling out its whole budget, got until=%v", tasks[waitIdx]["until"])
	}
	if got := fmt.Sprint(elect["bootwright_kubevirt_iso_source_failed_signature"]); !strings.Contains(got, "agent-iso-source") || !strings.Contains(got, "Failed") {
		t.Fatalf("the failed shared-media signature must pin the same role and generation as the succeeded one, got %v", elect["bootwright_kubevirt_iso_source_failed_signature"])
	}
	if !strings.Contains(waitUntil, "bootwright_kubevirt_iso_source_appear_retries") {
		t.Fatalf("a non-electing machine must give up on shared media that never appears at all, or an elected uploader that dies before creating the DataVolume idles every peer for the whole upload budget, got until=%v", tasks[waitIdx]["until"])
	}
	if tasks[waitIdx]["failed_when"] != false {
		t.Fatalf("the shared-media wait must degrade to the direct upload instead of failing the boot, got failed_when=%v", tasks[waitIdx]["failed_when"])
	}
	if got := fmt.Sprint(tasks[waitIdx]["when"]); !strings.Contains(got, "not (bootwright_kubevirt_iso_source_actor") {
		t.Fatalf("only a non-electing machine may wait for the shared media, got when=%v", tasks[waitIdx]["when"])
	}

	cloneBody, ok := tasks[cloneIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("agent ISO clone must be a command task, got %v", tasks[cloneIdx])
	}
	if got := fmt.Sprint(cloneBody["stdin"]); !strings.Contains(got, "datavolume-agent-iso.yaml.j2") {
		t.Fatalf("agent ISO clone must apply the per-VM clone DataVolume template, got %v", cloneBody["stdin"])
	}
	for _, idx := range []int{cloneIdx, clonePhaseIdx} {
		if got := fmt.Sprint(tasks[idx]["when"]); !strings.Contains(got, "bootwright_kubevirt_iso_source_ready") {
			t.Fatalf("%v must run only once the shared media is proven ready, got when=%v", tasks[idx]["name"], tasks[idx]["when"])
		}
	}
	cloneUntil := fmt.Sprint(tasks[clonePhaseIdx]["until"])
	if !strings.Contains(cloneUntil, "'Succeeded'") || !strings.Contains(cloneUntil, "'Failed'") {
		t.Fatalf("the clone wait must settle on either terminal phase instead of burning the whole timeout, got until=%v", tasks[clonePhaseIdx]["until"])
	}
	if !strings.Contains(cloneUntil, "bootwright_kubevirt_iso_clone_start_retries") {
		t.Fatalf("CDI leaves a clone its storage refused at CloneInProgress forever and never marks it Failed, so the wait must conclude at a start deadline once the transfer has provably not begun instead of polling out the whole budget, got until=%v", tasks[clonePhaseIdx]["until"])
	}
	if !strings.Contains(cloneUntil, "NotFound") {
		t.Fatalf("one transient kubectl error must not abandon a healthy clone: only a conclusive NotFound may end the poll early, as the sibling polls already require, got until=%v", tasks[clonePhaseIdx]["until"])
	}
	if !strings.Contains(cloneUntil, "bootwright_kubevirt_iso_clone_retries") {
		t.Fatalf("the clone wait must keep the house attempts escape so an exhausted budget lands on the fallback instead of failing the boot, got until=%v", tasks[clonePhaseIdx]["until"])
	}
	clonePoll := fmt.Sprint(tasks[clonePhaseIdx]["ansible.builtin.command"])
	for _, want := range []string{".status.phase", ".status.progress", "Running"} {
		if !strings.Contains(clonePoll, want) {
			t.Fatalf("the clone wait must read %q so an exhausted budget can say whether the clone was moving or stuck, got %v", want, tasks[clonePhaseIdx]["ansible.builtin.command"])
		}
	}
	cloneReport := fmt.Sprint(tasks[findAnsibleTask(t, tasks, "Report KubeVirt agent ISO clone fallback to direct upload")]["ansible.builtin.debug"])
	if !strings.Contains(cloneReport, "progress") {
		t.Fatalf("the clone fallback report must carry the observed progress, or the retry budget can only be tuned by guesswork, got %v", cloneReport)
	}
	if !strings.Contains(cloneReport, "attempts") {
		t.Fatalf("the clone fallback report must time itself from the poll's own attempts, or every exit prints the full budget and a wait that concluded early reads as one that ran to exhaustion, got %v", cloneReport)
	}
	if strings.Contains(cloneReport, "(bootwright_kubevirt_iso_clone_retries | int) - 1") {
		t.Fatalf("the clone fallback report must not compute its elapsed time from the configured budget, which is a constant on every path including an immediate exit, got %v", cloneReport)
	}
	eventsIdx := findAnsibleTask(t, tasks, "Read the KubeVirt agent ISO clone events")
	if !(cloneOutcomeIdx < eventsIdx && eventsIdx < cloneCleanupIdx) {
		t.Fatalf("the clone events must be read after the outcome is known and before the DataVolume is deleted, or the only object naming the reason is destroyed unread (outcome=%d events=%d cleanup=%d)", cloneOutcomeIdx, eventsIdx, cloneCleanupIdx)
	}
	if tasks[eventsIdx]["failed_when"] != false {
		t.Fatalf("reading the clone events is diagnostics on a path that already fell back, and must never fail the boot, got failed_when=%v", tasks[eventsIdx]["failed_when"])
	}
	if got := fmt.Sprint(tasks[eventsIdx]["ansible.builtin.command"]); !strings.Contains(got, "bootwright_kubevirt_iso_dv_name") {
		t.Fatalf("the clone events must be selected on the clone DataVolume itself, got %v", tasks[eventsIdx]["ansible.builtin.command"])
	}
	if tasks[cloneIdx]["failed_when"] != false {
		t.Fatalf("a clone the API server refuses must degrade to the direct upload like every other shared-media failure, not kill the host mid-boot, got failed_when=%v", tasks[cloneIdx]["failed_when"])
	}
	for _, idx := range []int{cloneCleanupIdx, uploadIdx} {
		if got := fmt.Sprint(tasks[idx]["when"]); !strings.Contains(got, "bootwright_kubevirt_iso_cloned") {
			t.Fatalf("%v must be driven by the clone outcome so unsupported storage falls back to the direct upload, got when=%v", tasks[idx]["name"], tasks[idx]["when"])
		}
	}
	cleanupArgv := fmt.Sprint(tasks[cloneCleanupIdx]["ansible.builtin.command"])
	if !strings.Contains(cleanupArgv, "bootwright_kubevirt_iso_dv_name") || !strings.Contains(cleanupArgv, "--ignore-not-found=true") {
		t.Fatalf("a failed clone must be removed before the direct upload recreates the per-machine DataVolume, got %v", tasks[cloneCleanupIdx])
	}

	template := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/templates/datavolume-agent-iso.yaml.j2")
	for _, want := range []string{
		"name: {{ bootwright_kubevirt_iso_dv_name }}",
		"bootwright.io/node: {{ bootwright_component.name }}",
		"bootwright.io/role: agent-iso",
		"bootwright.io/managed-by: bootwright",
		"cdi.kubevirt.io/storage.bind.immediate.requested",
		"name: {{ bootwright_kubevirt_iso_source_dv_name }}",
	} {
		if !strings.Contains(template, want) {
			t.Fatalf("per-VM agent ISO clone template must keep the per-VM name, labels and immediate binding while sourcing the shared PVC (missing %q)", want)
		}
	}
	if !strings.Contains(template, "source:\n    pvc:") {
		t.Fatal("per-VM agent ISO DataVolume must clone from the shared source PVC")
	}
	if !strings.Contains(template, "storage:\n    resources:") {
		t.Fatal("the clone target must be requested through spec.storage so CDI completes accessModes, volumeMode and the filesystem overhead from the same StorageProfile that virtctl image-upload used for the shared source; spec.pvc is copied through verbatim and pairs a Filesystem target with a Block source, which disqualifies both smart-clone strategies and then stalls the host-assisted copy at CloneInProgress forever")
	}
	for _, line := range strings.Split(template, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.Contains(line, "accessModes") || strings.Contains(line, "volumeMode") {
			t.Fatalf("the clone target must name neither accessModes nor volumeMode: the storage class is operator-supplied, and any value spelled out here is a guess that can differ from the shared source it clones, got %q", line)
		}
	}
}

func TestKubeVirtRootDiskInheritsVolumeModeFromTheStorageProfile(t *testing.T) {
	template := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/templates/datavolume-root.yaml.j2")
	if !strings.Contains(template, "storage:\n    resources:") {
		t.Fatal("the root disk must be requested through spec.storage so CDI completes accessModes and volumeMode from the class's StorageProfile; spec.pvc is copied through verbatim, so the claim silently defaults to volumeMode Filesystem and a Block class such as Ceph RBD ends up backing the guest's virtio root disk with a disk.img file on a filesystem inside the RBD image, which slows the agent's image write enough to trip its 30-minute no-progress watchdog")
	}
	if strings.Contains(template, "  pvc:\n") {
		t.Fatal("the root disk must not be requested through spec.pvc")
	}
	for _, line := range strings.Split(template, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.Contains(line, "accessModes") || strings.Contains(line, "volumeMode") {
			t.Fatalf("the root disk must name neither accessModes nor volumeMode: the storage class is operator-supplied and only its StorageProfile knows which pair it supports, got %q", line)
		}
	}
	for _, want := range []string{
		"blank: {}",
		"cdi.kubevirt.io/storage.bind.immediate.requested",
		"bootwright.io/role: root",
		"bootwright.io/managed-by: bootwright",
		fmt.Sprintf("storage: {{ bootwright_component.profile.diskGiB | default(%d, true) }}Gi", topology.KubeVirtDefaultDiskGiB),
		"storageClassName: {{ bootwright_kubevirt_storage_class }}",
	} {
		if !strings.Contains(template, want) {
			t.Fatalf("root disk template must keep its blank source, immediate binding, ownership labels and profile-driven size (missing %q)", want)
		}
	}

	tasks := readAnsibleTasks(t, kubeVirtSubstrateTasks)
	deleteIdx := findAnsibleTask(t, tasks, "Delete KubeVirt root DataVolume for authorized rebuild")
	applyIdx := findAnsibleTask(t, tasks, "Apply KubeVirt root disk DataVolume")
	if deleteIdx > applyIdx {
		t.Fatalf("an authorized rebuild must delete the root DataVolume before the apply recreates it (delete=%d apply=%d)", deleteIdx, applyIdx)
	}
	guard := fmt.Sprint(tasks[applyIdx]["when"])
	for _, want := range []string{"bootwright_kubevirt_root_dv_exists", "bootwright_kubevirt_rebuild_authorized"} {
		if !strings.Contains(guard, want) {
			t.Fatalf("the root DataVolume apply must create only, never re-send a spec to a live claim that CDI's webhook refuses to update and that holds the machine's installed operating system (missing %q), got when=%v", want, tasks[applyIdx]["when"])
		}
	}
}

func TestKubeVirtRefusesAReusedRootClaimWhoseVolumeModeDriftedFromItsStorageProfile(t *testing.T) {
	tasks := readAnsibleTasks(t, kubeVirtSubstrateTasks)

	claimIdx := findAnsibleTask(t, tasks, "Read reused KubeVirt root claim volume mode")
	profileIdx := findAnsibleTask(t, tasks, "Read KubeVirt storage profile volume mode")
	claimConclusiveIdx := findAnsibleTask(t, tasks, "Require a conclusive reused KubeVirt root claim volume mode probe")
	profileConclusiveIdx := findAnsibleTask(t, tasks, "Require a conclusive KubeVirt storage profile volume mode probe")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse a reused KubeVirt root claim whose volume mode drifted from its storage profile")
	vmIdx := findAnsibleTask(t, tasks, "Apply KubeVirt VirtualMachine")
	if !(claimIdx < claimConclusiveIdx && profileIdx < profileConclusiveIdx && claimConclusiveIdx < refuseIdx && profileConclusiveIdx < refuseIdx && refuseIdx < vmIdx) {
		t.Fatalf("both reused-volume probes must resolve conclusively before drift is judged and before the VirtualMachine is applied (claim=%d claimConclusive=%d profile=%d profileConclusive=%d refuse=%d vm=%d)", claimIdx, claimConclusiveIdx, profileIdx, profileConclusiveIdx, refuseIdx, vmIdx)
	}

	for _, tc := range []struct {
		name     string
		probeIdx int
		gateIdx  int
	}{
		{name: "root claim", probeIdx: claimIdx, gateIdx: claimConclusiveIdx},
		{name: "storage profile", probeIdx: profileIdx, gateIdx: profileConclusiveIdx},
	} {
		if tasks[tc.probeIdx]["failed_when"] != false {
			t.Fatalf("the %s probe must defer its verdict to an actionable assertion, got failed_when=%v", tc.name, tasks[tc.probeIdx]["failed_when"])
		}
		gate, ok := tasks[tc.gateIdx]["ansible.builtin.assert"].(map[string]any)
		if !ok {
			t.Fatalf("the %s probe verdict must be an assert, got %v", tc.name, tasks[tc.gateIdx])
		}
		that := fmt.Sprint(gate["that"])
		for _, want := range []string{"rc | default(1)) == 0", "stdout", "length > 0", "Block", "Filesystem"} {
			if !strings.Contains(that, want) {
				t.Fatalf("the %s probe must require a successful nonempty live answer (missing %q), got %v", tc.name, want, gate["that"])
			}
		}
		for _, want := range []string{"Refusing to apply", "bootwright_mutating_invocation", "same bootwright apply invocation"} {
			if !strings.Contains(fmt.Sprint(gate["fail_msg"]), want) {
				t.Fatalf("the %s probe refusal must fail closed with an exact retry (missing %q), got %v", tc.name, want, gate["fail_msg"])
			}
		}
		guard := fmt.Sprint(tasks[tc.gateIdx]["when"])
		for _, want := range []string{"bootwright_kubevirt_root_dv_exists", "bootwright_kubevirt_rebuild_authorized", "bootwright_kubevirt_storage_class"} {
			if !strings.Contains(guard, want) {
				t.Fatalf("the %s probe verdict must apply exactly to a reused configured-class disk (missing %q), got when=%v", tc.name, want, tasks[tc.gateIdx]["when"])
			}
		}
	}

	assertion, ok := tasks[refuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the volume-mode refusal must be an assert, got %v", tasks[refuseIdx])
	}
	if that := fmt.Sprint(assertion["that"]); !strings.Contains(that, "bootwright_kubevirt_root_claim_mode == bootwright_kubevirt_storage_profile_mode") {
		t.Fatalf("the volume-mode refusal must compare the bound claim against what the class provisions: volumeMode is immutable once bound and the apply is create-only, so a claim an older template shape created is reused forever and a Filesystem claim on a Block class trips the agent's 30-minute no-progress watchdog, got that=%v", assertion["that"])
	}
	failMsg := fmt.Sprint(assertion["fail_msg"])
	for _, want := range []string{"bootwright_apply_rebuild_invocation", "30m0s", "--machines"} {
		if !strings.Contains(failMsg, want) {
			t.Fatalf("the volume-mode refusal must name the symptom it prevents and the exact command that proceeds intentionally (missing %q), got %q", want, failMsg)
		}
	}
	guard := fmt.Sprint(tasks[refuseIdx]["when"])
	for _, want := range []string{"bootwright_kubevirt_root_dv_exists", "bootwright_kubevirt_rebuild_authorized", "bootwright_kubevirt_storage_class"} {
		if !strings.Contains(guard, want) {
			t.Fatalf("the volume-mode refusal applies only to a claim this run reuses; an absent disk is about to be created correctly and an authorized rebuild deletes it first (missing %q), got when=%v", want, tasks[refuseIdx]["when"])
		}
	}
}

func TestKubeVirtBootKeepsAPerMachineAgentISOThatAlreadyHoldsThisRunsMedia(t *testing.T) {
	tasks := readAnsibleTasks(t, kubeVirtBootTasks)
	currentIdx := findAnsibleTask(t, tasks, "Resolve KubeVirt agent ISO DataVolume currency")
	current, ok := tasks[currentIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("per-machine media currency must be a set_fact, got %v", tasks[currentIdx])
	}
	currentExpr := fmt.Sprint(current["bootwright_kubevirt_boot_iso_current"])
	for _, want := range []string{"bootwright_kubevirt_boot_iso_owned", "bootwright.io/agent-iso-generation", "'Succeeded'"} {
		if !strings.Contains(currentExpr, want) {
			t.Fatalf("per-machine media may only be reused when it is owned, carries this run's generation and imported successfully (missing %q): %v", want, current["bootwright_kubevirt_boot_iso_current"])
		}
	}

	guarded := []string{
		"Stop KubeVirt VirtualMachine before replacing the agent ISO",
		"Remove previous KubeVirt agent ISO DataVolume",
		"Purge the previous KubeVirt agent ISO PersistentVolumeClaim",
		"Clone KubeVirt agent ISO DataVolume from the shared source",
		"Report a rejected KubeVirt agent ISO DataVolume clone",
		"Wait for the KubeVirt agent ISO DataVolume clone",
		"Read the KubeVirt agent ISO clone events",
		"Remove the failed KubeVirt agent ISO DataVolume clone",
		"Purge the failed KubeVirt agent ISO clone PersistentVolumeClaim",
		"Build virtctl image-upload command",
		"Upload KubeVirt agent ISO DataVolume",
		"Require a successful KubeVirt agent ISO upload",
		"Label KubeVirt agent ISO DataVolume as Bootwright-managed",
	}
	for _, name := range guarded {
		idx := findAnsibleTask(t, tasks, name)
		if idx < currentIdx {
			t.Fatalf("%q rebuilds the boot media and must run after the currency verdict (currency=%d task=%d)", name, currentIdx, idx)
		}
		if got := fmt.Sprint(tasks[idx]["when"]); !strings.Contains(got, "not (bootwright_kubevirt_boot_iso_current") {
			t.Fatalf("%q must be skipped when the machine's DataVolume already holds this run's ISO, or every apply stops the VM and re-materialises media it already has, got when=%v", name, tasks[idx]["when"])
		}
	}

	label := fmt.Sprint(tasks[findAnsibleTask(t, tasks, "Label KubeVirt agent ISO DataVolume as Bootwright-managed")]["ansible.builtin.command"])
	if !strings.Contains(label, "bootwright.io/agent-iso-generation=") {
		t.Fatalf("a directly uploaded per-machine DataVolume must be stamped with the generation, or the next apply cannot tell it is current, got %v", label)
	}
	template := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/templates/datavolume-agent-iso.yaml.j2")
	if !strings.Contains(template, "bootwright.io/agent-iso-generation: {{ bootwright_kubevirt_iso_generation }}") {
		t.Fatal("a cloned per-machine DataVolume must carry the generation label from birth, or a clone can never be reused")
	}

	waitIdx := findAnsibleTask(t, tasks, "Wait for the shared KubeVirt agent ISO source DataVolume")
	if got := fmt.Sprint(tasks[waitIdx]["when"]); !strings.Contains(got, "not (bootwright_kubevirt_boot_iso_current") {
		t.Fatalf("a machine that keeps its own media never clones and must not wait for the shared source at all, got when=%v", tasks[waitIdx]["when"])
	}
}

func TestKubeVirtBootClearsTheAgentISOClaimItsDataVolumeLeavesBehind(t *testing.T) {
	tasks := readAnsibleTasks(t, kubeVirtBootTasks)
	for _, site := range []struct {
		remove  string
		purge   string
		claim   string
		rebuild string
	}{
		{
			remove:  "Remove stale KubeVirt agent ISO source DataVolume",
			purge:   "Purge the stale KubeVirt agent ISO source PersistentVolumeClaim",
			claim:   "bootwright_kubevirt_iso_source_dv_name",
			rebuild: "Upload KubeVirt agent ISO source DataVolume",
		},
		{
			remove:  "Remove previous KubeVirt agent ISO DataVolume",
			purge:   "Purge the previous KubeVirt agent ISO PersistentVolumeClaim",
			claim:   "bootwright_kubevirt_iso_dv_name",
			rebuild: "Clone KubeVirt agent ISO DataVolume from the shared source",
		},
		{
			remove:  "Remove the failed KubeVirt agent ISO DataVolume clone",
			purge:   "Purge the failed KubeVirt agent ISO clone PersistentVolumeClaim",
			claim:   "bootwright_kubevirt_iso_dv_name",
			rebuild: "Upload KubeVirt agent ISO DataVolume",
		},
	} {
		removeIdx := findAnsibleTask(t, tasks, site.remove)
		purgeIdx := findAnsibleTask(t, tasks, site.purge)
		rebuildIdx := findAnsibleTask(t, tasks, site.rebuild)
		if !(removeIdx < purgeIdx && purgeIdx < rebuildIdx) {
			t.Fatalf("%q must clear the claim between the DataVolume deletion and the media rebuild, or the rebuild meets a claim no DataVolume owns (remove=%d purge=%d rebuild=%d)", site.purge, removeIdx, purgeIdx, rebuildIdx)
		}
		if got := fmt.Sprint(tasks[purgeIdx]["ansible.builtin.include_tasks"]); got != "purge_media_pvc.yml" {
			t.Fatalf("%q must reuse the shared claim purge, got %v", site.purge, tasks[purgeIdx])
		}
		vars, ok := tasks[purgeIdx]["vars"].(map[string]any)
		if !ok || !strings.Contains(fmt.Sprint(vars["bootwright_kubevirt_media_pvc_name"]), site.claim) {
			t.Fatalf("%q must purge the claim named after the DataVolume it just deleted (%s), got vars=%v", site.purge, site.claim, tasks[purgeIdx]["vars"])
		}
		if got, want := fmt.Sprint(tasks[purgeIdx]["when"]), fmt.Sprint(tasks[removeIdx]["when"]); got != want {
			t.Fatalf("%q must run under exactly the conditions that deleted the DataVolume, or it purges media this run still boots from: got when=%v want when=%v", site.purge, tasks[purgeIdx]["when"], tasks[removeIdx]["when"])
		}
	}

	purge := readAnsibleTasks(t, kubeVirtPurgeMediaPVCTasks)
	deleteIdx := findAnsibleTask(t, purge, "Remove the KubeVirt agent ISO PersistentVolumeClaim")
	waitIdx := findAnsibleTask(t, purge, "Wait for the KubeVirt agent ISO PersistentVolumeClaim to disappear")
	requireIdx := findAnsibleTask(t, purge, "Require the KubeVirt agent ISO PersistentVolumeClaim to be gone")
	if !(deleteIdx < waitIdx && waitIdx < requireIdx) {
		t.Fatalf("the claim purge must delete, then wait, then prove the claim gone (delete=%d wait=%d require=%d)", deleteIdx, waitIdx, requireIdx)
	}
	deleteArgv := fmt.Sprint(purge[deleteIdx]["ansible.builtin.command"])
	for _, want := range []string{"persistentvolumeclaim", "bootwright_kubevirt_media_pvc_name", "--ignore-not-found=true"} {
		if !strings.Contains(deleteArgv, want) {
			t.Fatalf("the claim purge must delete the named claim idempotently (missing %q), got %v", want, purge[deleteIdx]["ansible.builtin.command"])
		}
	}
	if purge[waitIdx]["failed_when"] != false {
		t.Fatalf("the claim-gone poll must report through its own verdict instead of failing on a kubectl NotFound, got failed_when=%v", purge[waitIdx]["failed_when"])
	}
	waitUntil := fmt.Sprint(purge[waitIdx]["until"])
	if !strings.Contains(waitUntil, "NotFound") {
		t.Fatalf("a claim still terminating is not gone: the poll must settle on NotFound, got until=%v", purge[waitIdx]["until"])
	}
	if !strings.Contains(waitUntil, "attempts") || !strings.Contains(waitUntil, "bootwright_kubevirt_media_pvc_settle_retries") {
		t.Fatalf("the claim-gone poll must carry the house attempts escape so an exhausted budget lands on the verdict, got until=%v", purge[waitIdx]["until"])
	}
	verdict, ok := purge[requireIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the claim-gone verdict must be an assert, got %v", purge[requireIdx])
	}
	if got := fmt.Sprint(verdict["that"]); !strings.Contains(got, "NotFound") {
		t.Fatalf("the claim-gone verdict must require a NotFound probe, not merely a finished poll, got %v", verdict["that"])
	}
	if got := fmt.Sprint(verdict["fail_msg"]); !strings.Contains(got, "No DataVolume is associated with the existing PVC") {
		t.Fatalf("the claim-gone verdict must name the virtctl error a surviving claim produces, or the operator cannot connect the two, got %v", verdict["fail_msg"])
	}

	defaults := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/defaults/main.yml")
	for _, want := range []string{
		"bootwright_kubevirt_media_pvc_settle_retries:",
		"bootwright_kubevirt_media_pvc_settle_delay:",
		"bootwright_kubevirt_iso_clone_start_retries:",
	} {
		if !strings.Contains(defaults, want) {
			t.Fatalf("the claim-settle budget must be tunable from defaults (missing %q)", want)
		}
	}
}

func TestKubeVirtBootGatesOnGuestReadinessNotJustVirtualMachineInstanceReadiness(t *testing.T) {
	tasks := readAnsibleTasks(t, kubeVirtBootTasks)
	vmiIdx := findAnsibleTask(t, tasks, "Wait for KubeVirt VirtualMachineInstance readiness")
	readyIdx := findAnsibleTask(t, tasks, "Wait for KubeVirt node readiness")
	if vmiIdx > readyIdx {
		t.Fatalf("KubeVirt boot must gate on guest readiness after the VirtualMachineInstance is Ready")
	}
	assertIncludeRoleName(t, tasks[readyIdx], "bootwright.core.support_ssh_readiness")

	vars, ok := tasks[readyIdx]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no vars", tasks[readyIdx]["name"])
	}
	for key, want := range map[string]string{
		"bootwright_ssh_ready_readiness":               "bootwright_component.boot.readiness",
		"bootwright_ssh_ready_address":                 "bootwright_component.primaryIPAddress",
		"bootwright_ssh_ready_node_name":               "bootwright_component.name",
		"bootwright_ssh_ready_key_path":                "bootwright_current_cluster.nodeSSHPrivateKeyPath",
		"bootwright_ssh_ready_default_port":            "bootwright_kubevirt_node_ready_probe_port",
		"bootwright_ssh_ready_port_timeout_seconds":    "bootwright_kubevirt_node_ready_timeout_seconds",
		"bootwright_ssh_ready_connect_timeout_seconds": "bootwright_kubevirt_node_ready_ssh_connect_timeout_seconds",
		"bootwright_ssh_ready_auth_retries":            "bootwright_kubevirt_node_ready_ssh_auth_retries",
		"bootwright_ssh_ready_auth_delay_seconds":      "bootwright_kubevirt_node_ready_ssh_auth_delay_seconds",
	} {
		if got := fmt.Sprint(vars[key]); !strings.Contains(got, want) {
			t.Fatalf("KubeVirt node readiness %s got %q, want it to carry %q", key, got, want)
		}
	}

	defaults := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/defaults/main.yml")
	for _, want := range []string{
		"bootwright_kubevirt_node_ready_probe_port:",
		"bootwright_kubevirt_node_ready_timeout_seconds:",
		"bootwright_kubevirt_node_ready_ssh_connect_timeout_seconds:",
		"bootwright_kubevirt_node_ready_ssh_auth_retries:",
		"bootwright_kubevirt_node_ready_ssh_auth_delay_seconds:",
	} {
		if !strings.Contains(defaults, want) {
			t.Fatalf("KubeVirt boot defaults missing %q", want)
		}
	}
}

func TestKubeVirtDestroyReleasesSharedAgentISOSourceAtClusterScope(t *testing.T) {
	topTasks := readAnsibleTasks(t, kubeVirtSubstrateDestroyTasks)
	inputs, ok := topTasks[findAnsibleTask(t, topTasks, "Resolve KubeVirt destroy inputs")]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatal("kubevirt destroy inputs must be a set_fact")
	}
	if got := fmt.Sprint(inputs["bootwright_kubevirt_iso_source_dv_name"]); !strings.Contains(got, "agent-iso-source") {
		t.Fatalf("kubevirt destroy must know the cluster-scoped shared media name, got %v", inputs["bootwright_kubevirt_iso_source_dv_name"])
	}
	if got := fmt.Sprint(inputs["bootwright_kubevirt_cluster_scope_teardown"]); !strings.Contains(got, "bootwright_destroy_machine_scope is not defined") {
		t.Fatalf("shared media teardown must be reserved for a cluster-scoped destroy so a --machines destroy cannot pull media the other machines still boot from, got %v", inputs["bootwright_kubevirt_cluster_scope_teardown"])
	}

	tasks := nestedAnsibleTasks(t, topTasks[findAnsibleTask(t, topTasks, "Tear down KubeVirt guest on the reachable host cluster")], "block")
	probeIdx := findAnsibleTask(t, tasks, "Read KubeVirt DataVolume identities")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse to delete a foreign KubeVirt DataVolume")
	sourceDeleteIdx := findAnsibleTask(t, tasks, "Delete the shared KubeVirt agent ISO source DataVolume")
	if !(probeIdx < refuseIdx && refuseIdx < sourceDeleteIdx) {
		t.Fatalf("shared media must be probed and ownership-verified before deletion (probe=%d refuse=%d sourceDelete=%d)", probeIdx, refuseIdx, sourceDeleteIdx)
	}
	probeLoop := fmt.Sprint(tasks[probeIdx]["loop"])
	if !strings.Contains(probeLoop, "bootwright_kubevirt_destroy_storage_targets") {
		t.Fatalf("the per-item DataVolume probe must consume the scope-aware exact identity table, got loop=%v", tasks[probeIdx]["loop"])
	}
	sourceWhen := fmt.Sprint(tasks[sourceDeleteIdx]["when"])
	if !strings.Contains(sourceWhen, "bootwright_kubevirt_cluster_scope_teardown") {
		t.Fatalf("shared media deletion must be gated on cluster scope, got when=%v", tasks[sourceDeleteIdx]["when"])
	}
	if !strings.Contains(sourceWhen, "bootwright_kubevirt_dv_probe") || !strings.Contains(sourceWhen, "bootwright_kubevirt_iso_source_dv_name") {
		t.Fatalf("shared media deletion must stay gated on its own per-item probe, got when=%v", tasks[sourceDeleteIdx]["when"])
	}
	targetsIdx := findAnsibleTask(t, topTasks, "Resolve KubeVirt destroy storage targets")
	targets, ok := topTasks[targetsIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("kubevirt destroy storage targets must be a set_fact, got %v", topTasks[targetsIdx])
	}
	sharedIdentity := fmt.Sprint(targets["bootwright_kubevirt_destroy_storage_targets"])
	for _, want := range []string{"bootwright_kubevirt_iso_source_dv_name", "'node': ''", "'role': 'agent-iso-source'"} {
		if !strings.Contains(sharedIdentity, want) {
			t.Fatalf("the cluster-scoped shared source must use its exact node-less role identity (missing %q), got %v", want, targets["bootwright_kubevirt_destroy_storage_targets"])
		}
	}
	sourceArgv := fmt.Sprint(tasks[sourceDeleteIdx]["ansible.builtin.command"])
	if !strings.Contains(sourceArgv, "--ignore-not-found=true") {
		t.Fatalf("shared media deletion must stay idempotent across the cluster's machines, got %v", tasks[sourceDeleteIdx])
	}
}

func TestKubeVirtDestroyClearsTheClaimsItsDataVolumesLeaveBehind(t *testing.T) {
	topTasks := readAnsibleTasks(t, kubeVirtSubstrateDestroyTasks)
	blockIdx := findAnsibleTask(t, topTasks, "Tear down KubeVirt guest on the reachable host cluster")
	recordIdx := findAnsibleTask(t, topTasks, "Remove KubeVirt machine ownership record")
	if blockIdx > recordIdx {
		t.Fatalf("the guest teardown must finish before the ownership record is dropped, or destroy forgets a machine whose storage it never proved released (block=%d record=%d)", blockIdx, recordIdx)
	}

	tasks := nestedAnsibleTasks(t, topTasks[blockIdx], "block")
	claimProbeIdx := findAnsibleTask(t, tasks, "Read KubeVirt PersistentVolumeClaim identities")
	claimConclusiveIdx := findAnsibleTask(t, tasks, "Require conclusive KubeVirt PersistentVolumeClaim probes")
	dvDeleteIdx := findAnsibleTask(t, tasks, "Delete KubeVirt DataVolumes")
	if !(claimProbeIdx < claimConclusiveIdx && claimConclusiveIdx < dvDeleteIdx) {
		t.Fatalf("the claims must be conclusively probed before their DataVolumes are deleted, or a failed probe becomes absence (probe=%d conclusive=%d dvDelete=%d)", claimProbeIdx, claimConclusiveIdx, dvDeleteIdx)
	}
	claimProbe, ok := tasks[claimProbeIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("the claim probe must be a command task, got %v", tasks[claimProbeIdx])
	}
	if got := fmt.Sprint(claimProbe["argv"]); !strings.Contains(got, "persistentvolumeclaim") {
		t.Fatalf("the claim probe must read the claim itself, not its DataVolume, got argv=%v", claimProbe["argv"])
	}
	if got := fmt.Sprint(claimProbe["argv"]); !strings.Contains(got, "json") || strings.Contains(got, "jsonpath") {
		t.Fatalf("the claim probe must read the whole live identity document, not only managed-by, got argv=%v", claimProbe["argv"])
	}
	if tasks[claimProbeIdx]["failed_when"] != false {
		t.Fatalf("the claim probe must record an absent claim instead of failing destroy, got failed_when=%v", tasks[claimProbeIdx]["failed_when"])
	}
	probeLoop := fmt.Sprint(tasks[claimProbeIdx]["loop"])
	if !strings.Contains(probeLoop, "bootwright_kubevirt_destroy_storage_targets") {
		t.Fatalf("the claim probe must use the exact per-resource identity table, got loop=%v", tasks[claimProbeIdx]["loop"])
	}
	targetsIdx := findAnsibleTask(t, topTasks, "Resolve KubeVirt destroy storage targets")
	targets, ok := topTasks[targetsIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("kubevirt destroy storage targets must be a set_fact, got %v", topTasks[targetsIdx])
	}
	targetsExpr := fmt.Sprint(targets["bootwright_kubevirt_destroy_storage_targets"])
	claimConclusive, ok := tasks[claimConclusiveIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the claim probe verdict must be an assert, got %v", tasks[claimConclusiveIdx])
	}
	claimConclusiveThat := fmt.Sprint(claimConclusive["that"])
	for _, want := range []string{"NotFound", "item.rc is defined", "(item.rc | int) == 0", "(item.rc | int) != 0", "bootwright_kubernetes_object_probe", "PersistentVolumeClaim", "item.item.name", "bootwright_kubevirt_namespace"} {
		if !strings.Contains(claimConclusiveThat, want) {
			t.Fatalf("a claim probe may resolve only to an exact safely parsed live document or NotFound (missing %q), got %v", want, claimConclusive["that"])
		}
	}
	for _, want := range []string{"Refusing to treat it as absent", "bootwright_kubernetes_object_probe", "bootwright_mutating_invocation"} {
		if got := fmt.Sprint(claimConclusive["fail_msg"]); !strings.Contains(got, want) {
			t.Fatalf("an inconclusive claim probe must preserve evidence and name the exact retry (missing %q), got %v", want, claimConclusive["fail_msg"])
		}
	}
	if body := readRepoFile(t, kubeVirtSubstrateDestroyTasks); strings.Contains(body, "from_json") {
		t.Fatal("KubeVirt destroy must never parse a successful live probe through throwing from_json; malformed output is an actionable unknown-state refusal")
	}

	claimExactIdx := findAnsibleTask(t, tasks, "Resolve exact KubeVirt PersistentVolumeClaim identities")
	claimDVIdx := findAnsibleTask(t, tasks, "Resolve KubeVirt PersistentVolumeClaim DataVolume identities")
	claimRefuseIdx := findAnsibleTask(t, tasks, "Refuse to delete a foreign KubeVirt PersistentVolumeClaim")
	claimStampIdx := findAnsibleTask(t, tasks, "Stamp inherited KubeVirt PersistentVolumeClaim identities")
	claimStampRequireIdx := findAnsibleTask(t, tasks, "Require inherited KubeVirt PersistentVolumeClaim identity stamps")
	vmRefuseIdx := findAnsibleTask(t, tasks, "Refuse to delete a non-Bootwright KubeVirt VirtualMachine")
	dvRefuseIdx := findAnsibleTask(t, tasks, "Refuse to delete a foreign KubeVirt DataVolume")
	if !(claimProbeIdx < claimRefuseIdx && claimRefuseIdx < dvDeleteIdx) {
		t.Fatalf("each claim must be ownership-checked between its probe and the first deletion of this block, or the gate runs over state the run already changed (probe=%d refuse=%d dvDelete=%d)", claimProbeIdx, claimRefuseIdx, dvDeleteIdx)
	}
	if !(claimConclusiveIdx < claimExactIdx && claimExactIdx < claimDVIdx && claimDVIdx < claimRefuseIdx && claimRefuseIdx < claimStampIdx && claimStampIdx < claimStampRequireIdx && claimStampRequireIdx < dvDeleteIdx) {
		t.Fatalf("claim identity must be proven and durably stamped before its DataVolume evidence is deleted (conclusive=%d exact=%d association=%d refuse=%d stamp=%d require=%d delete=%d)", claimConclusiveIdx, claimExactIdx, claimDVIdx, claimRefuseIdx, claimStampIdx, claimStampRequireIdx, dvDeleteIdx)
	}
	if !(vmRefuseIdx < claimStampIdx && dvRefuseIdx < claimStampIdx && claimRefuseIdx < claimStampIdx) {
		t.Fatalf("every selected VM, DataVolume, and claim identity gate must pass before the first live mutation stamps retry evidence (vm=%d dv=%d pvc=%d stamp=%d)", vmRefuseIdx, dvRefuseIdx, claimRefuseIdx, claimStampIdx)
	}
	claimRefuse, ok := tasks[claimRefuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the claim ownership gate must be an assert, got %v", tasks[claimRefuseIdx])
	}
	if got := fmt.Sprint(tasks[claimRefuseIdx]["loop"]); !strings.Contains(got, "bootwright_kubevirt_destroy_storage_targets") {
		t.Fatalf("the claim ownership gate must judge each exact target identity, got loop=%v", tasks[claimRefuseIdx]["loop"])
	}
	claimRefuseThat := fmt.Sprint(claimRefuse["that"])
	for _, want := range []string{"bootwright_kubevirt_pvc_live_by_name", "bootwright_kubevirt_pvc_exact_identity_by_name", "bootwright_kubevirt_pvc_dv_identity_by_name"} {
		if !strings.Contains(claimRefuseThat, want) {
			t.Fatalf("the claim ownership gate must require absence, exact labels, or an exact DataVolume association (missing %q), got that=%v", want, claimRefuse["that"])
		}
	}
	claimExact, ok := tasks[claimExactIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("exact claim identity resolution must be a set_fact, got %v", tasks[claimExactIdx])
	}
	claimExactExpr := fmt.Sprint(claimExact["bootwright_kubevirt_pvc_exact_identity_by_name"])
	for _, want := range []string{"bootwright.io/managed-by", "bootwright.io/context", "bootwright.io/cluster", "bootwright.io/role", "bootwright.io/node", "bootwright_kubevirt_context_name", "item.node"} {
		if !strings.Contains(claimExactExpr, want) {
			t.Fatalf("a directly labelled claim must match the whole target identity (missing %q), got %v", want, claimExact["bootwright_kubevirt_pvc_exact_identity_by_name"])
		}
	}
	claimDV, ok := tasks[claimDVIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("claim DataVolume identity resolution must be a set_fact, got %v", tasks[claimDVIdx])
	}
	claimDVExpr := fmt.Sprint(claimDV["bootwright_kubevirt_pvc_dv_identity_by_name"])
	for _, want := range []string{"bootwright_kubevirt_dv_owned_by_name", "ownerReferences", "apiVersion", "DataVolume", "name", "uid"} {
		if !strings.Contains(claimDVExpr, want) {
			t.Fatalf("an unlabelled CDI claim must be tied by UID to the exact verified DataVolume before it is adopted (missing %q), got %v", want, claimDV["bootwright_kubevirt_pvc_dv_identity_by_name"])
		}
	}
	for _, want := range []string{"managed-by", "context", "cluster", "role", "node", "ownerReference", "bootwright_mutating_invocation", "--authorize unowned-networks"} {
		if !strings.Contains(fmt.Sprint(claimRefuse["fail_msg"]), want) {
			t.Fatalf("the claim ownership gate message must name the identity and exact retry (missing %q), got %v", want, claimRefuse["fail_msg"])
		}
	}
	claimRefuseWhen := fmt.Sprint(tasks[claimRefuseIdx]["when"])
	if !strings.Contains(claimRefuseWhen, "not (bootwright_destroy_authorize_unowned_networks") {
		t.Fatalf("a claim another context owns must be deletable only under --authorize unowned-networks, the token that already covers the DataVolume it belongs to, got when=%v", claimRefuseWhen)
	}
	if strings.Contains(claimRefuseWhen, "bootwright_destroy_authorize_unowned_vms") {
		t.Fatalf("the claim ownership gate must not be lifted by the VM authorization, got when=%v", claimRefuseWhen)
	}
	stampArgv := fmt.Sprint(tasks[claimStampIdx]["ansible.builtin.command"])
	for _, want := range []string{"persistentvolumeclaim", "bootwright.io/managed-by=bootwright", "bootwright.io/context=", "bootwright.io/cluster=", "bootwright.io/role=", "bootwright.io/node=", "--overwrite"} {
		if !strings.Contains(stampArgv, want) {
			t.Fatalf("an inherited claim identity must be durably stamped before its DataVolume is erased (missing %q), got %v", want, tasks[claimStampIdx])
		}
	}
	stampWhen := fmt.Sprint(tasks[claimStampIdx]["when"])
	for _, want := range []string{"bootwright_kubevirt_pvc_exact_identity_by_name", "bootwright_kubevirt_pvc_dv_identity_by_name", "bootwright_destroy_authorize_unowned_networks"} {
		if !strings.Contains(stampWhen, want) {
			t.Fatalf("claim identity stamping must be limited to conclusively inherited, non-forced identities (missing %q), got when=%v", want, tasks[claimStampIdx]["when"])
		}
	}
	stampRequire, ok := tasks[claimStampRequireIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("claim identity stamp result must be asserted before deletion, got %v", tasks[claimStampRequireIdx])
	}
	if got := fmt.Sprint(stampRequire["fail_msg"]); !strings.Contains(got, "ownership evidence") || !strings.Contains(got, "bootwright_mutating_invocation") {
		t.Fatalf("a failed identity stamp must preserve evidence and name the exact retry, got %v", stampRequire["fail_msg"])
	}

	for _, site := range []struct {
		remove string
		purge  string
		claim  string
	}{
		{
			remove: "Delete KubeVirt DataVolumes",
			purge:  "Purge the KubeVirt root disk PersistentVolumeClaim",
			claim:  "bootwright_kubevirt_root_dv_name",
		},
		{
			remove: "Delete KubeVirt DataVolumes",
			purge:  "Purge the KubeVirt agent ISO PersistentVolumeClaim",
			claim:  "bootwright_kubevirt_iso_dv_name",
		},
		{
			remove: "Delete the shared KubeVirt agent ISO source DataVolume",
			purge:  "Purge the shared KubeVirt agent ISO source PersistentVolumeClaim",
			claim:  "bootwright_kubevirt_iso_source_dv_name",
		},
	} {
		removeIdx := findAnsibleTask(t, tasks, site.remove)
		purgeIdx := findAnsibleTask(t, tasks, site.purge)
		if removeIdx > purgeIdx {
			t.Fatalf("%q must prove the claim released after its DataVolume is deleted, or destroy reports success over storage that outlives it (remove=%d purge=%d)", site.purge, removeIdx, purgeIdx)
		}
		if claimRefuseIdx > purgeIdx {
			t.Fatalf("%q must run after the claim ownership gate, or a claim another context owns is deleted before anything judged it (refuse=%d purge=%d)", site.purge, claimRefuseIdx, purgeIdx)
		}
		if !strings.Contains(targetsExpr, site.claim) {
			t.Fatalf("the claim target table must cover %s, or its purge can never fire, got targets=%v", site.claim, targets["bootwright_kubevirt_destroy_storage_targets"])
		}
		include, ok := tasks[purgeIdx]["ansible.builtin.include_role"].(map[string]any)
		if !ok {
			t.Fatalf("%q must reuse the shared claim purge through include_role, got %v", site.purge, tasks[purgeIdx])
		}
		if got := fmt.Sprint(include["name"]); got != "bootwright.core.container_cluster_boot_kubevirt" {
			t.Fatalf("%q must reuse the boot role's claim purge so the settle budget stays one default, got name=%v", site.purge, include["name"])
		}
		if got := fmt.Sprint(include["tasks_from"]); got != "purge_media_pvc.yml" {
			t.Fatalf("%q must reuse the shared claim purge task file, got tasks_from=%v", site.purge, include["tasks_from"])
		}
		vars, ok := tasks[purgeIdx]["vars"].(map[string]any)
		if !ok || !strings.Contains(fmt.Sprint(vars["bootwright_kubevirt_media_pvc_name"]), site.claim) {
			t.Fatalf("%q must purge the claim named after the DataVolume it just deleted (%s), got vars=%v", site.purge, site.claim, tasks[purgeIdx]["vars"])
		}
		purgeWhen := fmt.Sprint(tasks[purgeIdx]["when"])
		if strings.Contains(purgeWhen, "bootwright_kubevirt_dv_owner") {
			t.Fatalf("%q must not be gated on the DataVolume probe: the deletion above removes that DataVolume, so a re-run after a stuck claim would skip the purge and leak the claim for good, got when=%v", site.purge, tasks[purgeIdx]["when"])
		}
		if !strings.Contains(purgeWhen, "bootwright_kubevirt_pvc_probe") || !strings.Contains(purgeWhen, site.claim) {
			t.Fatalf("%q must fire on that claim's own presence, so a re-run still clears a claim an earlier run orphaned, got when=%v", site.purge, tasks[purgeIdx]["when"])
		}
		if !strings.Contains(purgeWhen, "'rc', 'equalto', 0") {
			t.Fatalf("%q must fire only on a claim the probe actually observed, or a destroy with no claim left pays the settle budget to prove it, got when=%v", site.purge, tasks[purgeIdx]["when"])
		}
	}

	sharedPurgeWhen := fmt.Sprint(tasks[findAnsibleTask(t, tasks, "Purge the shared KubeVirt agent ISO source PersistentVolumeClaim")]["when"])
	if !strings.Contains(sharedPurgeWhen, "bootwright_kubevirt_cluster_scope_teardown") {
		t.Fatalf("the shared media claim purge must stay gated on cluster scope, or a --machines destroy pulls a claim its peers still boot from, got when=%v", sharedPurgeWhen)
	}

	purge := readAnsibleTasks(t, kubeVirtPurgeMediaPVCTasks)
	deleteIdx := findAnsibleTask(t, purge, "Remove the KubeVirt agent ISO PersistentVolumeClaim")
	if purge[deleteIdx]["failed_when"] != false {
		t.Fatalf("a transient claim deletion error must not abort destroy before the ownership record is dropped: the probe and verdict below are the single decision point, got failed_when=%v", purge[deleteIdx]["failed_when"])
	}
}

func TestHostVirtctlUsesMaterializedHostKubeconfig(t *testing.T) {
	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_host_virtctl_provision.yml")
	if !strings.Contains(body, "bootwright_kubevirt_host_kubeconfigs[bootwright_task_host_cluster_name]") {
		t.Fatal("host virtctl playbook does not use the materialized host kubeconfig map")
	}
	if strings.Contains(body, "bootwright_clusters_dir") {
		t.Fatal("host virtctl playbook constructs a durable cluster-secret path")
	}
}

func TestKubeVirtRebuildPurgesTheRootClaimItsDataVolumeLeavesBehind(t *testing.T) {
	tasks := readAnsibleTasks(t, kubeVirtSubstrateTasks)
	deleteIdx := findAnsibleTask(t, tasks, "Delete KubeVirt root DataVolume for authorized rebuild")
	purgeIdx := findAnsibleTask(t, tasks, "Purge the KubeVirt root disk PersistentVolumeClaim for authorized rebuild")
	applyIdx := findAnsibleTask(t, tasks, "Apply KubeVirt root disk DataVolume")
	if !(deleteIdx < purgeIdx && purgeIdx < applyIdx) {
		t.Fatalf("an authorized rebuild must delete the root DataVolume, prove its claim is gone, then recreate it (delete=%d purge=%d apply=%d): CDI creates the claim as a dependent, and a claim that outlives its DataVolume cannot be adopted by a new one of the same name, so the recreate stalls on the remedy the volume-mode refusal itself recommends", deleteIdx, purgeIdx, applyIdx)
	}
	include, ok := tasks[purgeIdx]["ansible.builtin.include_role"].(map[string]any)
	if !ok {
		t.Fatalf("the rebuild purge must reuse the shared claim-purge tasks, got %v", tasks[purgeIdx])
	}
	if include["name"] != "bootwright.core.container_cluster_boot_kubevirt" || include["tasks_from"] != "purge_media_pvc.yml" {
		t.Fatalf("the rebuild purge must include the same purge_media_pvc.yml destroy already reuses, got %v", include)
	}
	purgeVars, ok := tasks[purgeIdx]["vars"].(map[string]any)
	if !ok || purgeVars["bootwright_kubevirt_media_pvc_name"] != "{{ bootwright_kubevirt_root_dv_name }}" {
		t.Fatalf("the rebuild purge must target the root disk claim, got vars=%v", tasks[purgeIdx]["vars"])
	}
	guard := fmt.Sprint(tasks[purgeIdx]["when"])
	for _, want := range []string{"bootwright_kubevirt_rebuild_authorized", "bootwright_kubevirt_root_dv_exists"} {
		if !strings.Contains(guard, want) {
			t.Fatalf("the rebuild purge must run only for an authorized rebuild of an existing disk (missing %q), got when=%v", want, tasks[purgeIdx]["when"])
		}
	}
}

func TestKubeVirtRootDiskWaitIsBudgetedAndDiagnosed(t *testing.T) {
	tasks := readAnsibleTasks(t, kubeVirtSubstrateTasks)
	waitIdx := findAnsibleTask(t, tasks, "Wait for KubeVirt root disk DataVolume")
	assertIdx := findAnsibleTask(t, tasks, "Require the KubeVirt root disk DataVolume to be ready")
	if waitIdx > assertIdx {
		t.Fatalf("the root disk wait must resolve before its assertion (wait=%d assert=%d)", waitIdx, assertIdx)
	}
	wait := tasks[waitIdx]
	if wait["failed_when"] != false {
		t.Fatalf("the root disk wait must defer failure to its assertion so the diagnosis runs first, got failed_when=%v", wait["failed_when"])
	}
	command, ok := wait["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("the root disk wait must be a command probe, got %v", wait)
	}
	if !stringListContains(command["argv"], "--timeout={{ bootwright_kubevirt_root_dv_ready_timeout }}") {
		t.Fatalf("the root disk wait budget must be configurable rather than hardcoded: a blank disk on a busy or externally backed storage class is slow, not broken, got argv=%v", command["argv"])
	}
	var defaults map[string]any
	if err := yaml.Unmarshal([]byte(readRepoFile(t, bootwrightCollectionRoleRoot+"/machine_substrate_kubevirt/defaults/main.yml")), &defaults); err != nil {
		t.Fatalf("decode machine_substrate_kubevirt defaults: %v", err)
	}
	if defaults["bootwright_kubevirt_root_dv_ready_timeout"] == nil {
		t.Fatalf("the root disk wait budget must have a role default, got %v", defaults)
	}
	assertBody, ok := tasks[assertIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the root disk readiness gate must be an assert, got %v", tasks[assertIdx])
	}
	failMsg := fmt.Sprint(assertBody["fail_msg"])
	for _, want := range []string{"storage", "describe pvc", "bootwright_kubevirt_root_dv_ready_timeout"} {
		if !strings.Contains(failMsg, want) {
			t.Fatalf("a root disk that never provisions is a storage condition on the host cluster; the failure must name it and where to look (missing %q), got %q", want, failMsg)
		}
	}
}
