package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

func TestStorageNativePreparationUsesRenderedProviderArtifact(t *testing.T) {
	mainTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/main.yml")
	installIdx := findAnsibleTask(t, mainTasks, "Install cephadm host tooling")
	prepareIdx := findAnsibleTask(t, mainTasks, "Run the selected native Ceph preparation flow")
	parityIdx := findAnsibleTask(t, mainTasks, "Re-prove every exact native Ceph package after provider preparation")
	runtimeIdx := findAnsibleTask(t, mainTasks, "Prove the Ceph storage node container runtime")
	if !(installIdx < prepareIdx && prepareIdx < parityIdx && parityIdx < runtimeIdx) {
		t.Fatalf("native preparation must run after artifact installation and before post-EVR and runtime gates: install=%d prepare=%d parity=%d runtime=%d", installIdx, prepareIdx, parityIdx, runtimeIdx)
	}

	prepare := mainTasks[prepareIdx]
	if got := fmt.Sprint(prepare["ansible.builtin.include_tasks"]); !strings.Contains(got, "artifactPolicy.nativePreparationMode") || strings.Contains(got, "ibm") || strings.Contains(got, "redhat") {
		t.Fatalf("native preparation dispatch must be selected only by rendered artifact policy, got %s", got)
	}
	prepareWhen := fmt.Sprint(prepare["when"])
	if !strings.Contains(prepareWhen, "storage_skip_prereqs") || strings.Contains(prepareWhen, "prereqs_only") {
		t.Fatalf("native preparation must run on every storage-infra host and skip only the later base pass, got when=%s", prepareWhen)
	}
	parity := mainTasks[parityIdx]
	if got := fmt.Sprint(parity["loop"]); !strings.Contains(got, "provider.packageArtifacts") {
		t.Fatalf("post-preparation parity must cover every rendered exact artifact, got loop=%s", got)
	}
	if got := fmt.Sprint(parity["ansible.builtin.include_tasks"]); got != "phases/native_package_parity_one.yml" {
		t.Fatalf("post-preparation parity must use the generic exact-EVR adapter, got %s", got)
	}
	parityWhen := fmt.Sprint(parity["when"])
	if !strings.Contains(parityWhen, "storage_skip_prereqs") || strings.Contains(parityWhen, "prereqs_only") {
		t.Fatalf("post-preparation exact parity must run in the same all-node storage-infra pass as preparation, got when=%s", parityWhen)
	}

	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/native_preparation/cephadm-ansible-local.yml"
	top := readAnsibleTasks(t, path)
	if len(top) != 1 {
		t.Fatalf("native preparation adapter has %d top-level tasks, want one bounded block", len(top))
	}
	vars, ok := top[0]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("native preparation adapter must declare a bounded variable contract, got %v", top[0])
	}
	extra := fmt.Sprint(vars["bootwright_ceph_native_preparation_extra_vars"])
	for _, want := range []string{
		"ceph_origin:custom",
		"ceph_defaults_ceph_origin:custom",
		"ceph_custom_repositories:[]",
		"upgrade_ceph_packages:false",
		"ceph_defaults_upgrade_ceph_packages:false",
		"packages_to_uninstall:[]",
		"ceph_client_pkgs:[]",
		"ceph_defaults_ceph_client_pkgs:[]",
		"runtime_package_requests",
		"prerequisitePackages",
		"reports_dir",
	} {
		if !strings.Contains(strings.ReplaceAll(extra, " ", ""), strings.ReplaceAll(want, " ", "")) {
			t.Fatalf("native preparation extra vars must retain %q, got %s", want, extra)
		}
	}

	block := nestedAnsibleTasks(t, top[0], "block")
	clearTransientIdx := findAnsibleTask(t, block, "Clear prior native preparation transient directory")
	createTransientIdx := findAnsibleTask(t, block, "Create the native preparation work directory")
	entrypointIdx := findAnsibleTask(t, block, "Inspect the installed native preparation entrypoints")
	ownerIdx := findAnsibleTask(t, block, "Read the RPM owner of the installed native preparation playbook")
	requireOwnerIdx := findAnsibleTask(t, block, "Require the declared package to own the native preparation playbook")
	syntaxIdx := findAnsibleTask(t, block, "Syntax-check the installed native preparation playbook")
	executeIdx := findAnsibleTask(t, block, "Execute the installed native preparation playbook without package upgrades")
	inspectReportIdx := findAnsibleTask(t, block, "Inspect the current native preparation report")
	requireReportIdx := findAnsibleTask(t, block, "Require the current native preparation report")
	preserveReportIdx := findAnsibleTask(t, block, "Preserve the current native preparation report")
	if !(clearTransientIdx < createTransientIdx && createTransientIdx < entrypointIdx && entrypointIdx < ownerIdx && ownerIdx < requireOwnerIdx && requireOwnerIdx < syntaxIdx && syntaxIdx < executeIdx && executeIdx < inspectReportIdx && inspectReportIdx < requireReportIdx && requireReportIdx < preserveReportIdx) {
		t.Fatalf("native adapter must prove installed entrypoints and RPM ownership, syntax-check, execute, prove a current transient report, then preserve it")
	}
	if got := fmt.Sprint(block[clearTransientIdx]["ansible.builtin.file"]); !strings.Contains(got, "native_preparation_dir") || !strings.Contains(got, "state:absent") {
		t.Fatalf("native adapter must delete the exact prior transient directory before creating this run's evidence, got %s", got)
	}
	owner := fmt.Sprint(block[ownerIdx]["ansible.builtin.command"])
	if !strings.Contains(owner, "rpm") || !strings.Contains(owner, "-qf") || !strings.Contains(owner, "native_preparation_playbook") {
		t.Fatalf("native playbook ownership must be proved by rpm -qf, got %s", owner)
	}
	for _, idx := range []int{syntaxIdx, executeIdx} {
		command := fmt.Sprint(block[idx]["ansible.builtin.command"])
		for _, want := range []string{"/usr/bin/ansible-playbook", "localhost,", "local", "--limit", "localhost", "native_preparation_vars_path", "native_preparation_playbook"} {
			if !strings.Contains(command, want) {
				t.Fatalf("native preparation command %q must contain %q, got %s", block[idx]["name"], want, command)
			}
		}
		environment := fmt.Sprint(block[idx]["environment"])
		for _, want := range []string{"ANSIBLE_CONFIG", "native_preparation_config", "ANSIBLE_LOG_PATH", "native_preparation_dir", "ANSIBLE_CACHE_PLUGIN:memory", "ANSIBLE_CACHE_PLUGIN_CONNECTION", "ANSIBLE_GATHERING:explicit", "ANSIBLE_LOCAL_TEMP", "ANSIBLE_REMOTE_TEMP"} {
			if !strings.Contains(strings.ReplaceAll(environment, " ", ""), strings.ReplaceAll(want, " ", "")) {
				t.Fatalf("native preparation command %q must isolate provider config state with %q, got %s", block[idx]["name"], want, environment)
			}
		}
	}
	if got := fmt.Sprint(block[syntaxIdx]["ansible.builtin.command"]); !strings.Contains(got, "--syntax-check") {
		t.Fatalf("installed native preparation playbook must be syntax-checked before execution, got %s", got)
	}
	if got := fmt.Sprint(vars["bootwright_ceph_native_preparation_extra_vars"]); !strings.Contains(got, "native_preparation_run_reports_dir") {
		t.Fatalf("provider preflight must write its report into the fresh transient run directory, got %s", got)
	}
	if got := fmt.Sprint(block[preserveReportIdx]["ansible.builtin.copy"]); !strings.Contains(got, "native_preparation_run_reports_dir") || !strings.Contains(got, "native_preparation_reports_dir") || !strings.Contains(got, "remote_src:true") {
		t.Fatalf("only the current run's provider report may be copied into durable ownership state, got %s", got)
	}

	for _, rel := range []string{
		path,
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/native_package_install_one.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/native_package_parity_one.yml",
	} {
		body := strings.ToLower(readRepoFile(t, rel))
		for _, forbidden := range []string{"distribution ==", "distribution=", "ibm", "red hat", "9.9", "20.1", "5.0.2"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s must contain no provider or concrete release branch %q", rel, forbidden)
			}
		}
	}
}

func TestStorageNativePreparationPostVerifiesExactEVRs(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/native_package_parity_one.yml"
	tasks := readAnsibleTasks(t, path)
	readIdx := findAnsibleTask(t, tasks, "Read the native Ceph package coordinates after provider preparation")
	requireIdx := findAnsibleTask(t, tasks, "Require provider preparation to preserve the declared native Ceph package build")
	if readIdx >= requireIdx {
		t.Fatalf("native artifact EVR must be read before the exact post-preparation gate")
	}
	if got := fmt.Sprint(tasks[readIdx]["ansible.builtin.command"]); !strings.Contains(got, "EPOCHNUM") || !strings.Contains(got, "VERSION") || !strings.Contains(got, "RELEASE") || !strings.Contains(got, "artifact.name") {
		t.Fatalf("native post-preparation probe must read all RPM coordinate forms by bare artifact name, got %s", got)
	}
	require, ok := tasks[requireIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(require["that"]), "artifact.spec") || !strings.Contains(fmt.Sprint(require["fail_msg"]), "desiredStatePath") || !strings.Contains(fmt.Sprint(require["fail_msg"]), "refuses") {
		t.Fatalf("native post-preparation parity must fail closed against the authored artifact coordinate, got %v", tasks[requireIdx])
	}
}
