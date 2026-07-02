package repocheck

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestManagedOSRefusesForeignHostRegardlessOfMode pins that a reachable managed-OS
// host without a Bootwright-owned marker fails closed even under --override (never
// adopt/wipe a foreign host), while an owned host whose hash drifted is refused only
// without --override; and that the marker/ownership stamps are gated so a host that
// failed the ownership proof is never recorded as Bootwright-owned.
func TestManagedOSRefusesForeignHostRegardlessOfMode(t *testing.T) {
	probe := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/tasks/probe_existing.yml")

	foreign := probe[findAnsibleTask(t, probe, "Refuse foreign or unmarked reachable managed OS")]
	foreignWhen := fmt.Sprint(foreign["when"])
	if strings.Contains(foreignWhen, "override") {
		t.Fatalf("foreign managed-OS refusal must fail closed regardless of apply_mode (no --override escape), got when=%v", foreign["when"])
	}
	if !strings.Contains(foreignWhen, "bootwright_os_pre_marker_owned") || !strings.Contains(foreignWhen, "bootwright_managed_os_already_ready") {
		t.Fatalf("foreign refusal must fire for a reachable host that is not Bootwright-owned, got when=%v", foreign["when"])
	}

	drifted := probe[findAnsibleTask(t, probe, "Refuse drifted Bootwright-owned managed OS without override")]
	driftedWhen := fmt.Sprint(drifted["when"])
	if !strings.Contains(driftedWhen, "override") {
		t.Fatalf("drifted owned managed-OS refusal must be relaxed by --override, got when=%v", drifted["when"])
	}
	if !strings.Contains(driftedWhen, "bootwright_os_pre_marker_owned") {
		t.Fatalf("drifted refusal must apply only to a Bootwright-owned host, got when=%v", drifted["when"])
	}

	// The marker and ownership stamps are gated so a host that failed the ownership
	// proof is never recorded as Bootwright-owned.
	main := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/tasks/main.yml")
	for _, name := range []string{"Write managed OS marker", "Record managed OS ownership"} {
		task := main[findAnsibleTask(t, main, name)]
		if got := fmt.Sprint(task["when"]); !strings.Contains(got, "bootwright_managed_os_stamp_owned") {
			t.Fatalf("%s must be gated on the ownership-proof fact, got when=%v", name, task["when"])
		}
	}
}

func TestManagedOSPlaybookUsesLinearTaskGrouping(t *testing.T) {
	var plays []map[string]any
	path := "ansible/collections/ansible_collections/bootwright/core/playbooks/task_managed_machine_os_apply.yml"
	if err := yaml.Unmarshal([]byte(readRepoFile(t, path)), &plays); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	if len(plays) != 1 {
		t.Fatalf("%s has %d plays, want 1", path, len(plays))
	}
	if got := plays[0]["strategy"]; got == "free" {
		t.Fatalf("%s must use Ansible's default linear strategy so per-task host results stay grouped", path)
	}
	if got := plays[0]["any_errors_fatal"]; got != true {
		t.Fatalf("%s must stop all selected managed OS machines when one host fails an unsafe-state guard, got any_errors_fatal=%v", path, got)
	}
}

func TestManagedOSSSHTrustKeyscanWaitsForHostKeys(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/tasks/ssh_trust.yml")
	scan := tasks[findAnsibleTask(t, tasks, "Scan managed OS SSH host key")]
	if _, ok := scan["retries"]; !ok {
		t.Fatalf("%s must retry because port 22 can open before sshd returns host keys", scan["name"])
	}
	if _, ok := scan["delay"]; !ok {
		t.Fatalf("%s must set retry delay", scan["name"])
	}
	until := fmt.Sprint(scan["until"])
	for _, want := range []string{
		"bootwright_os_ssh_keyscan_required",
		"stdout_lines",
		"reject('match', '^#')",
	} {
		if !strings.Contains(until, want) {
			t.Fatalf("%s until missing %q: %s", scan["name"], want, until)
		}
	}
	failedWhen := fmt.Sprint(scan["failed_when"])
	if !strings.Contains(failedWhen, "not in [0, 1]") {
		t.Fatalf("%s must tolerate ssh-keyscan rc=1 while waiting for keys, got %s", scan["name"], failedWhen)
	}
	record := tasks[findAnsibleTask(t, tasks, "Record managed OS SSH known_hosts entries")]
	knownHosts, ok := record["ansible.builtin.known_hosts"].(map[string]any)
	if !ok {
		t.Fatalf("%s must use known_hosts, got %v", record["name"], record)
	}
	if knownHosts["path"] != "{{ bootwright_component.osInstall.ssh.knownHostsPath }}" {
		t.Fatalf("%s path = %v", record["name"], knownHosts["path"])
	}
	if knownHosts["name"] != "{{ bootwright_component.osInstall.ssh.address }}" {
		t.Fatalf("%s name = %v", record["name"], knownHosts["name"])
	}
	if knownHosts["key"] != "{{ item }}" {
		t.Fatalf("%s key = %v, want loop item", record["name"], knownHosts["key"])
	}
	loop := fmt.Sprint(record["loop"])
	for _, want := range []string{"stdout_lines", "reject('match', '^#')", "list"} {
		if !strings.Contains(loop, want) {
			t.Fatalf("%s loop missing %q: %s", record["name"], want, loop)
		}
	}
	if strings.Contains(loop, "first") {
		t.Fatalf("%s must record every scanned key, got loop %s", record["name"], loop)
	}
	if record["delegate_to"] != "localhost" {
		t.Fatalf("%s must write controller-local trust, got delegate_to=%v", record["name"], record["delegate_to"])
	}
	restrict := tasks[findAnsibleTask(t, tasks, "Restrict managed OS SSH known_hosts file")]
	fileTask, ok := restrict["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s must set file permissions, got %v", restrict["name"], restrict)
	}
	if fileTask["path"] != "{{ bootwright_component.osInstall.ssh.knownHostsPath }}" || fileTask["mode"] != "0600" {
		t.Fatalf("%s file task = %v", restrict["name"], fileTask)
	}

	probeTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/tasks/probe_existing.yml")
	pre := probeTasks[findAnsibleTask(t, probeTasks, "Record managed OS SSH host key before install when reachable")]
	assertIncludeTasksFile(t, pre, "ssh_trust.yml")
	preVars, ok := pre["vars"].(map[string]any)
	if !ok {
		t.Fatalf("%s must pass keyscan vars", pre["name"])
	}
	if got := fmt.Sprint(preVars["bootwright_os_ssh_keyscan_required"]); got != "false" {
		t.Fatalf("%s keyscan required = %s, want false", pre["name"], got)
	}

	waitTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/tasks/wait.yml")
	post := waitTasks[findAnsibleTask(t, waitTasks, "Record managed OS SSH host key")]
	assertIncludeTasksFile(t, post, "ssh_trust.yml")
	postVars, ok := post["vars"].(map[string]any)
	if !ok {
		t.Fatalf("%s must pass keyscan vars", post["name"])
	}
	for key, want := range map[string]string{
		"bootwright_os_ssh_keyscan_required": "true",
		"bootwright_os_ssh_keyscan_retries":  "60",
		"bootwright_os_ssh_keyscan_delay":    "10",
	} {
		if got := fmt.Sprint(postVars[key]); got != want {
			t.Fatalf("%s %s = %s, want %s", post["name"], key, got, want)
		}
	}
}

func TestManagedOSAnacondaInstallsMkksisoPackage(t *testing.T) {
	topTasks := managedOSAnacondaTasks(t)
	validateInputsIdx := findAnsibleTask(t, topTasks, "Validate managed Anaconda install inputs")
	validateSourceIdx := findAnsibleTask(t, topTasks, "Validate managed Anaconda install source")
	resolvePathsIdx := findAnsibleTask(t, topTasks, "Resolve managed OS install paths")
	resolveVirtualMediaPathsIdx := findAnsibleTask(t, topTasks, "Resolve managed OS virtual media paths")
	resolveFilesIdx := findAnsibleTask(t, topTasks, "Resolve managed OS install files")
	resolveSourcePathIdx := findAnsibleTask(t, topTasks, "Resolve managed OS install source path")
	readMarkerIdx := findAnsibleTask(t, topTasks, "Read managed OS install marker before install")
	refuseMarkerIdx := findAnsibleTask(t, topTasks, "Refuse foreign or unmarked reachable managed OS")
	installBlockIdx := findAnsibleTask(t, topTasks, "Install managed OS from virtual media")
	waitSSHIdx := findAnsibleTask(t, topTasks, "Wait for managed OS SSH port")
	cleanupMediaIdx := findAnsibleTask(t, topTasks, "Clean managed OS virtual media after SSH is ready")
	baremetalEjectIdx := findAnsibleTask(t, topTasks, "Eject Redfish virtual media after SSH is ready")
	recordHostKeyIdx := findAnsibleTask(t, topTasks, "Record managed OS SSH host key")
	verifySSHIdx := findAnsibleTask(t, topTasks, "Verify managed OS SSH authentication")
	writeMarkerIdx := findAnsibleTask(t, topTasks, "Write managed OS install marker")
	validateInputs, ok := topTasks[validateInputsIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an assert task", topTasks[validateInputsIdx]["name"])
	}
	if got := fmt.Sprint(validateInputs["that"]); !strings.Contains(got, "osInstall.marker.path") || !strings.Contains(got, "osInstall.marker.desiredHash") {
		t.Fatalf("Anaconda input validation must require marker path and desired hash, got %v", validateInputs["that"])
	}
	if validateSourceIdx >= resolvePathsIdx {
		t.Fatalf("Anaconda role must validate install source before resolving install paths")
	}
	if !(resolvePathsIdx < resolveVirtualMediaPathsIdx && resolveVirtualMediaPathsIdx < resolveFilesIdx && resolveFilesIdx < resolveSourcePathIdx) {
		t.Fatalf("Anaconda role must resolve virtual media paths before install files and the effective source path")
	}
	if !(readMarkerIdx < refuseMarkerIdx && refuseMarkerIdx < installBlockIdx) {
		t.Fatalf("Anaconda role must check the managed OS marker before the install block")
	}
	if !(installBlockIdx < waitSSHIdx && waitSSHIdx < cleanupMediaIdx && cleanupMediaIdx < baremetalEjectIdx && baremetalEjectIdx < recordHostKeyIdx) {
		t.Fatalf("Anaconda role must let Kickstart reboot, wait for SSH, then clean media and eject Redfish virtual media before recording the host key")
	}
	if !(recordHostKeyIdx < verifySSHIdx && verifySSHIdx < writeMarkerIdx) {
		t.Fatalf("Anaconda role must write the managed OS marker after SSH verification")
	}
	assertIncludeRoleName(t, topTasks[cleanupMediaIdx], "{{ bootwright_component.mediaPrepareRole }}")
	cleanupVars, ok := topTasks[cleanupMediaIdx]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("%s must pass cleanup vars, got %v", topTasks[cleanupMediaIdx]["name"], topTasks[cleanupMediaIdx])
	}
	if cleanupVars["bootwright_component"] != "{{ bootwright_managed_os_boot_component }}" || cleanupVars["bootwright_redfish_action_effective"] != "cleanup" {
		t.Fatalf("%s must clean resolved managed OS media, got vars=%v", topTasks[cleanupMediaIdx]["name"], cleanupVars)
	}
	// Bare metal has no mediaPrepareRole, so the cleanup above is skipped; the
	// install role must still eject the BMC virtual media via the boot role's
	// cleanup_media action or it lingers as a /dev/sr0.
	assertIncludeRoleName(t, topTasks[baremetalEjectIdx], "{{ bootwright_component.bootApplyRole }}")
	baremetalVars, ok := topTasks[baremetalEjectIdx]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("%s must pass cleanup vars, got %v", topTasks[baremetalEjectIdx]["name"], topTasks[baremetalEjectIdx])
	}
	if baremetalVars["bootwright_component"] != "{{ bootwright_managed_os_boot_component }}" || baremetalVars["bootwright_redfish_action"] != "cleanup_media" {
		t.Fatalf("%s must eject resolved managed OS media via cleanup_media, got vars=%v", topTasks[baremetalEjectIdx]["name"], baremetalVars)
	}
	if got := fmt.Sprint(topTasks[baremetalEjectIdx]["when"]); !strings.Contains(got, "bootwright_managed_os_boot_component is defined") || !strings.Contains(got, "container_cluster_boot_redfish") || !strings.Contains(got, "mediaPrepareRole") {
		t.Fatalf("%s must run only for boot_redfish machines without a mediaPrepareRole on the install run, got when=%v", topTasks[baremetalEjectIdx]["name"], topTasks[baremetalEjectIdx]["when"])
	}
	validateSource, ok := topTasks[validateSourceIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an assert task", topTasks[validateSourceIdx]["name"])
	}
	if got := fmt.Sprint(validateSource["that"]); !strings.Contains(got, "mediaType") || !strings.Contains(got, "installer.sourceURL") || !strings.Contains(got, "installer.rhsm.enabled") {
		t.Fatalf("Anaconda install source validation must reject boot media without sourceURL or RHSM, got %v", validateSource["that"])
	}
	resolveFiles, ok := topTasks[resolveFilesIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", topTasks[resolveFilesIdx]["name"])
	}
	for _, want := range []string{"bootwright_os_source_iso", "bootwright_os_source_id_path", "bootwright_os_install_iso", "bootwright_os_legacy_install_iso", "bootwright_os_install_tmpdir", "bootwright_os_marker_path", "bootwright_os_marker_desired_hash", "bootwright_os_marker_payload"} {
		if _, ok := resolveFiles[want]; !ok {
			t.Fatalf("%s missing %s", topTasks[resolveFilesIdx]["name"], want)
		}
	}
	if resolveFiles["bootwright_os_install_iso"] != "{{ bootwright_os_stage_path }}" {
		t.Fatalf("%s must build the attach ISO directly at the virtual media stage path, got %v", topTasks[resolveFilesIdx]["name"], resolveFiles["bootwright_os_install_iso"])
	}
	if resolveFiles["bootwright_os_legacy_install_iso"] != "{{ bootwright_os_install_root }}/install.iso" {
		t.Fatalf("%s legacy private install ISO got %v", topTasks[resolveFilesIdx]["name"], resolveFiles["bootwright_os_legacy_install_iso"])
	}
	resolveSourcePath, ok := topTasks[resolveSourcePathIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", topTasks[resolveSourcePathIdx]["name"])
	}
	sourcePathExpr := fmt.Sprint(resolveSourcePath["bootwright_os_source_iso_effective"])
	if !strings.Contains(sourcePathExpr, "bootwright_component.osInstall.image.effectiveSourcePath") {
		t.Fatalf("%s must consume the renderer-emitted effective source path, got %s", topTasks[resolveSourcePathIdx]["name"], sourcePathExpr)
	}

	tasks := nestedAnsibleTasks(t, topTasks[installBlockIdx], "block")
	loadIdx := findAnsibleTask(t, tasks, "Load Anaconda package list")
	installIdx := findAnsibleTask(t, tasks, "Install Anaconda ISO tooling packages")
	verifyIdx := findAnsibleTask(t, tasks, "Verify mkksiso is available")
	assertIdx := findAnsibleTask(t, tasks, "Assert mkksiso is available")
	if !(loadIdx < installIdx && installIdx < verifyIdx && verifyIdx < assertIdx) {
		t.Fatalf("Anaconda role must install mkksiso packages before verifying and asserting mkksiso")
	}
	pkg, ok := tasks[installIdx]["ansible.builtin.package"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a package task", tasks[installIdx]["name"])
	}
	if got, _ := pkg["name"].(string); got != "{{ bootwright_machine_os_install_anaconda_packages }}" {
		t.Fatalf("%s package name got %q", tasks[installIdx]["name"], got)
	}
	mkksisoProbe, ok := tasks[verifyIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a command task", tasks[verifyIdx]["name"])
	}
	for _, want := range []string{"mkksiso", "--help"} {
		if !stringListContains(mkksisoProbe["argv"], want) {
			t.Fatalf("%s argv missing %q: %v", tasks[verifyIdx]["name"], want, mkksisoProbe["argv"])
		}
	}
	if got := tasks[verifyIdx]["failed_when"]; got != false {
		t.Fatalf("%s must leave failure reporting to the assert task, got %v", tasks[verifyIdx]["name"], got)
	}
	mkksisoAssert, ok := tasks[assertIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an assert task", tasks[assertIdx]["name"])
	}
	if !stringListContains(mkksisoAssert["that"], "bootwright_mkksiso_probe.rc == 0") {
		t.Fatalf("%s must assert the mkksiso probe result, got %v", tasks[assertIdx]["name"], mkksisoAssert["that"])
	}
	copyIdx := findAnsibleTask(t, tasks, "Copy managed ISO source to provider host")
	copyTask, ok := tasks[copyIdx]["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a copy task", tasks[copyIdx]["name"])
	}
	if got := copyTask["remote_src"]; got != false {
		t.Fatalf("%s must not remote-copy provider-local sources, got remote_src=%v", tasks[copyIdx]["name"], got)
	}
	if got := tasks[copyIdx]["when"]; !stringListContains(got, "bootwright_component.osInstall.image.kind in ['media', 'file']") || !stringListContains(got, "not (bootwright_component.osInstall.image.sourceOnTarget | default(false) | bool)") {
		t.Fatalf("%s must skip sources already present on the provider host, got when=%v", tasks[copyIdx]["name"], got)
	}
	downloadIdx := findAnsibleTask(t, tasks, "Download managed ISO source on provider host")
	sourceStatIdx := findAnsibleTask(t, tasks, "Stat managed ISO source")
	sourceIdentityIdx := findAnsibleTask(t, tasks, "Record managed ISO source identity")
	checksumIdx := findAnsibleTask(t, tasks, "Verify managed ISO checksum")
	createMediaDirIdx := findAnsibleTask(t, tasks, "Create managed OS virtual media directory")
	removeLegacyIdx := findAnsibleTask(t, tasks, "Remove legacy private managed OS install ISO path")
	statISOIdx := findAnsibleTask(t, tasks, "Stat managed OS install ISO")
	rebuildStateIdx := findAnsibleTask(t, tasks, "Resolve managed OS install ISO rebuild state")
	removeStaleIdx := findAnsibleTask(t, tasks, "Remove stale managed OS install ISO before rebuild")
	resetTmpIdx := findAnsibleTask(t, tasks, "Reset managed OS install ISO temp directory before rebuild")
	createTmpIdx := findAnsibleTask(t, tasks, "Create managed OS install ISO temp directory")
	buildISOIdx := findAnsibleTask(t, tasks, "Build managed OS install ISO")
	buildISOWithCmdlineIdx := findAnsibleTask(t, tasks, "Build managed OS install ISO with kernel command line")
	if !(copyIdx < sourceStatIdx && downloadIdx < sourceStatIdx && sourceStatIdx < sourceIdentityIdx && sourceIdentityIdx < checksumIdx && sourceIdentityIdx < createMediaDirIdx && createMediaDirIdx < removeLegacyIdx && removeLegacyIdx < statISOIdx) {
		t.Fatalf("Anaconda role must resolve source metadata before checksum and rebuild decisions")
	}
	sourceStat, ok := tasks[sourceStatIdx]["ansible.builtin.stat"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a stat task", tasks[sourceStatIdx]["name"])
	}
	if sourceStat["path"] != "{{ bootwright_os_source_iso_effective }}" || sourceStat["get_checksum"] != false {
		t.Fatalf("%s must stat the effective source without checksumming it, got %v", tasks[sourceStatIdx]["name"], sourceStat)
	}
	sourceIdentity, ok := tasks[sourceIdentityIdx]["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a copy task", tasks[sourceIdentityIdx]["name"])
	}
	sourceIdentityContent := fmt.Sprint(sourceIdentity["content"])
	for _, want := range []string{"bootwright_os_source_iso_effective", "bootwright_os_source_stat.stat.size", "bootwright_os_source_stat.stat.mtime", "kernelArgs"} {
		if !strings.Contains(sourceIdentityContent, want) {
			t.Fatalf("%s source identity missing %q: %s", tasks[sourceIdentityIdx]["name"], want, sourceIdentityContent)
		}
	}
	createMediaDir, ok := tasks[createMediaDirIdx]["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a file task", tasks[createMediaDirIdx]["name"])
	}
	if createMediaDir["path"] != "{{ bootwright_os_install_iso | dirname }}" || createMediaDir["state"] != "directory" || createMediaDir["mode"] != "0755" {
		t.Fatalf("%s must create the published virtual media directory, got %v", tasks[createMediaDirIdx]["name"], createMediaDir)
	}
	removeLegacy, ok := tasks[removeLegacyIdx]["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a file task", tasks[removeLegacyIdx]["name"])
	}
	if removeLegacy["path"] != "{{ bootwright_os_legacy_install_iso }}" || removeLegacy["state"] != "absent" {
		t.Fatalf("%s must remove the old private attach ISO path, got %v", tasks[removeLegacyIdx]["name"], removeLegacy)
	}
	checksumCommand, ok := tasks[checksumIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a command task", tasks[checksumIdx]["name"])
	}
	if got := fmt.Sprint(checksumCommand["argv"]); !strings.Contains(got, "bootwright_os_source_iso_effective") {
		t.Fatalf("%s must checksum the effective source ISO, got argv=%v", tasks[checksumIdx]["name"], checksumCommand["argv"])
	}
	statISO, ok := tasks[statISOIdx]["ansible.builtin.stat"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a stat task", tasks[statISOIdx]["name"])
	}
	if statISO["path"] != "{{ bootwright_os_install_iso }}" || statISO["get_checksum"] != false {
		t.Fatalf("%s must stat install.iso without checksumming it, got %v", tasks[statISOIdx]["name"], statISO)
	}
	if !(statISOIdx < rebuildStateIdx && rebuildStateIdx < removeStaleIdx && removeStaleIdx < resetTmpIdx && resetTmpIdx < createTmpIdx && createTmpIdx < buildISOIdx && buildISOIdx < buildISOWithCmdlineIdx) {
		t.Fatalf("Anaconda role must remove stale install.iso and reset temp state before mkksiso")
	}
	rebuildFact, ok := tasks[rebuildStateIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", tasks[rebuildStateIdx]["name"])
	}
	rebuildExpr := fmt.Sprint(rebuildFact["bootwright_os_install_iso_rebuild_needed"])
	for _, want := range []string{
		"bootwright_os_source_copy.changed",
		"bootwright_os_source_download.changed",
		"bootwright_os_source_identity.changed",
		"bootwright_os_kickstart.changed",
		"bootwright_os_install_iso_stat.stat.exists",
	} {
		if !strings.Contains(rebuildExpr, want) {
			t.Fatalf("%s rebuild expression missing %q: %s", tasks[rebuildStateIdx]["name"], want, rebuildExpr)
		}
	}
	if got := tasks[rebuildStateIdx]["changed_when"]; got != false {
		t.Fatalf("%s must not report changes, got %v", tasks[rebuildStateIdx]["name"], got)
	}
	removeStale, ok := tasks[removeStaleIdx]["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a file task", tasks[removeStaleIdx]["name"])
	}
	if removeStale["path"] != "{{ bootwright_os_install_iso }}" || removeStale["state"] != "absent" {
		t.Fatalf("%s must remove bootwright_os_install_iso, got %v", tasks[removeStaleIdx]["name"], removeStale)
	}
	if got := tasks[removeStaleIdx]["when"]; !stringListContains(got, "bootwright_os_install_iso_rebuild_needed | bool") || !stringListContains(got, "bootwright_os_install_iso_stat.stat.exists | default(false)") {
		t.Fatalf("%s must only remove an existing ISO when rebuild is needed, got when=%v", tasks[removeStaleIdx]["name"], got)
	}
	if got := tasks[buildISOIdx]["when"]; !stringListContains(got, "bootwright_os_install_iso_rebuild_needed | bool") || !stringListContains(got, "(bootwright_component.osInstall.installer.kernelArgs | default([]) | length) == 0") {
		t.Fatalf("%s must run only when rebuild is needed and kernel args are empty, got when=%v", tasks[buildISOIdx]["name"], got)
	}
	buildCommand, ok := tasks[buildISOIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a command task", tasks[buildISOIdx]["name"])
	}
	if got := fmt.Sprint(buildCommand["argv"]); !strings.Contains(got, "bootwright_os_source_iso_effective") {
		t.Fatalf("%s must use the effective source ISO, got argv=%v", tasks[buildISOIdx]["name"], buildCommand["argv"])
	}
	if stringListContains(buildCommand["argv"], "--cmdline") {
		t.Fatalf("%s must not pass --cmdline for empty kernel args, got argv=%v", tasks[buildISOIdx]["name"], buildCommand["argv"])
	}
	buildEnv, ok := tasks[buildISOIdx]["environment"].(map[string]any)
	if !ok {
		t.Fatalf("%s must set mkksiso temp environment", tasks[buildISOIdx]["name"])
	}
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		if got := buildEnv[key]; got != "{{ bootwright_os_install_tmpdir }}" {
			t.Fatalf("%s environment %s got %v, want bootwright_os_install_tmpdir", tasks[buildISOIdx]["name"], key, got)
		}
	}
	if got := tasks[buildISOWithCmdlineIdx]["when"]; !stringListContains(got, "bootwright_os_install_iso_rebuild_needed | bool") || !stringListContains(got, "(bootwright_component.osInstall.installer.kernelArgs | default([]) | length) > 0") {
		t.Fatalf("%s must run only when rebuild is needed and kernel args are present, got when=%v", tasks[buildISOWithCmdlineIdx]["name"], got)
	}
	buildWithCmdlineCommand, ok := tasks[buildISOWithCmdlineIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a command task", tasks[buildISOWithCmdlineIdx]["name"])
	}
	for _, want := range []string{"mkksiso", "--ks", "--cmdline", "bootwright_component.osInstall.installer.kernelArgs | join(' ')", "bootwright_os_source_iso_effective", "bootwright_os_install_iso"} {
		if !strings.Contains(fmt.Sprint(buildWithCmdlineCommand["argv"]), want) {
			t.Fatalf("%s argv missing %q: %v", tasks[buildISOWithCmdlineIdx]["name"], want, buildWithCmdlineCommand["argv"])
		}
	}
	buildWithCmdlineEnv, ok := tasks[buildISOWithCmdlineIdx]["environment"].(map[string]any)
	if !ok {
		t.Fatalf("%s must set mkksiso temp environment", tasks[buildISOWithCmdlineIdx]["name"])
	}
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		if got := buildWithCmdlineEnv[key]; got != "{{ bootwright_os_install_tmpdir }}" {
			t.Fatalf("%s environment %s got %v, want bootwright_os_install_tmpdir", tasks[buildISOWithCmdlineIdx]["name"], key, got)
		}
	}
	if findAnsibleTaskIndex(tasks, "Stage managed OS install ISO for virtual media") >= 0 {
		t.Fatalf("Anaconda role must not stage install.iso with ansible.builtin.copy")
	}
	for _, forbidden := range []string{
		"Stat managed OS install ISO for virtual media stage",
		"Stat managed OS staged virtual media ISO",
		"Resolve managed OS virtual media stage state",
		"Link managed OS install ISO into virtual media stage",
		"Copy managed OS install ISO into virtual media stage when linking is unsupported",
	} {
		if findAnsibleTaskIndex(tasks, forbidden) >= 0 {
			t.Fatalf("Anaconda role must build directly into virtual media stage instead of running %q", forbidden)
		}
	}
	stagePermsIdx := findAnsibleTask(t, tasks, "Set managed OS virtual media permissions")
	dirLabelIdx := findAnsibleTask(t, tasks, "Align managed OS virtual media directory label with its publish root")
	fileLabelIdx := findAnsibleTask(t, tasks, "Align managed OS virtual media label with its staging directory")
	resolveBootComponentIdx := findAnsibleTask(t, tasks, "Resolve managed OS Redfish boot component")
	prepareMediaIdx := findAnsibleTask(t, tasks, "Prepare provider virtual media before managed OS boot")
	bootMediaIdx := findAnsibleTask(t, tasks, "Boot managed OS installer through Redfish virtual media")
	persistentCleanupIdx := findAnsibleTask(t, tasks, "Clean managed OS persistent virtual media after installer boot")
	if !(buildISOWithCmdlineIdx < stagePermsIdx && stagePermsIdx < dirLabelIdx && dirLabelIdx < fileLabelIdx && fileLabelIdx < resolveBootComponentIdx && resolveBootComponentIdx < prepareMediaIdx && prepareMediaIdx < bootMediaIdx && bootMediaIdx < persistentCleanupIdx) {
		t.Fatalf("Anaconda role must resolve tokenized boot component before media preparation, boot, and persistent cleanup")
	}
	stagePerms, ok := tasks[stagePermsIdx]["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a file task", tasks[stagePermsIdx]["name"])
	}
	if stagePerms["path"] != "{{ bootwright_os_install_iso }}" || stagePerms["state"] != "file" || stagePerms["mode"] != "{{ '0600' if ((bootwright_component.osInstall.installer.rhsm.enabled | default(false) | bool) or (bootwright_component.osInstall.installer.proxy.credentialsPath | default('') | length > 0)) else '0644' }}" {
		t.Fatalf("%s must set permissions on the published install ISO, got %v", tasks[stagePermsIdx]["name"], stagePerms)
	}
	// The staged ISO must inherit its publish directory's SELinux label (the
	// artifact server's :Z container_file_t for bare-metal), NOT be reset to the
	// filesystem default with restorecon -- otherwise the nginx container cannot
	// read it and the BMC fetch 404s. Mirror the agent-ISO publish flow's chcon
	// --reference alignment (dir <- publish root, ISO <- dir).
	for _, tc := range []struct {
		idx       int
		reference string
		target    string
	}{
		{dirLabelIdx, "--reference={{ bootwright_os_install_iso | dirname | dirname }}", "{{ bootwright_os_install_iso | dirname }}"},
		{fileLabelIdx, "--reference={{ bootwright_os_install_iso | dirname }}", "{{ bootwright_os_install_iso }}"},
	} {
		labelCmd, ok := tasks[tc.idx]["ansible.builtin.command"].(map[string]any)
		if !ok {
			t.Fatalf("%s is not a command task", tasks[tc.idx]["name"])
		}
		labelArgv := fmt.Sprint(labelCmd["argv"])
		for _, want := range []string{"chcon", tc.reference, tc.target} {
			if !strings.Contains(labelArgv, want) {
				t.Fatalf("%s argv missing %q: %v", tasks[tc.idx]["name"], want, labelCmd["argv"])
			}
		}
		if strings.Contains(labelArgv, "restorecon") {
			t.Fatalf("%s must align labels with chcon --reference, not restorecon (which resets to var_lib_t and 404s the nginx :Z mount): %v", tasks[tc.idx]["name"], labelCmd["argv"])
		}
		if got := tasks[tc.idx]["failed_when"]; got != false {
			t.Fatalf("%s must tolerate hosts without chcon, got failed_when=%v", tasks[tc.idx]["name"], got)
		}
		if got := tasks[tc.idx]["when"]; got != "ansible_selinux.status | default('disabled') == 'enabled'" {
			t.Fatalf("%s must only run with SELinux enabled, got when=%v", tasks[tc.idx]["name"], got)
		}
	}
	if findAnsibleTaskIndex(tasks, "Restore managed OS virtual media labels") >= 0 {
		t.Fatalf("Anaconda role must not restorecon the staged ISO (resets to var_lib_t, unreadable by the nginx :Z container)")
	}
	resolveBootComponent, ok := tasks[resolveBootComponentIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", tasks[resolveBootComponentIdx]["name"])
	}
	resolveBootExpr := fmt.Sprint(resolveBootComponent["bootwright_managed_os_boot_component"])
	for _, want := range []string{"bootwright_os_stage_path", "bootwright_os_fetch_url", "bootOrder", "disk-first"} {
		if !strings.Contains(resolveBootExpr, want) {
			t.Fatalf("%s must resolve %q before media preparation: %s", tasks[resolveBootComponentIdx]["name"], want, resolveBootExpr)
		}
	}
	prepareVars, ok := tasks[prepareMediaIdx]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("%s must pass resolved component vars, got %v", tasks[prepareMediaIdx]["name"], tasks[prepareMediaIdx])
	}
	if prepareVars["bootwright_component"] != "{{ bootwright_managed_os_boot_component }}" {
		t.Fatalf("%s must use resolved managed OS boot component, got vars=%v", tasks[prepareMediaIdx]["name"], prepareVars)
	}
	persistentCleanupVars, ok := tasks[persistentCleanupIdx]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("%s must pass persistent cleanup vars, got %v", tasks[persistentCleanupIdx]["name"], tasks[persistentCleanupIdx])
	}
	if persistentCleanupVars["bootwright_component"] != "{{ bootwright_managed_os_boot_component }}" || persistentCleanupVars["bootwright_redfish_action_effective"] != "cleanup_persistent" {
		t.Fatalf("%s must clean only persistent managed OS media, got vars=%v", tasks[persistentCleanupIdx]["name"], persistentCleanupVars)
	}
	redHat := readAnsibleStringListVar(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/vars/os/RedHat.yml", "bootwright_machine_os_install_anaconda_packages")
	assertContainsAll(t, redHat, []string{"lorax"})
}

func TestManagedOSKickstartTemplateKeepsSSHKeyConditionalParseable(t *testing.T) {
	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/templates/ks.cfg.j2")
	if strings.Contains(body, "lookup('ansible.builtin.file', ks.sshPublicKeyPath) if") {
		t.Fatalf("Kickstart template must not use an inline conditional around the SSH key lookup")
	}
	for _, want := range []string{
		"reboot",
		"{% set rhsm = installer.rhsm | default({}) %}",
		"{% if rhsm.enabled | default(false) %}",
		"rhsm --organization=\"{{ lookup('ansible.builtin.file', rhsm.organizationPath) | trim }}\" --activation-key=\"{{ lookup('ansible.builtin.file', rhsm.activationKeyPath) | trim }}\"{{ sat_flag }}{{ proxy_flag }}{{ insights_flag }}",
		"{% elif installer.sourceURL | default('') | length > 0 %}",
		"url --url={{ installer.sourceURL }}{{ proxy_flag }}",
		"{% else %}",
		"cdrom",
		"{% set satellite = rhsm.satellite | default({}) %}",
		"{% set proxy_flag = (' --proxy=' ~ proxy_url) if (proxy_url | length > 0) else '' %}",
		"{% if sat_enabled %}{% set sat_flag = ' --server-hostname=' ~ satellite.hostname %}{% if satellite.contentBaseURL | default('') | length > 0 %}{% set sat_flag = sat_flag ~ ' --rhsm-baseurl=' ~ satellite.contentBaseURL %}{% endif %}{% endif %}",
		"%pre --erroronfail",
		"cat > /etc/pki/ca-trust/source/anchors/bootwright-satellite-ca.pem <<'BOOTWRIGHT_SATELLITE_CA'",
		"update-ca-trust extract",
		"bootloader --location=mbr --boot-drive={{ storage.rootDisk }}",
		"part swap --recommended --ondisk={{ storage.rootDisk }}",
		"part / --fstype=xfs --size=10240 --grow --ondisk={{ storage.rootDisk }}",
		"selinux --{{ selinux.mode }}",
		"firewall --{{ 'enabled' if firewall.enabled else 'disabled' }}",
		"{% set net_interfaces = net.interfaces | default([]) %}",
		"{% for iface in net_interfaces %}",
		"--bondslaves={{ iface.bondSlaves | join(',') }}",
		"--bondopts={{ iface.bondOptions }}",
		"--vlanid={{ iface.vlanID }}",
		"--interfacename={{ iface.interfaceName }}",
		"--noipv4 --noipv6",
		"{{ hostname_flag }}",
		"services{{ svc_opts }}",
		"{% if enabled_services | length > 0 %}{% set svc_opts = svc_opts ~ ' --enabled=' ~ (enabled_services | join(',')) %}{% endif %}",
		"{% set ssh_key = '' %}",
		"{% if (ks.authorizeMachineSSHKey | default(false)) and (ks.sshPublicKeyPath | default('') | length > 0) %}",
		"{% set ssh_key = lookup('ansible.builtin.file', ks.sshPublicKeyPath) %}",
		"%packages{{ pkg_opts }}",
		"{% if packages.languages | default([]) | length > 0 %}{% set pkg_opts = pkg_opts ~ ' --inst-langs=' ~ (packages.languages | join(',')) %}{% endif %}",
		"@^{{ packages.environment | default('minimal') }}-environment",
		"{% set marker = bootwright_component.osInstall.marker | default({}) %}",
		"cat > {{ marker.path }} <<'BOOTWRIGHT_INSTALL_MARKER'",
		"{{ marker | to_nice_json }}",
		// An omitted ssh.user is an empty (defined) string, which `default('root')`
		// would NOT replace; the `, true` makes it fall back to root so the sshkey /
		// user lines never render an empty username.
		"sshkey --username={{ ks.sshUser | default('root', true) }}",
		"{% if (ks.sshUser | default('root', true)) != 'root' %}",
		// urlencode leaves '/' unescaped (safe='/'); a credential containing '/'
		// must be %2F-encoded or it terminates the proxy URL authority early.
		"| urlencode | replace('/', '%2F')",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Kickstart template missing %q", want)
		}
	}
	if strings.Contains(body, "systemctl enable sshd") {
		t.Fatalf("Kickstart template must not force-enable sshd outside customizations.services.enabled")
	}
	for _, forbidden := range []string{"reboot --eject", "poweroff"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Kickstart template must rely on boot control, not %q", forbidden)
		}
	}
}

// TestManagedOSKickstartTemplateNoCommandGluedToBlockTag guards the whole class
// of bug behind `rhsm ...:8080lang en_US.UTF-8` and `%packages ...@^...-environment`:
// Ansible renders this template with trim_blocks=True, which strips the newline
// after every {% ... %} tag. A line that mixes literal kickstart content (a command
// or a {{ expression }}) with a TRAILING block tag therefore loses its newline and
// the next kickstart command collapses onto it. Every command line must end in
// literal text or a {{ expression }}; only pure control lines may end in a tag.
func TestManagedOSKickstartTemplateNoCommandGluedToBlockTag(t *testing.T) {
	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/templates/ks.cfg.j2")
	blockTag := regexp.MustCompile(`\{%.*?%\}`)
	commentTag := regexp.MustCompile(`\{#.*?#\}`)
	for i, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimRight(line, " \t")
		if !strings.HasSuffix(trimmed, "%}") {
			continue
		}
		// Strip control/comment blocks; whatever remains is literal content or a
		// {{ expression }} that trim_blocks would glue onto the following line.
		residue := strings.TrimSpace(commentTag.ReplaceAllString(blockTag.ReplaceAllString(trimmed, ""), ""))
		if residue != "" {
			t.Fatalf("ks.cfg.j2:%d ends in a {%% %%} tag with trailing content %q; trim_blocks will glue the next line onto it — end command lines in literal text or a {{ expression }} instead", i+1, residue)
		}
	}
}

func managedOSAnacondaTasks(t *testing.T) []map[string]any {
	t.Helper()
	base := "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/tasks/"
	return readAnsibleTasksFromFiles(t,
		base+"validate.yml",
		base+"resolve.yml",
		base+"probe_existing.yml",
		base+"install_media.yml",
		base+"wait.yml",
		base+"marker.yml",
		base+"ownership.yml",
	)
}
