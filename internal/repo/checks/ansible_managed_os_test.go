package repocheck

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestManagedOSRefusesForeignHostRegardlessOfMode(t *testing.T) {
	probe := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_identity/tasks/probe_existing.yml")

	foreign := probe[findAnsibleTask(t, probe, "Refuse foreign or unmarked reachable managed OS")]
	foreignWhen := fmt.Sprint(foreign["when"])
	if strings.Contains(foreignWhen, "override") {
		t.Fatalf("foreign managed-OS refusal must fail closed regardless of apply_mode (no --mode rebuild escape), got when=%v", foreign["when"])
	}
	if !strings.Contains(foreignWhen, "bootwright_os_pre_marker_owned") || !strings.Contains(foreignWhen, "bootwright_managed_os_already_ready") {
		t.Fatalf("foreign refusal must fire for a reachable host that is not Bootwright-owned, got when=%v", foreign["when"])
	}
	foreignMessage := fmt.Sprint(foreign["ansible.builtin.fail"])
	for _, want := range []string{"verified backup", "out of band", "No bootwright retry command", "bootwright_mutating_invocation"} {
		if !strings.Contains(foreignMessage, want) {
			t.Fatalf("foreign refusal must name the sanctioned external recovery and exact post-repair invocation; missing %q in %s", want, foreignMessage)
		}
	}

	create := probe[findAnsibleTask(t, probe, "Refuse a pre-existing managed OS under --mode create")]
	createWhen := fmt.Sprint(create["when"])
	if !strings.Contains(createWhen, "bootwright_os_pre_marker_owned") {
		t.Fatalf("create-mode owned-state remedy must not preempt the foreign-ownership refusal, got when=%v", create["when"])
	}
	createMessage := fmt.Sprint(create["ansible.builtin.fail"])
	for _, want := range []string{"greenfield", "bootwright_apply_reconcile_invocation"} {
		if !strings.Contains(createMessage, want) {
			t.Fatalf("create-mode refusal must name the exact reconcile invocation; missing %q in %s", want, createMessage)
		}
	}

	drifted := probe[findAnsibleTask(t, probe, "Refuse drifted Bootwright-owned managed OS without override")]
	driftedWhen := fmt.Sprint(drifted["when"])
	if !strings.Contains(driftedWhen, "'rebuild'") {
		t.Fatalf("drifted owned managed-OS refusal must be relaxed by --mode rebuild, got when=%v", drifted["when"])
	}
	if !strings.Contains(driftedWhen, "bootwright_os_pre_marker_owned") {
		t.Fatalf("drifted refusal must apply only to a Bootwright-owned host, got when=%v", drifted["when"])
	}
	driftedMessage := fmt.Sprint(drifted["ansible.builtin.fail"])
	if !strings.Contains(driftedMessage, "bootwright_apply_rebuild_invocation") {
		t.Fatalf("drifted managed-OS refusal must name the exact selected rebuild invocation, got %s", driftedMessage)
	}

	for _, role := range []string{"machine_os_install_anaconda", "machine_os_install_clone"} {
		main := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/"+role+"/tasks/main.yml")
		if idx := findAnsibleTaskIndex(main, "Probe existing managed OS state"); idx < 0 {
			t.Fatalf("%s must probe existing managed OS state before installing", role)
		}
		for _, name := range []string{"Write managed OS marker", "Record managed OS ownership"} {
			task := main[findAnsibleTask(t, main, name)]
			if got := fmt.Sprint(task["when"]); !strings.Contains(got, "bootwright_managed_os_stamp_owned") {
				t.Fatalf("%s %s must be gated on the ownership-proof fact, got when=%v", role, name, task["when"])
			}
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
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_identity/tasks/ssh_trust.yml")
	if findAnsibleTask(t, tasks, "Ensure managed OS SSH trust directory") >= findAnsibleTask(t, tasks, "Scan managed OS SSH host key") {
		t.Fatalf("the trust directory must be ensured before the host-key scan writes into it")
	}
	scan := tasks[findAnsibleTask(t, tasks, "Scan managed OS SSH host key")]
	if _, ok := scan["retries"]; !ok {
		t.Fatalf("%s must retry because port 22 can open before sshd returns host keys", scan["name"])
	}
	if _, ok := scan["delay"]; !ok {
		t.Fatalf("%s must set retry delay", scan["name"])
	}
	if scan["delegate_to"] != "localhost" {
		t.Fatalf("%s must run on the controller, got delegate_to=%v", scan["name"], scan["delegate_to"])
	}
	until := fmt.Sprint(scan["until"])
	for _, want := range []string{"bootwright_os_ssh_keyscan_required", "rc"} {
		if !strings.Contains(until, want) {
			t.Fatalf("%s until missing %q: %s", scan["name"], want, until)
		}
	}
	shell, ok := scan["ansible.builtin.shell"].(map[string]any)
	if !ok {
		t.Fatalf("%s must be a shell task using ssh, got %v", scan["name"], scan)
	}
	cmd := fmt.Sprint(shell["cmd"])
	if strings.Contains(cmd, "ssh-keyscan") {
		t.Fatalf("%s must NOT use ssh-keyscan (it ignores the FIPS crypto policy): %s", scan["name"], cmd)
	}
	for _, forbidden := range []string{"HostKeyAlgorithms", "ssh-ed25519", "ecdsa-sha2", "ssh-rsa"} {
		if strings.Contains(cmd, forbidden) {
			t.Fatalf("%s must let the system-policy-aware SSH client negotiate the host-key algorithm, found %q: %s", scan["name"], forbidden, cmd)
		}
	}
	for _, want := range []string{
		"flock 9",
		"mktemp",
		"StrictHostKeyChecking=accept-new",
		"UserKnownHostsFile=\"$tmp\"",
		"ssh-keygen -R",
		"ssh-keygen -F",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("%s cmd missing %q: %s", scan["name"], want, cmd)
		}
	}
	restrict := tasks[findAnsibleTask(t, tasks, "Restrict managed OS SSH known_hosts file")]
	fileTask, ok := restrict["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s must set file permissions, got %v", restrict["name"], restrict)
	}
	if fileTask["path"] != "{{ bootwright_component.osInstall.ssh.knownHostsPath }}" || fileTask["mode"] != "0600" {
		t.Fatalf("%s file task = %v", restrict["name"], fileTask)
	}

	waitTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/tasks/wait.yml")
	verify := waitTasks[findAnsibleTask(t, waitTasks, "Verify managed OS SSH authentication")]
	verifyShell, ok := verify["ansible.builtin.shell"].(map[string]any)
	if !ok {
		t.Fatalf("%s must be a shell task using strict SSH verification, got %v", verify["name"], verify)
	}
	verifyCmd := fmt.Sprint(verifyShell["cmd"])
	if strings.Contains(verifyCmd, "ssh-keyscan") || strings.Contains(verifyCmd, "StrictHostKeyChecking=accept-new") {
		t.Fatalf("%s must consume the host key recorded by the FIPS-aware scan without re-pinning it: %s", verify["name"], verifyCmd)
	}
	if !strings.Contains(verifyCmd, "StrictHostKeyChecking=yes") {
		t.Fatalf("%s must verify the recorded host key strictly: %s", verify["name"], verifyCmd)
	}

	probeTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_identity/tasks/probe_existing.yml")
	pre := probeTasks[findAnsibleTask(t, probeTasks, "Record managed OS SSH host key before install when reachable")]
	assertIncludeTasksFile(t, pre, "ssh_trust.yml")
	preVars, ok := pre["vars"].(map[string]any)
	if !ok {
		t.Fatalf("%s must pass keyscan vars", pre["name"])
	}
	if got := fmt.Sprint(preVars["bootwright_os_ssh_keyscan_required"]); got != "false" {
		t.Fatalf("%s keyscan required = %s, want false", pre["name"], got)
	}

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
	waitOldSSHStopIdx := findAnsibleTask(t, topTasks, "Wait for previous managed OS SSH port to stop")
	waitSSHIdx := findAnsibleTask(t, topTasks, "Wait for managed OS SSH port")
	cleanupMediaIdx := findAnsibleTask(t, topTasks, "Clean managed OS virtual media after SSH is ready")
	baremetalEjectIdx := findAnsibleTask(t, topTasks, "Eject boot virtual media after SSH is ready")
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
	if !(installBlockIdx < waitOldSSHStopIdx && waitOldSSHStopIdx < waitSSHIdx && waitSSHIdx < cleanupMediaIdx && cleanupMediaIdx < baremetalEjectIdx && baremetalEjectIdx < recordHostKeyIdx) {
		t.Fatalf("Anaconda role must let Kickstart reboot, wait for any previous SSH listener to stop, wait for SSH, then clean media and eject Redfish virtual media before recording the host key")
	}
	if !(recordHostKeyIdx < verifySSHIdx && verifySSHIdx < writeMarkerIdx) {
		t.Fatalf("Anaconda role must write the managed OS marker after SSH verification")
	}
	waitOldSSHStop, ok := topTasks[waitOldSSHStopIdx]["ansible.builtin.wait_for"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a wait_for task", topTasks[waitOldSSHStopIdx]["name"])
	}
	if waitOldSSHStop["host"] != "{{ bootwright_component.osInstall.ssh.address }}" || waitOldSSHStop["port"] != 22 || waitOldSSHStop["state"] != "stopped" || waitOldSSHStop["timeout"] != 300 {
		t.Fatalf("%s must wait for the previous SSH listener to stop, got %v", topTasks[waitOldSSHStopIdx]["name"], waitOldSSHStop)
	}
	if got := fmt.Sprint(topTasks[waitOldSSHStopIdx]["when"]); !strings.Contains(got, "bootwright_managed_os_boot_component is defined") || !strings.Contains(got, "bootwright_os_pre_ssh_port.failed") || !strings.Contains(got, "bootwright_managed_os_already_ready") {
		t.Fatalf("%s must run only after an install boot when SSH was previously reachable, got when=%v", topTasks[waitOldSSHStopIdx]["name"], topTasks[waitOldSSHStopIdx]["when"])
	}
	assertIncludeRoleName(t, topTasks[cleanupMediaIdx], "{{ bootwright_component.mediaPrepareRole }}")
	cleanupVars, ok := topTasks[cleanupMediaIdx]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("%s must pass cleanup vars, got %v", topTasks[cleanupMediaIdx]["name"], topTasks[cleanupMediaIdx])
	}
	if cleanupVars["bootwright_component"] != "{{ bootwright_managed_os_boot_component }}" || cleanupVars["bootwright_vmedia_action_effective"] != "cleanup" {
		t.Fatalf("%s must clean resolved managed OS media, got vars=%v", topTasks[cleanupMediaIdx]["name"], cleanupVars)
	}
	assertIncludeRoleName(t, topTasks[baremetalEjectIdx], "{{ bootwright_component.cleanupMediaRole }}")
	baremetalVars, ok := topTasks[baremetalEjectIdx]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("%s must pass cleanup vars, got %v", topTasks[baremetalEjectIdx]["name"], topTasks[baremetalEjectIdx])
	}
	if baremetalVars["bootwright_component"] != "{{ bootwright_managed_os_boot_component }}" || baremetalVars["bootwright_vmedia_action"] != "cleanup_media" {
		t.Fatalf("%s must eject resolved managed OS media via cleanup_media, got vars=%v", topTasks[baremetalEjectIdx]["name"], baremetalVars)
	}
	baremetalEjectWhen := fmt.Sprint(topTasks[baremetalEjectIdx]["when"])
	if !strings.Contains(baremetalEjectWhen, "bootwright_managed_os_boot_component is defined") || !strings.Contains(baremetalEjectWhen, "cleanupMediaRole") || !strings.Contains(baremetalEjectWhen, "mediaPrepareRole") {
		t.Fatalf("%s must run only for machines with a cleanupMediaRole and no mediaPrepareRole, got when=%v", topTasks[baremetalEjectIdx]["name"], topTasks[baremetalEjectIdx]["when"])
	}
	if strings.Contains(baremetalEjectWhen, "bootwright.core.") {
		t.Fatalf("%s must not compare rendered role names, got when=%v", topTasks[baremetalEjectIdx]["name"], topTasks[baremetalEjectIdx]["when"])
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
	for _, want := range []string{"--rm-args", "rd.live.check"} {
		if !stringListContains(buildCommand["argv"], want) {
			t.Fatalf("%s must strip the ISO media-check via mkksiso --rm-args rd.live.check, missing %q: %v", tasks[buildISOIdx]["name"], want, buildCommand["argv"])
		}
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
	if got, ok := tasks[buildISOIdx]["throttle"]; ok {
		t.Fatalf("%s must use managed-OS host fanout instead of task throttle, got throttle=%v", tasks[buildISOIdx]["name"], got)
	}
	if got := tasks[buildISOWithCmdlineIdx]["when"]; !stringListContains(got, "bootwright_os_install_iso_rebuild_needed | bool") || !stringListContains(got, "(bootwright_component.osInstall.installer.kernelArgs | default([]) | length) > 0") {
		t.Fatalf("%s must run only when rebuild is needed and kernel args are present, got when=%v", tasks[buildISOWithCmdlineIdx]["name"], got)
	}
	buildWithCmdlineCommand, ok := tasks[buildISOWithCmdlineIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a command task", tasks[buildISOWithCmdlineIdx]["name"])
	}
	for _, want := range []string{"mkksiso", "--ks", "--cmdline", "bootwright_component.osInstall.installer.kernelArgs | join(' ')", "--rm-args", "rd.live.check", "bootwright_os_source_iso_effective", "bootwright_os_install_iso"} {
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
	if got, ok := tasks[buildISOWithCmdlineIdx]["throttle"]; ok {
		t.Fatalf("%s must use managed-OS host fanout instead of task throttle, got throttle=%v", tasks[buildISOWithCmdlineIdx]["name"], got)
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
	if stagePerms["path"] != "{{ bootwright_os_install_iso }}" || stagePerms["state"] != "file" || stagePerms["mode"] != "{{ '0600' if (bootwright_os_kickstart_sensitive | bool) else '0644' }}" {
		t.Fatalf("%s must set permissions on the published install ISO, got %v", tasks[stagePermsIdx]["name"], stagePerms)
	}
	resolveTasks := readAnsibleTasks(t, filepath.Join(bootwrightCollectionRoleRoot, "machine_os_install_anaconda", "tasks", "resolve.yml"))
	sensitivity := findAnsibleTask(t, resolveTasks, "Resolve whether the managed OS kickstart carries a secret")
	fact := resolveTasks[sensitivity]["ansible.builtin.set_fact"].(map[string]any)["bootwright_os_kickstart_sensitive"].(string)
	for _, want := range []string{"rhsm.enabled", "proxy.credentialsPath", "diskEncryption"} {
		if !strings.Contains(fact, want) {
			t.Fatalf("bootwright_os_kickstart_sensitive must cover %q, got %q; a kickstart that carries a secret must publish its ISO 0600", want, fact)
		}
	}
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
	if persistentCleanupVars["bootwright_component"] != "{{ bootwright_managed_os_boot_component }}" || persistentCleanupVars["bootwright_vmedia_action_effective"] != "cleanup_persistent" {
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
		"{% set localization = ks.localization | default({}) %}",
		"lang {{ localization.language | default('en_US.UTF-8', true) }}",
		"keyboard {{ localization.keyboard | default('us', true) }}",
		"timezone {{ localization.timezone | default('UTC', true) }} --utc",
		"{% if localization.formats | default('') | length > 0 %}",
		"cat > /etc/locale.conf <<'BOOTWRIGHT_LOCALE_CONF'",
		"LC_TIME={{ localization.formats }}",
		"{% if localization.instLangs | default([]) | length > 0 %}{% set pkg_opts = pkg_opts ~ ' --inst-langs=' ~ (localization.instLangs | join(',')) %}{% endif %}",
		"{% set rhsm = installer.rhsm | default({}) %}",
		"{% if rhsm.enabled | default(false) %}",
		"rhsm --organization=\"{{ lookup('ansible.builtin.file', rhsm.organizationPath) | trim }}\" --activation-key=\"{{ lookup('ansible.builtin.file', rhsm.activationKeyPath) | trim }}\"{{ sat_flag }}{{ rhsm_proxy_flag }}{{ insights_flag }}",
		"{% elif installer.sourceURL | default('') | length > 0 %}",
		"url --url={{ installer.sourceURL }}{{ url_proxy_flag }}",
		"{% else %}",
		"cdrom",
		"{% set satellite = rhsm.satellite | default({}) %}",
		"{% set proxy_flag = (' --proxy=' ~ proxy_url) if (proxy_url | length > 0) else '' %}",
		"{% if sat_enabled %}{% set sat_flag = ' --server-hostname=' ~ satellite.hostname %}{% if satellite.contentBaseURL | default('') | length > 0 %}{% set sat_flag = sat_flag ~ ' --rhsm-baseurl=' ~ satellite.contentBaseURL %}{% endif %}{% endif %}",
		"{% set ssh_user = ks.sshUser | default('bootwright', true) %}",
		"{% set initial_password_path = ks.initialPasswordPath | default('', true) %}",
		"%pre --erroronfail",
		"cat > /etc/pki/ca-trust/source/anchors/bootwright-satellite-ca.pem <<'BOOTWRIGHT_SATELLITE_CA'",
		"update-ca-trust extract",
		"bootloader --location=mbr --boot-drive={{ storage.rootDisk }}",
		"part swap --recommended --ondisk={{ storage.rootDisk }}{{ luks_flags }}",
		"part / --fstype=xfs --size=10240 --grow --ondisk={{ storage.rootDisk }}{{ luks_flags }}",
		"autopart --type=lvm --fstype=xfs{{ luks_flags }}",
		"{% set disk_encryption = security.diskEncryption | default({}) %}",
		"{% set encrypted = (disk_encryption | length) > 0 %}",
		"{% set luks_flags = ' --encrypted --luks-version=' ~ (disk_encryption.luksVersion | default('luks2', true)) ~ pbkdf_flag ~ ' --passphrase=' ~ luks_passphrase %}",
		"clevis luks bind -y -d \"${bootwright_luks_device}\" -k \"${bootwright_luks_key}\" {{ clevis.pin }} '{{ clevis.config }}'",
		"dracut -fv --regenerate-all",
		"%post --erroronfail --interpreter=/bin/bash --log=/root/bootwright-ks-luks.log",
		"selinux --{{ selinux.mode }}",
		"firewall --{{ 'enabled' if firewall.enabled else 'disabled' }}",
		"{% set net_interfaces = net.interfaces | default([]) %}",
		"{% for iface in net_interfaces %}",
		"--bondslaves={{ iface.bondSlaves | join(',') }}",
		"--bondopts={{ iface.bondOptions }}",
		"--vlanid={{ iface.vlanID }}",
		"--interfacename={{ iface.interfaceName }}",
		"--mtu={{ iface.mtu }}",
		"--noipv4 --noipv6",
		"{{ hostname_flag }}",
		"services{{ svc_opts }}",
		"{% if enabled_services | length > 0 %}{% set svc_opts = svc_opts ~ ' --enabled=' ~ (enabled_services | join(',')) %}{% endif %}",
		"{% set ssh_key = '' %}",
		"{% if ks.sshPublicKeyPath | default('') | length > 0 %}",
		"{% set ssh_key = lookup('ansible.builtin.file', ks.sshPublicKeyPath) %}",
		"{% if initial_password | length > 0 %}",
		"user --name={{ ssh_user }} --password={{ initial_password }} --plaintext",
		"user --name={{ ssh_user }} --lock",
		"rootpw --lock",
		"PermitRootLogin no",
		"Defaults:{{ ssh_user }} !requiretty",
		"{{ ssh_user }} ALL=(ALL) NOPASSWD: ALL",
		"%packages{{ pkg_opts }}",
		"@^{{ packages.environment | default('minimal') }}-environment",
		"{% set marker = bootwright_component.osInstall.marker | default({}) %}",
		"cat > {{ marker.path }} <<'BOOTWRIGHT_INSTALL_MARKER'",
		"{{ marker | to_nice_json }}",
		"install -d -m 0755 /etc/ssh/sshd_config.d",
		"cat > /etc/ssh/sshd_config.d/99-bootwright-managed-os.conf <<'BOOTWRIGHT_SSHD_CONFIG'",
		"PasswordAuthentication no",
		"sshkey --username={{ ssh_user }}",
		"{% set sudoers_path = ks.sudoersPath | default('/etc/sudoers.d/60-bootwright', true) %}",
		"| urlencode | replace('/', '%2F')",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Kickstart template missing %q", want)
		}
	}
	if strings.Contains(body, "systemctl enable sshd") {
		t.Fatalf("Kickstart template must not force-enable sshd outside customizations.services.enabled")
	}
	for _, forbidden := range []string{
		"rootpw --iscrypted --allow-ssh {{ ssh_password_hash }}",
		"%wheel ALL=(ALL) NOPASSWD: ALL",
		"sshkey --username={{ ks.sshUser | default('root', true) }}",
		"user --name={{ ks.sshUser }} --groups=wheel --lock",
		"sed -i 's/^#\\?PasswordAuthentication .*/PasswordAuthentication no/' /etc/ssh/sshd_config",
		"/root/.ssh/authorized_keys",
		"PermitRootLogin yes",
		"rootpw --plaintext --allow-ssh {{ initial_password }}",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Kickstart template must render the normalized ssh_user and password hash, not %q", forbidden)
		}
	}
	for _, forbidden := range []string{"reboot --eject", "poweroff"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Kickstart template must rely on boot control, not %q", forbidden)
		}
	}
}

func TestManagedOSKickstartShredsRetainedCopiesBeforeMarker(t *testing.T) {
	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/templates/ks.cfg.j2")
	cleanupHeader := "%post --erroronfail --interpreter=/bin/bash --log=/root/bootwright-ks-secret-cleanup.log"
	cleanupStart := strings.Index(body, "{% endif %}\n"+cleanupHeader)
	markerStart := strings.Index(body, "%post --log=/root/bootwright-ks-post.log")
	if cleanupStart < 0 || markerStart < 0 || cleanupStart >= markerStart {
		t.Fatalf("Kickstart secret cleanup must begin outside the encryption condition and complete before the marker-writing post: cleanup=%d marker=%d", cleanupStart, markerStart)
	}
	cleanupEndOffset := strings.Index(body[cleanupStart:], "%end")
	if cleanupEndOffset < 0 {
		t.Fatal("Kickstart secret cleanup post has no terminator")
	}
	cleanup := body[cleanupStart : cleanupStart+cleanupEndOffset]
	for _, want := range []string{
		cleanupHeader,
		"set -euo pipefail",
		"for bootwright_kickstart_copy in /root/anaconda-ks.cfg /root/original-ks.cfg; do",
		"shred -u -- \"${bootwright_kickstart_copy}\"",
	} {
		if !strings.Contains(cleanup, want) {
			t.Fatalf("Kickstart secret cleanup missing %q", want)
		}
	}
	if strings.Contains(cleanup, "|| true") {
		t.Fatal("Kickstart secret cleanup must fail closed when a retained credential copy cannot be shredded")
	}
	if got := strings.Count(body, "shred -u -- \"${bootwright_kickstart_copy}\""); got != 1 {
		t.Fatalf("Kickstart secret cleanup command count = %d, want exactly one unconditional cleanup", got)
	}
}

func TestManagedOSKickstartTemplateNoCommandGluedToBlockTag(t *testing.T) {
	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/templates/ks.cfg.j2")
	blockTag := regexp.MustCompile(`\{%.*?%\}`)
	commentTag := regexp.MustCompile(`\{#.*?#\}`)
	for i, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimRight(line, " \t")
		if !strings.HasSuffix(trimmed, "%}") {
			continue
		}
		residue := strings.TrimSpace(commentTag.ReplaceAllString(blockTag.ReplaceAllString(trimmed, ""), ""))
		if residue != "" {
			t.Fatalf("ks.cfg.j2:%d ends in a {%% %%} tag with trailing content %q; trim_blocks will glue the next line onto it — end command lines in literal text or a {{ expression }} instead", i+1, residue)
		}
	}
}

func TestManagedOSSourceStagingElectedPerProviderHostAndSourceID(t *testing.T) {
	base := bootwrightCollectionRoleRoot + "/machine_os_install_anaconda/tasks/"
	main := readAnsibleTasks(t, base+"main.yml")
	probeIdx := findAnsibleTask(t, main, "Probe existing managed OS state")
	keyIdx := findAnsibleTask(t, main, "Resolve managed OS source staging key")
	electIdx := findAnsibleTask(t, main, "Elect managed OS source staging host")
	mediaIdx := findAnsibleTask(t, main, "Install managed OS media when needed")
	if !(probeIdx < keyIdx && keyIdx < electIdx && electIdx < mediaIdx) {
		t.Fatalf("the staging key must be published for every host, and the election resolved from it, before any host stages media")
	}
	for _, idx := range []int{keyIdx, electIdx} {
		if _, ok := main[idx]["run_once"]; ok {
			t.Fatalf("%s must not use run_once: it binds the source path, kind and condition to the first host while the image is a per-machine fact", main[idx]["name"])
		}
		if _, ok := main[idx]["when"]; ok {
			t.Fatalf("%s must run for every host so hostvars carry a staging key for the whole play, got when=%v", main[idx]["name"], main[idx]["when"])
		}
	}

	key, ok := main[keyIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", main[keyIdx]["name"])
	}
	keyExpr := fmt.Sprint(key["bootwright_os_source_stage_key"])
	for _, want := range []string{"bootwright_machine_task_provider_host_name", "bootwright_os_source_iso", "bootwright_managed_os_install_required"} {
		if !strings.Contains(keyExpr, want) {
			t.Fatalf("the staging key must combine %q so machines that share a provider host and source id elect one stager, got %q", want, keyExpr)
		}
	}
	if !strings.Contains(keyExpr, "else ''") {
		t.Fatalf("a host that is not installing must publish an empty staging key so it can never be elected to stage a source it never fetches, got %q", keyExpr)
	}

	elect, ok := main[electIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", main[electIdx]["name"])
	}
	if got := fmt.Sprint(elect["bootwright_os_source_stage_host"]); !strings.Contains(got, "bootwright_os_source_stage_hosts[bootwright_os_source_stage_key]") {
		t.Fatalf("%s must resolve one stager per key, got %q", main[electIdx]["name"], got)
	}
	electVars := fmt.Sprint(main[electIdx]["vars"])
	for _, want := range []string{"ansible_play_hosts", "hostvars", "bootwright_os_source_stage_key"} {
		if !strings.Contains(electVars, want) {
			t.Fatalf("%s must elect from the play's own published keys, missing %q: %s", main[electIdx]["name"], want, electVars)
		}
	}

	media := readAnsibleTasks(t, base+"install_media.yml")
	tasks := nestedAnsibleTasks(t, media[findAnsibleTask(t, media, "Install managed OS from virtual media")], "block")
	ownerCondition := "bootwright_os_source_stage_host == inventory_hostname"
	for _, name := range []string{"Copy managed ISO source to provider host", "Download managed ISO source on provider host"} {
		idx := findAnsibleTask(t, tasks, name)
		if !stringListContains(tasks[idx]["when"], ownerCondition) {
			t.Fatalf("%s must run only on the elected stager so peers neither re-transfer nor re-checksum the shared source, got when=%v", name, tasks[idx]["when"])
		}
		if got, ok := tasks[idx]["throttle"]; ok {
			t.Fatalf("%s must rely on the per-source election instead of serializing every provider host, got throttle=%v", name, got)
		}
		if _, ok := tasks[idx]["run_once"]; ok {
			t.Fatalf("%s must not use run_once: the source id and image kind are per-machine facts", name)
		}
	}

	statIdx := findAnsibleTask(t, tasks, "Stat managed ISO source")
	stagedIdx := findAnsibleTask(t, tasks, "Assert managed ISO source is staged")
	identityIdx := findAnsibleTask(t, tasks, "Record managed ISO source identity")
	changeIdx := findAnsibleTask(t, tasks, "Resolve managed ISO source change key")
	verifyOwnerIdx := findAnsibleTask(t, tasks, "Resolve managed ISO checksum verification owner")
	verifyIdx := findAnsibleTask(t, tasks, "Verify managed ISO checksum")
	assertIdx := findAnsibleTask(t, tasks, "Assert managed ISO checksum")
	if !(statIdx < stagedIdx && stagedIdx < identityIdx && identityIdx < changeIdx && changeIdx < verifyOwnerIdx && verifyOwnerIdx < verifyIdx && verifyIdx < assertIdx) {
		t.Fatalf("a host that delegated staging must prove the source landed before recording identity, and the change keys of all peers must be published before the verification owner is resolved")
	}
	staged, ok := tasks[stagedIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an assert task", tasks[stagedIdx]["name"])
	}
	if !stringListItemContains(staged["that"], "bootwright_os_source_stat.stat.exists") {
		t.Fatalf("%s must fail closed when the elected stager did not leave a source behind, got that=%v", tasks[stagedIdx]["name"], staged["that"])
	}

	change, ok := tasks[changeIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", tasks[changeIdx]["name"])
	}
	changeExpr := fmt.Sprint(change["bootwright_os_source_change_key"])
	for _, want := range []string{"bootwright_os_source_stage_key", "bootwright_os_source_copy.changed", "bootwright_os_source_download.changed", "bootwright_os_source_identity.changed"} {
		if !strings.Contains(changeExpr, want) {
			t.Fatalf("the change key must publish %q so no host's staging change is lost when it is not the stager, got %q", want, changeExpr)
		}
	}
	verifyOwner, ok := tasks[verifyOwnerIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", tasks[verifyOwnerIdx]["name"])
	}
	verifyExpr := fmt.Sprint(verifyOwner["bootwright_os_source_verify_required"])
	for _, want := range []string{ownerCondition, "ansible_play_hosts", "bootwright_os_source_change_key", "bootwright_os_source_stage_key"} {
		if !strings.Contains(verifyExpr, want) {
			t.Fatalf("the elected stager must checksum whenever any peer sharing its source changed, missing %q: %s", want, verifyExpr)
		}
	}
	if !stringListContains(tasks[verifyIdx]["when"], "bootwright_os_source_verify_required | bool") {
		t.Fatalf("%s must be gated on the single shared verification, got when=%v", tasks[verifyIdx]["name"], tasks[verifyIdx]["when"])
	}
	if !stringListContains(tasks[verifyIdx]["when"], "(bootwright_component.osInstall.image.checksum | default('') | length) > 0") {
		t.Fatalf("%s must still only run for a declared checksum, got when=%v", tasks[verifyIdx]["name"], tasks[verifyIdx]["when"])
	}
	if !stringListContains(tasks[assertIdx]["when"], "not (bootwright_os_source_sha256.skipped | default(false) | bool)") {
		t.Fatalf("%s must stay skipped on hosts that delegated verification, got when=%v", tasks[assertIdx]["name"], tasks[assertIdx]["when"])
	}
}

func managedOSAnacondaTasks(t *testing.T) []map[string]any {
	t.Helper()
	base := "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/tasks/"
	identity := "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_identity/tasks/"
	return readAnsibleTasksFromFiles(t,
		base+"validate.yml",
		base+"resolve.yml",
		identity+"probe_existing.yml",
		base+"install_media.yml",
		base+"wait.yml",
		identity+"configure_network.yml",
		identity+"marker.yml",
		base+"ownership.yml",
	)
}
