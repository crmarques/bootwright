package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

const (
	libvirtNetworkApplyPath   = bootwrightCollectionRoleRoot + "/machine_substrate_libvirt/tasks/network.yml"
	libvirtMachineApplyPath   = bootwrightCollectionRoleRoot + "/machine_substrate_libvirt/tasks/machine.yml"
	libvirtMachineDestroyPath = bootwrightCollectionRoleRoot + "/machine_substrate_libvirt/tasks/destroy.yml"
	libvirtContextDestroyPath = bootwrightCollectionRoleRoot + "/provider_host_libvirt/tasks/destroy_context.yml"
	vsphereMediaCleanupPath   = bootwrightCollectionRoleRoot + "/container_cluster_media_vsphere/tasks/cleanup.yml"
	vsphereDestroyPath        = bootwrightCollectionRoleRoot + "/machine_substrate_vsphere/tasks/destroy.yml"
	vsphereDestroyMediaPath   = bootwrightCollectionRoleRoot + "/machine_substrate_vsphere/tasks/destroy_vmedia.yml"
)

func TestLibvirtNetworkApplyRequiresExactLiveIdentityBeforeMutation(t *testing.T) {
	tasks := readAnsibleTasks(t, libvirtNetworkApplyPath)
	probeIdx := findAnsibleTask(t, tasks, "Read live libvirt network definition before apply")
	conclusiveIdx := findAnsibleTask(t, tasks, "Require a conclusive libvirt network probe before apply")
	identityIdx := findAnsibleTask(t, tasks, "Resolve live libvirt network identity before apply")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse to redefine a foreign libvirt network")
	directoryIdx := findAnsibleTask(t, tasks, "Create per-cluster libvirt state directory")
	defineIdx := findAnsibleTask(t, tasks, "Define libvirt network")
	activateIdx := findAnsibleTask(t, tasks, "Activate libvirt network")
	if !(probeIdx < conclusiveIdx && conclusiveIdx < identityIdx && identityIdx < refuseIdx && refuseIdx < directoryIdx && directoryIdx < defineIdx && defineIdx < activateIdx) {
		t.Fatalf("live network identity gate must precede every apply mutation: probe=%d conclusive=%d identity=%d refuse=%d directory=%d define=%d activate=%d", probeIdx, conclusiveIdx, identityIdx, refuseIdx, directoryIdx, defineIdx, activateIdx)
	}
	probe := tasks[probeIdx]
	if probe["failed_when"] != false || !strings.Contains(fmt.Sprint(probe["ansible.builtin.command"]), "net-dumpxml") {
		t.Fatalf("network apply must capture virsh net-dumpxml for an explicit conclusive gate, got %v", probe)
	}
	conclusive := fmt.Sprint(tasks[conclusiveIdx]["ansible.builtin.assert"])
	for _, want := range []string{"rc is defined", "stdout is defined", "stderr is defined", "bootwright_libvirt_explicit_absence", "bootwright_mutating_invocation"} {
		if !strings.Contains(conclusive, want) {
			t.Fatalf("network apply conclusive gate missing %q: %s", want, conclusive)
		}
	}
	identity := fmt.Sprint(tasks[identityIdx]["ansible.builtin.set_fact"])
	if !strings.Contains(identity, "bootwright.core.bootwright_libvirt_resource_identity") {
		t.Fatalf("network apply must parse live XML through the fail-closed identity classifier, got %s", identity)
	}
	refuse := fmt.Sprint(tasks[refuseIdx]["ansible.builtin.assert"])
	for _, want := range []string{"bootwright_libvirt_apply_network_identity", "context", "cluster", "bootwright_mutating_invocation", "foreign or unprovable network"} {
		if !strings.Contains(refuse, want) {
			t.Fatalf("network apply exact-identity refusal missing %q: %s", want, refuse)
		}
	}
	if strings.Contains(refuse, "bootwright_ownership_records") || strings.Contains(refuse, "No Bootwright retry command") {
		t.Fatalf("network apply must neither trust a controller record nor omit the exact post-repair retry: %s", refuse)
	}
}

func TestLibvirtDomainApplyAndDestroyRequireExactLiveIdentityAndSuccessFields(t *testing.T) {
	for _, tc := range []struct {
		path       string
		probe      string
		conclusive string
		identity   string
		firstWrite string
	}{
		{
			path:       libvirtMachineApplyPath,
			probe:      "Read libvirt domain ownership metadata for apply",
			conclusive: "Require a conclusive libvirt domain probe",
			identity:   "Resolve libvirt domain ownership for apply",
			firstWrite: "Create per-machine libvirt state directories",
		},
		{
			path:       libvirtMachineDestroyPath,
			probe:      "Read libvirt domain ownership metadata",
			conclusive: "Require a conclusive libvirt domain probe before destroy",
			identity:   "Resolve libvirt domain ownership for destroy",
			firstWrite: "Stop libvirt domain",
		},
	} {
		tasks := readAnsibleTasks(t, tc.path)
		probeIdx := findAnsibleTask(t, tasks, tc.probe)
		conclusiveIdx := findAnsibleTask(t, tasks, tc.conclusive)
		identityIdx := findAnsibleTask(t, tasks, tc.identity)
		writeIdx := findAnsibleTask(t, tasks, tc.firstWrite)
		if !(probeIdx < conclusiveIdx && conclusiveIdx < identityIdx && identityIdx < writeIdx) {
			t.Fatalf("%s live domain gate must precede mutation: probe=%d conclusive=%d identity=%d write=%d", tc.path, probeIdx, conclusiveIdx, identityIdx, writeIdx)
		}
		conclusive := fmt.Sprint(tasks[conclusiveIdx]["ansible.builtin.assert"])
		for _, want := range []string{"rc is defined", "stdout is defined", "stderr is defined", "bootwright_libvirt_explicit_absence", "bootwright_mutating_invocation"} {
			if !strings.Contains(conclusive, want) {
				t.Fatalf("%s conclusive domain gate missing %q: %s", tc.path, want, conclusive)
			}
		}
		identity := fmt.Sprint(tasks[identityIdx]["ansible.builtin.set_fact"])
		for _, want := range []string{"bootwright_libvirt_resource_identity", "context", "cluster", "machine"} {
			if !strings.Contains(identity, want) {
				t.Fatalf("%s exact domain identity missing %q: %s", tc.path, want, identity)
			}
		}
	}

	contextTasks := readAnsibleTasks(t, libvirtContextDestroyPath)
	for _, taskName := range []string{
		"Require conclusive libvirt block-device probes before context sweep",
		"Require conclusive libvirt ownership probes before context sweep",
		"Require swept libvirt domain absence before context storage deletion",
	} {
		assertion := fmt.Sprint(contextTasks[findAnsibleTask(t, contextTasks, taskName)]["ansible.builtin.assert"])
		for _, want := range []string{"rc is defined", "bootwright_libvirt_explicit_absence", "bootwright_mutating_invocation"} {
			if !strings.Contains(assertion, want) {
				t.Fatalf("context sweep gate %q missing %q: %s", taskName, want, assertion)
			}
		}
	}
}

func TestLibvirtSafetyGatesDoNotTreatAmbiguousFailedToGetAsAbsence(t *testing.T) {
	root := "ansible/collections/ansible_collections/bootwright/core"
	for _, diagnostic := range []string{"failed to get domain", "failed to get network"} {
		if files := repoFilesContaining(t, root, ".yml", diagnostic); len(files) > 0 {
			t.Fatalf("ambiguous libvirt diagnostic %q must not prove absence; found in %v", diagnostic, files)
		}
	}
}

func TestLibvirtNetworkDestroyUsesOnlyExactLiveIdentity(t *testing.T) {
	tasks := readAnsibleTasks(t, machineInfraPrepareDestroy)
	gate := nestedAnsibleTasks(t, tasks[findAnsibleTask(t, tasks, "Gate the cluster libvirt network before destroy")], "block")
	conclusiveIdx := findAnsibleTask(t, gate, "Require a conclusive libvirt network probe before destroy")
	identityIdx := findAnsibleTask(t, gate, "Resolve live libvirt network identity for destroy")
	ownedIdx := findAnsibleTask(t, gate, "Resolve libvirt network ownership for destroy")
	refuseIdx := findAnsibleTask(t, gate, "Refuse to remove a non-Bootwright libvirt network")
	authorizeIdx := findAnsibleTask(t, gate, "Authorize cluster libvirt network removal")
	if !(conclusiveIdx < identityIdx && identityIdx < ownedIdx && ownedIdx < refuseIdx && refuseIdx < authorizeIdx) {
		t.Fatalf("destroy must classify exact live identity before authorizing removal: conclusive=%d identity=%d owned=%d refuse=%d authorize=%d", conclusiveIdx, identityIdx, ownedIdx, refuseIdx, authorizeIdx)
	}
	conclusive := fmt.Sprint(gate[conclusiveIdx]["ansible.builtin.assert"])
	for _, want := range []string{"rc is defined", "stdout is defined", "stderr is defined", "bootwright_libvirt_explicit_absence"} {
		if !strings.Contains(conclusive, want) {
			t.Fatalf("network destroy conclusive gate missing %q: %s", want, conclusive)
		}
	}
	identity := fmt.Sprint(gate[identityIdx]["ansible.builtin.set_fact"])
	if !strings.Contains(identity, "bootwright.core.bootwright_libvirt_resource_identity") {
		t.Fatalf("network destroy must use the exact XML classifier, got %s", identity)
	}
	owned := fmt.Sprint(gate[ownedIdx]["ansible.builtin.set_fact"])
	for _, want := range []string{"bootwright_libvirt_network_identity", "context", "cluster"} {
		if !strings.Contains(owned, want) {
			t.Fatalf("network destroy live ownership decision missing %q: %s", want, owned)
		}
	}
	if strings.Contains(owned, "bootwright_ownership_records") {
		t.Fatalf("a stale or name-only controller record must never override contradictory live network identity: %s", owned)
	}
	message := ansibleFailureMessage(t, gate[refuseIdx])
	for _, want := range []string{"contradictory or malformed live identity", "bootwright_mutating_invocation", "--authorize unowned-networks"} {
		if !strings.Contains(message, want) {
			t.Fatalf("network destroy refusal missing %q: %s", want, message)
		}
	}
}

func TestLibvirtExistingRootDiskProbeFailsBeforeOwnershipOrDomainMutation(t *testing.T) {
	tasks := readAnsibleTasks(t, libvirtMachineApplyPath)
	probeIdx := findAnsibleTask(t, tasks, "Probe existing libvirt root disk size")
	evidenceIdx := findAnsibleTask(t, tasks, "Resolve existing libvirt root disk probe evidence")
	conclusiveIdx := findAnsibleTask(t, tasks, "Require a conclusive existing libvirt root disk probe")
	createIdx := findAnsibleTask(t, tasks, "Create machine disk")
	ownIdx := findAnsibleTask(t, tasks, "Align libvirt disk image ownership")
	domainIdx := findAnsibleTask(t, tasks, "Define libvirt domain")
	if !(probeIdx < evidenceIdx && evidenceIdx < conclusiveIdx && conclusiveIdx < createIdx && createIdx < ownIdx && ownIdx < domainIdx) {
		t.Fatalf("existing root disk evidence must fail closed before create, ownership, or domain mutation: probe=%d evidence=%d conclusive=%d create=%d own=%d domain=%d", probeIdx, evidenceIdx, conclusiveIdx, createIdx, ownIdx, domainIdx)
	}
	evidence := fmt.Sprint(tasks[evidenceIdx]["ansible.builtin.set_fact"])
	for _, want := range []string{"results[0].stat.exists", "results[1].stat.exists", "bootwright.core.bootwright_qemu_image_virtual_size"} {
		if !strings.Contains(evidence, want) {
			t.Fatalf("root disk evidence must cover migrated and current locations plus strict JSON parsing, missing %q: %s", want, evidence)
		}
	}
	conclusive := fmt.Sprint(tasks[conclusiveIdx]["ansible.builtin.assert"])
	for _, want := range []string{"bootwright_libvirt_root_disk_present", "rc is defined", "rc == 0", "stdout is defined", "bootwright_libvirt_root_disk_virtual_size", "bootwright_mutating_invocation"} {
		if !strings.Contains(conclusive, want) {
			t.Fatalf("root disk conclusive gate missing %q: %s", want, conclusive)
		}
	}
	resize := fmt.Sprint(tasks[findAnsibleTask(t, tasks, "Refuse an in-place libvirt root disk resize")])
	if strings.Contains(resize, "from_json") || !strings.Contains(resize, "bootwright_libvirt_root_disk_virtual_size") {
		t.Fatalf("root disk resize must consume already-validated evidence, got %s", resize)
	}
}

func TestVSphereMediaReplacementValidatesAuthorityAndRetainsEvidenceOnDeleteFailure(t *testing.T) {
	top := readAnsibleTasks(t, vsphereMediaInsertPath)
	tasks := flattenAnsibleTasks(top)
	probeIdx := findAnsibleTask(t, tasks, "Probe previously recorded vSphere virtual media")
	pathGuardIdx := findAnsibleTask(t, tasks, "Require a safe vSphere virtual media ownership record path before replacement")
	readIdx := findAnsibleTask(t, tasks, "Read previously recorded vSphere virtual media")
	readGuardIdx := findAnsibleTask(t, tasks, "Require readable vSphere virtual media ownership evidence before replacement")
	validateIdx := findAnsibleTask(t, tasks, "Require exact vSphere virtual media ownership before replacement")
	deleteIdx := findAnsibleTask(t, tasks, "Delete superseded vSphere virtual media before releasing evidence")
	cleanupIdx := findAnsibleTask(t, tasks, "Remove superseded vSphere virtual media staging paths and record")
	uploadIdx := findAnsibleTask(t, tasks, "Upload vSphere virtual media to the datastore")
	if !(probeIdx < pathGuardIdx && pathGuardIdx < readIdx && readIdx < readGuardIdx && readGuardIdx < validateIdx && validateIdx < deleteIdx && deleteIdx < cleanupIdx && cleanupIdx < uploadIdx) {
		t.Fatalf("replacement must prove a safe readable record, validate authority, delete remotely, and only then clean evidence before upload: probe=%d path=%d read=%d readable=%d validate=%d delete=%d cleanup=%d upload=%d", probeIdx, pathGuardIdx, readIdx, readGuardIdx, validateIdx, deleteIdx, cleanupIdx, uploadIdx)
	}
	requireVSphereVMediaRecordFileGuards(t, tasks[probeIdx], tasks[pathGuardIdx], tasks[readIdx], tasks[readGuardIdx])
	requireVSphereVMediaAuthorityFilter(t, tasks[validateIdx])
	delete := tasks[deleteIdx]
	block := nestedAnsibleTasks(t, delete, "block")
	remote := block[findAnsibleTask(t, block, "Delete superseded vSphere virtual media from the datastore")]
	if _, ok := remote["failed_when"]; ok {
		t.Fatalf("superseded datastore deletion must raise into rescue before evidence cleanup, got %v", remote)
	}
	rescue := nestedAnsibleTasks(t, delete, "rescue")
	message := ansibleFailureMessage(t, rescue[findAnsibleTask(t, rescue, "Refuse superseded vSphere virtual media evidence cleanup after datastore failure")])
	for _, want := range []string{"Refusing to delete local staging state or ownership", "bootwright_mutating_invocation"} {
		if !strings.Contains(message, want) {
			t.Fatalf("superseded media delete failure missing %q: %s", want, message)
		}
	}
}

func TestVSphereMediaCleanupValidatesAuthorityBeforeMutationAndRetainsEvidence(t *testing.T) {
	top := readAnsibleTasks(t, vsphereMediaCleanupPath)
	tasks := flattenAnsibleTasks(top)
	probeIdx := findAnsibleTask(t, tasks, "Probe recorded vSphere virtual media")
	pathGuardIdx := findAnsibleTask(t, tasks, "Require a safe vSphere virtual media ownership record path before cleanup")
	readIdx := findAnsibleTask(t, tasks, "Read recorded vSphere virtual media")
	readGuardIdx := findAnsibleTask(t, tasks, "Require readable vSphere virtual media ownership evidence before cleanup")
	validateIdx := findAnsibleTask(t, tasks, "Require exact vSphere virtual media ownership before cleanup")
	driveIdx := findAnsibleTask(t, tasks, "Remove vSphere virtual media drive")
	deleteIdx := findAnsibleTask(t, tasks, "Delete uploaded vSphere virtual media before releasing evidence")
	evidenceIdx := findAnsibleTask(t, tasks, "Remove recorded vSphere virtual media staging paths and record")
	if !(probeIdx < pathGuardIdx && pathGuardIdx < readIdx && readIdx < readGuardIdx && readGuardIdx < validateIdx && validateIdx < driveIdx && driveIdx < deleteIdx && deleteIdx < evidenceIdx) {
		t.Fatalf("cleanup must prove a safe readable record before authority and drive/datastore mutation, with evidence last: probe=%d path=%d read=%d readable=%d validate=%d drive=%d delete=%d evidence=%d", probeIdx, pathGuardIdx, readIdx, readGuardIdx, validateIdx, driveIdx, deleteIdx, evidenceIdx)
	}
	requireVSphereVMediaRecordFileGuards(t, tasks[probeIdx], tasks[pathGuardIdx], tasks[readIdx], tasks[readGuardIdx])
	requireVSphereVMediaAuthorityFilter(t, tasks[validateIdx])
	delete := tasks[deleteIdx]
	block := nestedAnsibleTasks(t, delete, "block")
	remote := block[findAnsibleTask(t, block, "Delete uploaded vSphere virtual media from the datastore")]
	if _, ok := remote["failed_when"]; ok {
		t.Fatalf("cleanup datastore deletion must raise into rescue before evidence cleanup, got %v", remote)
	}
	rescue := nestedAnsibleTasks(t, delete, "rescue")
	message := ansibleFailureMessage(t, rescue[findAnsibleTask(t, rescue, "Refuse vSphere virtual media evidence cleanup after datastore failure")])
	if !strings.Contains(message, "bootwright_mutating_invocation") {
		t.Fatalf("cleanup delete failure must render the exact retry invocation: %s", message)
	}
}

func TestVSphereDestroyValidatesEveryMediaRecordBeforeMutation(t *testing.T) {
	tasks := readAnsibleTasks(t, vsphereDestroyPath)
	validateIdx := findAnsibleTask(t, tasks, "Require exact recorded vSphere virtual media ownership before destroy")
	vmDeleteIdx := findAnsibleTask(t, tasks, "Delete vSphere VM")
	mediaDeleteIdx := findAnsibleTask(t, tasks, "Delete recorded vSphere virtual media from the datastore")
	evidenceIdx := findAnsibleTask(t, tasks, "Remove recorded vSphere virtual media staging paths and records")
	if !(validateIdx < vmDeleteIdx && vmDeleteIdx < mediaDeleteIdx && mediaDeleteIdx < evidenceIdx) {
		t.Fatalf("every media record must be validated before VM/media mutation and evidence must be last: validate=%d vm=%d media=%d evidence=%d", validateIdx, vmDeleteIdx, mediaDeleteIdx, evidenceIdx)
	}
	requireVSphereVMediaAuthorityFilter(t, tasks[validateIdx])
	if got := fmt.Sprint(tasks[validateIdx]["loop"]); !strings.Contains(got, "bootwright_vsphere_vmedia_records") {
		t.Fatalf("destroy media authority gate must cover every selected record, got loop=%v", tasks[validateIdx]["loop"])
	}
	if _, ok := tasks[mediaDeleteIdx]["when"]; ok {
		t.Fatalf("validated records must not bypass remote deletion through empty-field when clauses, got when=%v", tasks[mediaDeleteIdx]["when"])
	}
	destroyMedia := readAnsibleTasks(t, vsphereDestroyMediaPath)
	guard := destroyMedia[findAnsibleTask(t, destroyMedia, "Delete recorded vSphere virtual media before releasing ownership")]
	remote := nestedAnsibleTasks(t, guard, "block")
	if _, ok := remote[findAnsibleTask(t, remote, "Delete recorded vSphere virtual media file")]["failed_when"]; ok {
		t.Fatalf("destroy datastore deletion must fail before its later ownership cleanup: %v", remote)
	}
}

func requireVSphereVMediaAuthorityFilter(t *testing.T, task map[string]any) {
	t.Helper()
	assertion := fmt.Sprint(task["ansible.builtin.assert"])
	for _, want := range []string{"bootwright.core.bootwright_vsphere_vmedia_record_authorized", "bootwright_clusters_dir", "bootwright_current_cluster.name", "bootwright_component.name", "bootwright_component.providerName", "bootwright_mutating_invocation", "authorization"} {
		if !strings.Contains(assertion, want) {
			t.Fatalf("vSphere media authority gate missing %q: %s", want, assertion)
		}
	}
}

func requireVSphereVMediaRecordFileGuards(t *testing.T, probe, pathGuard, read, readGuard map[string]any) {
	t.Helper()
	if probe["failed_when"] != false {
		t.Fatalf("vSphere media record stat must defer failure to its actionable success-only guard: %v", probe)
	}
	pathAssertion := fmt.Sprint(pathGuard["ansible.builtin.assert"])
	for _, want := range []string{"stat.exists is defined", "stat.isreg is defined", "stat.islnk is defined", "regular non-symlink", "bootwright_mutating_invocation"} {
		if !strings.Contains(pathAssertion, want) {
			t.Fatalf("vSphere media record path guard missing unsafe-probe fixture %q: %s", want, pathAssertion)
		}
	}
	if read["failed_when"] != false {
		t.Fatalf("vSphere media record slurp must defer failure to its actionable content guard: %v", read)
	}
	readAssertion := fmt.Sprint(readGuard["ansible.builtin.assert"])
	for _, want := range []string{"content is defined", "unreadable record", "bootwright_mutating_invocation"} {
		if !strings.Contains(readAssertion, want) {
			t.Fatalf("vSphere media record read guard missing failed-probe fixture %q: %s", want, readAssertion)
		}
	}
}
