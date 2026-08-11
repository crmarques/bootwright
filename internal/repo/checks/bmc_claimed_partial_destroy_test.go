package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

func TestBMCClaimedPartialDestroyUsesExactDurableClaimAuthority(t *testing.T) {
	const path = bootwrightCollectionRoleRoot + "/provider_service_bmc_emulated/tasks/destroy.yml"
	tasks := flattenAnsibleTasks(readAnsibleTasks(t, path))
	probe := findAnsibleTask(t, tasks, "Probe exact BMC provider-global claim before unrecorded destroy")
	authority := findAnsibleTask(t, tasks, "Resolve exact claimed partial BMC destroy authority")
	refusal := findAnsibleTask(t, tasks, "Refuse unrecorded BMC live or local state")
	record := findAnsibleTask(t, tasks, "Resolve exact claimed BMC record for partial teardown")
	destroy := findAnsibleTask(t, tasks, "Destroy exact claimed partial BMC composite")
	if !(probe < authority && authority < refusal && refusal < record && record < destroy) {
		t.Fatalf("claimed partial BMC destroy must probe, classify, refuse unclaimed state, synthesize exact evidence, then mutate: %d %d %d %d %d", probe, authority, refusal, record, destroy)
	}
	authorityExpr := fmt.Sprint(tasks[authority]["ansible.builtin.set_fact"])
	for _, want := range []string{
		"bootwright_bmc_destroy_records",
		"bootwright_bmc_claim_record",
	} {
		if !strings.Contains(authorityExpr, want) {
			t.Errorf("claimed partial authority is missing %q: %s", want, authorityExpr)
		}
	}
	refusalExpr := fmt.Sprint(tasks[refusal])
	for _, want := range []string{
		"not (bootwright_bmc_claimed_partial_destroy_detected | bool)",
		"not (bootwright_bmc_claim_root_stat.stat.exists | bool)",
		"not (bootwright_bmc_live_resource_present | bool)",
		"not (bootwright_bmc_live_state_present | bool)",
		"bootwright_mutating_invocation",
	} {
		if !strings.Contains(refusalExpr, want) {
			t.Errorf("unclaimed BMC refusal is missing %q: %s", want, refusalExpr)
		}
	}
	recordExpr := fmt.Sprint(tasks[record]["ansible.builtin.set_fact"])
	for _, want := range []string{
		"bootwright.io/ownership/v1alpha1",
		"bootwright_bmc_claim_record",
	} {
		if !strings.Contains(recordExpr, want) {
			t.Errorf("synthetic claimed-partial evidence is missing %q: %s", want, recordExpr)
		}
	}
	destroyExpr := fmt.Sprint(tasks[destroy])
	for _, want := range []string{
		"destroy_resource.yml",
		"bootwright_bmc_desired_destroy_verified:true",
		"bootwright_bmc_claimed_partial_destroy_verified:true",
	} {
		if !strings.Contains(destroyExpr, want) {
			t.Errorf("claimed-partial destroy dispatch is missing %q: %s", want, destroyExpr)
		}
	}
	if strings.Contains(readRepoFile(t, path), "bootwright_apply_full_invocation") {
		t.Fatal("BMC destroy must not require a broad apply invocation to recover an exact claimed partial")
	}
}

func TestGenericRecordedDestroyAcceptsOnlyGatedClaimedPartialBMC(t *testing.T) {
	const path = bootwrightCollectionRoleRoot + "/ownership_record/tasks/destroy_resource.yml"
	tasks := flattenAnsibleTasks(readAnsibleTasks(t, path))
	validate := findAnsibleTask(t, tasks, "Validate live ownership evidence before recorded resource mutation")
	race := findAnsibleTask(t, tasks, "Require claimed partial BMC evidence to remain record-free")
	prefer := findAnsibleTask(t, tasks, "Prefer validated live ownership evidence")
	gate := findAnsibleTask(t, tasks, "Revalidate recorded resource before teardown")
	authority := findAnsibleTask(t, tasks, "Require live record or positive all-members-absent replay")
	endpoints := findAnsibleTask(t, tasks, "Acquire exact BMC endpoint authority for recorded or claim-backed teardown")
	firstMutation := findAnsibleTask(t, tasks, "Destroy exact BMC provider-global composite")
	if !(validate < race && race < prefer && prefer < gate && gate < authority && authority < endpoints && endpoints < firstMutation) {
		t.Fatalf("claimed partial authority must remain record-free, behind the exact composite gate and endpoint authority, and ahead of mutation: %d %d %d %d %d %d %d", validate, race, prefer, gate, authority, endpoints, firstMutation)
	}
	raceExpr := fmt.Sprint(tasks[race])
	for _, want := range []string{
		"not (bootwright_ownership_validated_stat.stat.exists | bool)",
		"bootwright_bmc_claimed_partial_destroy_verified",
		"bootwright_mutating_invocation",
	} {
		if !strings.Contains(raceExpr, want) {
			t.Errorf("claimed-partial record race gate is missing %q: %s", want, raceExpr)
		}
	}
	expr := fmt.Sprint(tasks[authority])
	for _, want := range []string{
		"bootwright_ownership_destroy_kind == 'bmc-emulator'",
		"bootwright_bmc_claimed_partial_destroy_verified",
	} {
		if !strings.Contains(expr, want) {
			t.Errorf("generic destroy claimed-partial authority is missing %q: %s", want, expr)
		}
	}
	mutationWhen := fmt.Sprint(tasks[firstMutation]["when"])
	if !strings.Contains(mutationWhen, "not (bootwright_ownership_bmc_replay_absent") {
		t.Fatalf("positive all-members-absent BMC replay must skip the exact runtime teardown block: %s", mutationWhen)
	}
	endpointWhen := fmt.Sprint(tasks[endpoints]["when"])
	if !strings.Contains(endpointWhen, "not (bootwright_ownership_bmc_replay_absent") {
		t.Fatalf("positive all-members-absent BMC replay must not create endpoint authority: %s", endpointWhen)
	}
	remove := findAnsibleTask(t, tasks, "Remove destroyed ownership resource record")
	revalidate := findAnsibleTask(t, tasks, "Revalidate host operation before BMC owner record removal")
	if !(firstMutation < revalidate && revalidate < remove) {
		t.Fatalf("BMC record removal must be the final host-operation-guarded evidence mutation: destroy=%d revalidate=%d remove=%d", firstMutation, revalidate, remove)
	}
	if !strings.Contains(fmt.Sprint(tasks[remove]["when"]), "bootwright_bmc_claimed_partial_destroy_verified") {
		t.Fatal("claimed-partial BMC teardown must never remove a record that appears during recovery")
	}
}

func TestBMCClaimAndLiveRuntimeProofPrecedeEveryServiceMutation(t *testing.T) {
	const gatePath = bootwrightCollectionRoleRoot + "/provider_service_bmc_emulated/tasks/ownership_gate.yml"
	gateTasks := flattenAnsibleTasks(readAnsibleTasks(t, gatePath))
	claimProbe := findAnsibleTask(t, gateTasks, "Probe exact BMC provider-global claim before live classification")
	loadedProbe := findAnsibleTask(t, gateTasks, "Probe loaded BMC systemd unit identities")
	mountProbe := findAnsibleTask(t, gateTasks, "Probe mounts beneath BMC owned roots")
	refusal := findAnsibleTask(t, gateTasks, "Refuse foreign or unprovable BMC provider-global replacement")
	if !(claimProbe < loadedProbe && loadedProbe < mountProbe && mountProbe < refusal) {
		t.Fatalf("BMC claim, loaded-unit, and mount proof must precede replacement authorization: %d %d %d %d", claimProbe, loadedProbe, mountProbe, refusal)
	}

	const applyPath = bootwrightCollectionRoleRoot + "/provider_service_bmc_emulated/tasks/main.yml"
	applyTasks := flattenAnsibleTasks(readAnsibleTasks(t, applyPath))
	gate := findAnsibleTask(t, applyTasks, "Prove BMC emulator ownership before apply mutation")
	operation := findAnsibleTask(t, applyTasks, "Revalidate host-wide operation before BMC evidence publication")
	endpoints := findAnsibleTask(t, applyTasks, "Resolve exact BMC endpoints before durable intent publication")
	claimWrite := findAnsibleTask(t, applyTasks, "Publish host-atomic BMC claim before apply mutation")
	reservation := findAnsibleTask(t, applyTasks, "Reserve complete BMC endpoint union after durable claim publication")
	slots := findAnsibleTask(t, applyTasks, "Acquire exact BMC endpoint slots after durable claim publication")
	portTransition := findAnsibleTask(t, applyTasks, "Release old listeners blocking desired BMC cross-port transition")
	firstMutation := findAnsibleTask(t, applyTasks, "Prepare BMC emulator host")
	if !(gate < operation && operation < endpoints && endpoints < claimWrite && claimWrite < reservation && reservation < slots && slots < portTransition && portTransition < firstMutation) {
		t.Fatalf("BMC apply must gate runtime, bind and scan under the host operation, publish reconstructible family evidence, reserve its endpoint union, acquire slots, and resolve cross-port listeners before mutation: %d %d %d %d %d %d %d %d", gate, operation, endpoints, claimWrite, reservation, slots, portTransition, firstMutation)
	}
	endpointContent := readRepoFile(t, bootwrightCollectionRoleRoot+"/provider_service_bmc_emulated/tasks/apply/endpoint_claims.yml")
	for _, want := range []string{"retainedClaims", "Bind BMC apply to command-wide selected host consequences"} {
		if !strings.Contains(endpointContent, want) {
			t.Errorf("BMC apply endpoint transition is missing %q", want)
		}
	}
	reservationContent := readRepoFile(t, bootwrightCollectionRoleRoot+"/provider_service_bmc_emulated/tasks/apply/endpoint_reservation.yml")
	for _, want := range []string{"retainedClaims", "reserve_host_endpoint_claims.yml"} {
		if !strings.Contains(reservationContent, want) {
			t.Errorf("BMC apply endpoint reservation is missing %q", want)
		}
	}

	const destroyPath = bootwrightCollectionRoleRoot + "/provider_service_bmc_emulated/tasks/destroy_owned.yml"
	destroyTop := readAnsibleTasks(t, destroyPath)
	preflight := findAnsibleTask(t, destroyTop, "Require exact BMC teardown preconditions")
	record := findAnsibleTask(t, destroyTop, "Recheck BMC ownership record immediately before teardown")
	gateAgain := findAnsibleTask(t, destroyTop, "Re-prove BMC claim units mounts and pool immediately before teardown")
	runtimeBlock := findAnsibleTask(t, destroyTop, "Teardown exact BMC provider-global composite")
	if !(preflight < record && record < gateAgain && gateAgain < runtimeBlock) {
		t.Fatalf("BMC teardown must re-prove record and live composite immediately before its mutation block: %d %d %d %d", preflight, record, gateAgain, runtimeBlock)
	}
	destroyTasks := nestedAnsibleTasks(t, destroyTop[runtimeBlock], "block")
	stop := findAnsibleTask(t, destroyTasks, "Stop exact loaded BMC systemd units")
	removePaths := findAnsibleTask(t, destroyTasks, "Remove exact BMC runtime paths while retaining durable claim")
	reload := findAnsibleTask(t, destroyTasks, "Reload systemd after exact BMC unit removal")
	verify := findAnsibleTask(t, destroyTasks, "Require removed BMC systemd units to be unloaded")
	endpointRelease := findAnsibleTask(t, destroyTasks, "Release all exact BMC endpoint claims after teardown proof")
	transitionRemove := findAnsibleTask(t, destroyTasks, "Atomically remove exact BMC transition after teardown")
	claimRemove := findAnsibleTask(t, destroyTasks, "Atomically remove exact BMC full claim after teardown")
	markerRemove := findAnsibleTask(t, destroyTasks, "Atomically remove exact legacy BMC marker after teardown")
	rootRemove := findAnsibleTask(t, destroyTasks, "Remove empty BMC provider-global claim root")
	if !(stop < removePaths && removePaths < reload && reload < verify && verify < endpointRelease && endpointRelease < transitionRemove && transitionRemove < claimRemove && claimRemove < markerRemove && markerRemove < rootRemove) {
		t.Fatalf("BMC teardown must retain every durable authority through runtime proof and remove exact endpoint/transition/claim/marker evidence in recovery-safe order: %d %d %d %d %d %d %d %d %d", stop, removePaths, reload, verify, endpointRelease, transitionRemove, claimRemove, markerRemove, rootRemove)
	}
	content := readRepoFile(t, destroyPath)
	for _, want := range []string{"bootwright_bmc_mount_identity.targets", "firewallManaged", "bootwright_mutating_invocation", "Recheck claimed partial BMC record absence after claim removal"} {
		if !strings.Contains(content, want) {
			t.Errorf("BMC exact teardown is missing %q", want)
		}
	}
}

func TestBMCProviderPlayAcquiresHostOperationBeforeAnySharedMutation(t *testing.T) {
	const applyPath = "ansible/collections/ansible_collections/bootwright/core/playbooks/task_provider_services_apply.yml"
	applyPlays := readAnsiblePlays(t, applyPath)
	if len(applyPlays) != 1 {
		t.Fatalf("provider apply play count = %d, want 1", len(applyPlays))
	}
	applyTasks := nestedAnsibleTasks(t, applyPlays[0], "tasks")
	skip := findAnsibleTask(t, applyTasks, "Skip when host serves no provider work")
	acquire := findAnsibleTask(t, applyTasks, "Acquire command-wide host operation before shared provider mutation")
	base := findAnsibleTask(t, applyTasks, "Apply base host packages")
	substrate := findAnsibleTask(t, applyTasks, "Apply substrate host setup roles")
	services := findAnsibleTask(t, applyTasks, "Apply provider service roles")
	if !(skip < acquire && acquire < base && base < substrate && substrate < services) {
		t.Fatalf("provider apply must acquire its command-wide host operation before base, substrate, and BMC mutations: %d %d %d %d %d", skip, acquire, base, substrate, services)
	}
	applyContent := readRepoFile(t, applyPath)
	if strings.Contains(applyContent, "tasks_from: persist") {
		t.Fatal("provider shared-service apply must not persist unclaimed host-global proxy state")
	}

	const destroyPath = "ansible/collections/ansible_collections/bootwright/core/playbooks/task_provider_services_destroy.yml"
	destroyPlays := readAnsiblePlays(t, destroyPath)
	if len(destroyPlays) != 1 {
		t.Fatalf("provider destroy play count = %d, want 1", len(destroyPlays))
	}
	destroyTasks := nestedAnsibleTasks(t, destroyPlays[0], "tasks")
	destroyAcquire := findAnsibleTask(t, destroyTasks, "Acquire command-wide host operation before shared provider teardown")
	desired := findAnsibleTask(t, destroyTasks, "Destroy provider service roles")
	orphan := findAnsibleTask(t, destroyTasks, "Destroy recorded provider resources")
	completion := findAnsibleTask(t, destroyTasks, "Finalize provider service destroy proof")
	if !(destroyAcquire < desired && desired < orphan && orphan < completion) {
		t.Fatalf("provider destroy must acquire once before desired and record-only BMC dispatch and retain it through completion proof: %d %d %d %d", destroyAcquire, desired, orphan, completion)
	}
	orphanExpr := fmt.Sprint(destroyTasks[orphan])
	for _, want := range []string{"bootwright_bmc_record_only_destroy:true", "destroy_resource.yml", "bmc-emulator"} {
		if !strings.Contains(orphanExpr, want) {
			t.Errorf("record-only BMC dispatch is missing %q: %s", want, orphanExpr)
		}
	}
}

func TestBMCCrossPortTransitionsStopOnlyExactBlockingUnits(t *testing.T) {
	const path = bootwrightCollectionRoleRoot + "/provider_service_bmc_emulated/tasks/apply/port_transition.yml"
	tasks := flattenAnsibleTasks(readAnsibleTasks(t, path))
	resolve := findAnsibleTask(t, tasks, "Resolve BMC cross-port transition stops")
	vmediaGate := findAnsibleTask(t, tasks, "Revalidate BMC authority before cross-port vmedia stop")
	vmediaStop := findAnsibleTask(t, tasks, "Stop old vmedia unit blocking desired Redfish port")
	redfishGate := findAnsibleTask(t, tasks, "Revalidate BMC authority before cross-port Redfish stop")
	redfishStop := findAnsibleTask(t, tasks, "Stop old Redfish unit blocking desired vmedia port")
	if !(resolve < vmediaGate && vmediaGate < vmediaStop && vmediaStop < redfishGate && redfishGate < redfishStop) {
		t.Fatalf("cross-port stops must be independently authority-gated before either systemd mutation: %d %d %d %d %d", resolve, vmediaGate, vmediaStop, redfishGate, redfishStop)
	}
	resolveExpr := strings.Join(strings.Fields(fmt.Sprint(tasks[resolve]["ansible.builtin.set_fact"])), " ")
	for _, want := range []string{
		"pending.attributes.redfishPort == bootwright_bmc_apply_transition_envelope.active.attributes.vMediaPort",
		"pending.attributes.vMediaPort == bootwright_bmc_apply_transition_envelope.active.attributes.redfishPort",
	} {
		if !strings.Contains(resolveExpr, want) {
			t.Errorf("cross-port transition classification is missing %q: %s", want, resolveExpr)
		}
	}
	for _, idx := range []int{vmediaStop, redfishStop} {
		if _, ok := tasks[idx]["ansible.builtin.systemd_service"]; !ok {
			t.Fatalf("cross-port task %q must stop only its exact systemd unit", tasks[idx]["name"])
		}
		if !strings.Contains(fmt.Sprint(tasks[idx]["when"]), "bootwright_bmc_loaded_unit_identities") {
			t.Fatalf("cross-port task %q is not conditioned on the exact loaded-unit proof", tasks[idx]["name"])
		}
	}
}

func TestBMCRecordOnlyDestroyBindsManifestAndEndpointAuthority(t *testing.T) {
	const bindingPath = bootwrightCollectionRoleRoot + "/provider_service_bmc_emulated/tasks/operation_binding.yml"
	bindingTasks := flattenAnsibleTasks(readAnsibleTasks(t, bindingPath))
	selection := findAnsibleTask(t, bindingTasks, "Resolve selected host consequence binding for BMC")
	recordDigest := findAnsibleTask(t, bindingTasks, "Resolve exact record-backed BMC selection digest")
	require := findAnsibleTask(t, bindingTasks, "Require exact BMC composite in selected host consequence set")
	if !(selection < recordDigest && recordDigest < require) {
		t.Fatalf("record-only BMC destroy must resolve its exact record digest before manifest authorization: %d %d %d", selection, recordDigest, require)
	}
	recordExpr := fmt.Sprint(bindingTasks[recordDigest])
	for _, want := range []string{"bootwright_bmc_record_only_destroy", "bootwright_bmc_record_claim_identity", "bootwright_bmc_consequence_digest"} {
		if !strings.Contains(recordExpr, want) {
			t.Errorf("record-backed BMC selection binding is missing %q: %s", want, recordExpr)
		}
	}
	requireExpr := fmt.Sprint(bindingTasks[require])
	for _, want := range []string{"selectionDigests", "claimDigests", "difference(bootwright_bmc_operation_claim_digests)", "bootwright_mutating_invocation"} {
		if !strings.Contains(requireExpr, want) {
			t.Errorf("BMC command-wide operation binding is missing %q: %s", want, requireExpr)
		}
	}

	const endpointPath = bootwrightCollectionRoleRoot + "/provider_service_bmc_emulated/tasks/destroy_endpoint_claims.yml"
	endpointTasks := flattenAnsibleTasks(readAnsibleTasks(t, endpointPath))
	resolve := findAnsibleTask(t, endpointTasks, "Resolve exact BMC endpoint set authorized for destroy")
	validate := findAnsibleTask(t, endpointTasks, "Require exact BMC endpoint set authorized for destroy")
	digests := findAnsibleTask(t, endpointTasks, "Resolve exact BMC destroy operation consequence digests")
	bind := findAnsibleTask(t, endpointTasks, "Bind BMC destroy to command-wide selected host consequences")
	preflight := findAnsibleTask(t, endpointTasks, "Preflight symmetric host consequences before BMC destroy authority")
	acquire := findAnsibleTask(t, endpointTasks, "Acquire exact BMC endpoint authority for resumable destroy")
	if !(resolve < validate && validate < digests && digests < bind && bind < preflight && preflight < acquire) {
		t.Fatalf("BMC orphan destroy must derive exact active/pending endpoints, bind the manifest, scan every shared-service family, then acquire authority before runtime mutation: %d %d %d %d %d %d", resolve, validate, digests, bind, preflight, acquire)
	}
	preflightExpr := fmt.Sprint(endpointTasks[preflight])
	for _, want := range []string{"preflight_host_shared_service_consequences.yml", "bootwright_host_prepublication_candidate_family:bmc-emulator", "bootwright_bmc_claim_transition", "bootwright_bmc_claim_record"} {
		if !strings.Contains(strings.ReplaceAll(preflightExpr, " ", ""), strings.ReplaceAll(want, " ", "")) {
			t.Errorf("BMC orphan destroy symmetric preflight is missing %q: %s", want, preflightExpr)
		}
	}
	if !strings.Contains(fmt.Sprint(endpointTasks[acquire]), "retainedClaims") {
		t.Fatal("BMC orphan destroy endpoint authority must retain the full active/pending transition union")
	}
}

func TestBMCFirstApplyCrashLeavesRecoverableFamilyEvidenceBeforeEndpointRegistry(t *testing.T) {
	const mainPath = bootwrightCollectionRoleRoot + "/provider_service_bmc_emulated/tasks/main.yml"
	mainTasks := flattenAnsibleTasks(readAnsibleTasks(t, mainPath))
	preflight := findAnsibleTask(t, mainTasks, "Resolve exact BMC endpoints before durable intent publication")
	family := findAnsibleTask(t, mainTasks, "Publish host-atomic BMC claim before apply mutation")
	registry := findAnsibleTask(t, mainTasks, "Reserve complete BMC endpoint union after durable claim publication")
	slots := findAnsibleTask(t, mainTasks, "Acquire exact BMC endpoint slots after durable claim publication")
	if !(preflight < family && family < registry && registry < slots) {
		t.Fatalf("a BMC first apply must leave reconstructible family authority before either aggregate or per-slot endpoint evidence: %d %d %d %d", preflight, family, registry, slots)
	}
	preflightContent := readRepoFile(t, bootwrightCollectionRoleRoot+"/provider_service_bmc_emulated/tasks/apply/endpoint_claims.yml")
	if strings.Contains(preflightContent, "reserve_host_endpoint_claims.yml") || strings.Contains(preflightContent, "acquire_host_endpoint_slots.yml") {
		t.Fatal("BMC prepublication classification must not leave endpoint-only crash evidence")
	}
	preflightTasks := flattenAnsibleTasks(readAnsibleTasks(t, bootwrightCollectionRoleRoot+"/provider_service_bmc_emulated/tasks/apply/endpoint_claims.yml"))
	bind := findAnsibleTask(t, preflightTasks, "Bind BMC apply to command-wide selected host consequences")
	scan := findAnsibleTask(t, preflightTasks, "Preflight symmetric host consequences before BMC apply publication")
	if bind >= scan {
		t.Fatalf("BMC apply must bind the selected host manifest before the symmetric prepublication scan: %d %d", bind, scan)
	}
	scanExpr := fmt.Sprint(preflightTasks[scan])
	for _, want := range []string{"preflight_host_shared_service_consequences.yml", "bootwright_host_prepublication_candidate_family:bmc-emulator", "bootwright_bmc_apply_transition_envelope", "bootwright_bmc_desired_claim"} {
		if !strings.Contains(strings.ReplaceAll(scanExpr, " ", ""), strings.ReplaceAll(want, " ", "")) {
			t.Errorf("BMC apply symmetric preflight is missing %q: %s", want, scanExpr)
		}
	}
	publishTasks := flattenAnsibleTasks(readAnsibleTasks(t, bootwrightCollectionRoleRoot+"/provider_service_bmc_emulated/tasks/publish_claim.yml"))
	claim := findAnsibleTask(t, publishTasks, "Atomically acquire or advance BMC full claim")
	if _, ok := publishTasks[claim]["bootwright.core.claim_cas"]; !ok {
		t.Fatal("the pre-endpoint BMC family authority must be host-atomic exact-content evidence")
	}
	const destroyPath = bootwrightCollectionRoleRoot + "/provider_service_bmc_emulated/tasks/destroy.yml"
	destroyTasks := flattenAnsibleTasks(readAnsibleTasks(t, destroyPath))
	probe := findAnsibleTask(t, destroyTasks, "Probe exact BMC provider-global claim before unrecorded destroy")
	resolve := findAnsibleTask(t, destroyTasks, "Resolve exact claimed partial BMC destroy authority")
	recover := findAnsibleTask(t, destroyTasks, "Destroy exact claimed partial BMC composite")
	if !(probe < resolve && resolve < recover) {
		t.Fatalf("claim-only first-apply interruption must remain executable through claimed-partial destroy recovery: %d %d %d", probe, resolve, recover)
	}
}
