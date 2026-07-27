package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

const ownershipRoleTaskRoot = bootwrightCollectionRoleRoot + "/ownership_record/tasks/"

func TestHostBasePackageInstallGatedOnRPMQuery(t *testing.T) {
	tasks := readAnsibleTasks(t, bootwrightCollectionRoleRoot+"/machine_base/tasks/main.yml")
	probeIdx := findAnsibleTask(t, tasks, "Probe installed base host packages")
	presenceIdx := findAnsibleTask(t, tasks, "Resolve base host package presence")
	installIdx := findAnsibleTask(t, tasks, "Install base host packages")
	if !(probeIdx < presenceIdx && presenceIdx < installIdx) {
		t.Fatalf("machine_base must probe installed packages and resolve presence before the package transaction")
	}

	probe, ok := tasks[probeIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a command task", tasks[probeIdx]["name"])
	}
	argv := fmt.Sprint(probe["argv"])
	for _, want := range []string{"rpm", "--query", "--quiet", "bootwright_machine_base_packages"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("%s argv missing %q: %s", tasks[probeIdx]["name"], want, argv)
		}
	}
	if got := tasks[probeIdx]["changed_when"]; got != false {
		t.Fatalf("%s must not report changes, got changed_when=%v", tasks[probeIdx]["name"], got)
	}
	if got := tasks[probeIdx]["failed_when"]; got != false {
		t.Fatalf("%s must leave the install decision to the presence fact, got failed_when=%v", tasks[probeIdx]["name"], got)
	}
	if got := tasks[probeIdx]["check_mode"]; got != false {
		t.Fatalf("%s must run under --check so the presence fact is not derived from a check-mode skip, got check_mode=%v", tasks[probeIdx]["name"], got)
	}
	if got := fmt.Sprint(tasks[probeIdx]["when"]); !strings.Contains(got, "os_family") || !strings.Contains(got, "RedHat") {
		t.Fatalf("%s must only run the rpm query on Red Hat hosts, got when=%v", tasks[probeIdx]["name"], tasks[probeIdx]["when"])
	}

	presence, ok := tasks[presenceIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", tasks[presenceIdx]["name"])
	}
	fact := fmt.Sprint(presence["bootwright_machine_base_packages_present"])
	if !strings.Contains(fact, "bootwright_machine_base_package_probe.rc") {
		t.Fatalf("%s must derive presence from the rpm query rc, got %q", tasks[presenceIdx]["name"], fact)
	}
	if !strings.Contains(fact, "default(1)") || !strings.Contains(fact, "== 0") {
		t.Fatalf("%s must treat an unprobeable host as not-present so the package transaction still runs, got %q", tasks[presenceIdx]["name"], fact)
	}

	if got := fmt.Sprint(tasks[installIdx]["when"]); !strings.Contains(got, "not (bootwright_machine_base_packages_present") {
		t.Fatalf("%s must skip the package transaction only when every base package is already installed, got when=%v", tasks[installIdx]["name"], tasks[installIdx]["when"])
	}
	install, ok := tasks[installIdx]["ansible.builtin.package"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a package task", tasks[installIdx]["name"])
	}
	if install["name"] != "{{ bootwright_machine_base_packages }}" || install["state"] != "present" {
		t.Fatalf("%s must still install the full base package list, got %v", tasks[installIdx]["name"], install)
	}
}

func TestOwnedPackageFactsGatheredOnlyWhenAttributionMissing(t *testing.T) {
	tasks := readAnsibleTasks(t, ownershipRoleTaskRoot+"package_apply.yml")
	dirIdx := findAnsibleTask(t, tasks, "Create package ownership directory")
	findIdx := findAnsibleTask(t, tasks, "Find existing package ownership records")
	readIdx := findAnsibleTask(t, tasks, "Read existing package ownership records")
	indexIdx := findAnsibleTask(t, tasks, "Index existing package ownership records")
	unattributedIdx := findAnsibleTask(t, tasks, "Resolve packages without recorded preexisting attribution")
	factsIdx := findAnsibleTask(t, tasks, "Gather installed package facts before ownership apply")
	installIdx := findAnsibleTask(t, tasks, "Install owned packages")
	writeIdx := findAnsibleTask(t, tasks, "Write ownership records for owned packages")
	if !(dirIdx < findIdx && findIdx < readIdx && readIdx < indexIdx && indexIdx < unattributedIdx && unattributedIdx < factsIdx) {
		t.Fatalf("package ownership apply must index existing records before deciding whether package facts are needed")
	}
	if !(factsIdx < installIdx) {
		t.Fatalf("package facts must be gathered before the install so preexisting reflects what the operator already had")
	}
	if !(installIdx < writeIdx) {
		t.Fatalf("package ownership records must be written after the install")
	}

	readLoop := fmt.Sprint(tasks[readIdx]["loop"])
	for _, want := range []string{"bootwright_ownership_packages", "intersect"} {
		if !strings.Contains(readLoop, want) {
			t.Fatalf("%s must read only the records of the requested packages, missing %q: %s", tasks[readIdx]["name"], want, readLoop)
		}
	}

	if got := fmt.Sprint(tasks[factsIdx]["when"]); !strings.Contains(got, "bootwright_ownership_packages_unattributed") || !strings.Contains(got, "length > 0") {
		t.Fatalf("%s must be skipped only when every requested package already carries a recorded preexisting flag, got when=%v", tasks[factsIdx]["name"], tasks[factsIdx]["when"])
	}
	facts, ok := tasks[factsIdx]["ansible.builtin.package_facts"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a package_facts task", tasks[factsIdx]["name"])
	}
	if facts["manager"] != "auto" {
		t.Fatalf("%s must keep manager auto, got %v", tasks[factsIdx]["name"], facts)
	}

	unattributed, ok := tasks[unattributedIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", tasks[unattributedIdx]["name"])
	}
	expr := fmt.Sprint(unattributed["bootwright_ownership_packages_unattributed"])
	if !strings.Contains(expr, "bootwright_ownership_packages") || !strings.Contains(expr, "difference") {
		t.Fatalf("%s must subtract attributed records from the requested packages, got %q", tasks[unattributedIdx]["name"], expr)
	}
	attributed := fmt.Sprint(tasks[unattributedIdx]["vars"])
	if !strings.Contains(attributed, "selectattr('value.preexisting', 'defined')") {
		t.Fatalf("%s must treat a record without a preexisting key as unattributed so the flag is never invented from post-install facts, got vars=%v", tasks[unattributedIdx]["name"], tasks[unattributedIdx]["vars"])
	}

	assertIncludeTasksFile(t, tasks[writeIdx], "package_records_write.yml")
}

func TestOwnedPackageRecordsKeepPerPackagePreexistingAttribution(t *testing.T) {
	tasks := readAnsibleTasks(t, ownershipRoleTaskRoot+"package_records_write.yml")
	if len(tasks) != 1 {
		t.Fatalf("package_records_write.yml must write every record in one batched task, got %d tasks", len(tasks))
	}
	write := tasks[0]
	if write["loop"] != "{{ bootwright_ownership_packages }}" {
		t.Fatalf("record write must loop over the requested packages, got loop=%v", write["loop"])
	}
	control, ok := write["loop_control"].(map[string]any)
	if !ok {
		t.Fatalf("record write must name its loop var, got %v", write)
	}
	loopVar, _ := control["loop_var"].(string)
	if loopVar == "" || loopVar == "bootwright_ownership_packages" {
		t.Fatalf("record write loop_var got %q", loopVar)
	}

	copyTask, ok := write["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("record write is not a copy task, got %v", write)
	}
	dest := fmt.Sprint(copyTask["dest"])
	if !strings.Contains(dest, "bootwright_ownership_package_safe") || !strings.Contains(dest, "inventory_hostname") {
		t.Fatalf("record write must key each record file on the sanitized package name, got dest=%q", dest)
	}
	if copyTask["mode"] != "0600" || copyTask["owner"] != "root" {
		t.Fatalf("record write must keep root-only record permissions, got %v", copyTask)
	}

	vars, ok := write["vars"].(map[string]any)
	if !ok {
		t.Fatalf("record write must build the record from per-item vars, got %v", write)
	}
	if got := fmt.Sprint(vars["bootwright_ownership_package_safe"]); !strings.Contains(got, loopVar) {
		t.Fatalf("record write must sanitize the per-item package name, got %q", got)
	}
	if got := fmt.Sprint(vars["bootwright_ownership_package_existing"]); !strings.Contains(got, "bootwright_ownership_package_index") || !strings.Contains(got, "bootwright_ownership_package_safe") {
		t.Fatalf("record write must look the existing record up per package, got %q", got)
	}
	record, ok := vars["bootwright_ownership_package_record"].(map[string]any)
	if !ok {
		t.Fatalf("record write must build a record mapping, got %v", vars["bootwright_ownership_package_record"])
	}
	preexisting := fmt.Sprint(record["preexisting"])
	if !strings.Contains(preexisting, "bootwright_ownership_package_existing.preexisting") {
		t.Fatalf("preexisting must keep an already-recorded flag, got %q", preexisting)
	}
	if !strings.Contains(preexisting, "ansible_facts.packages") || !strings.Contains(preexisting, "bootwright_ownership_package_name in") {
		t.Fatalf("a first-time record must derive preexisting from that one package's live presence, got %q", preexisting)
	}
	requiredBy := fmt.Sprint(record["requiredBy"])
	if !strings.Contains(requiredBy, "bootwright_ownership_package_existing_required_by") || !strings.Contains(requiredBy, "unique") {
		t.Fatalf("requiredBy must union the recorded owners with this caller, got %q", requiredBy)
	}
	if got := fmt.Sprint(record["package"]); !strings.Contains(got, "bootwright_ownership_package_name") {
		t.Fatalf("record must carry the unsanitized package name, got %q", got)
	}

	remove := readAnsibleTasks(t, ownershipRoleTaskRoot+"package_remove_one.yml")
	removeIdx := findAnsibleTask(t, remove, "Remove package that Bootwright introduced")
	if got := fmt.Sprint(remove[removeIdx]["when"]); !strings.Contains(got, "preexisting | default(true)") {
		t.Fatalf("destroy must keep consuming the per-package preexisting flag, got when=%v", remove[removeIdx]["when"])
	}
}

func TestOwnedPackageSingleRecordSharesTheBatchedWriter(t *testing.T) {
	tasks := readAnsibleTasks(t, ownershipRoleTaskRoot+"package_apply_one.yml")
	indexIdx := findAnsibleTask(t, tasks, "Index existing package ownership record")
	writeIdx := findAnsibleTask(t, tasks, "Write package ownership record")
	if !(findAnsibleTask(t, tasks, "Stat existing package ownership record") < indexIdx && indexIdx < writeIdx) {
		t.Fatalf("single-package apply must index its record before writing it")
	}
	index, ok := tasks[indexIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", tasks[indexIdx]["name"])
	}
	expr := fmt.Sprint(index["bootwright_ownership_package_index"])
	if !strings.Contains(expr, "bootwright_ownership_package_safe") {
		t.Fatalf("%s must key the index on the sanitized package name, got %q", tasks[indexIdx]["name"], expr)
	}
	if _, ok := tasks[indexIdx]["when"]; ok {
		t.Fatalf("%s must run unconditionally so a record-less package cannot inherit a previous inclusion's index", tasks[indexIdx]["name"])
	}
	assertIncludeTasksFile(t, tasks[writeIdx], "package_records_write.yml")
	vars, ok := tasks[writeIdx]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("%s must pass its single package to the batched writer, got %v", tasks[writeIdx]["name"], tasks[writeIdx])
	}
	if !stringListItemContains(vars["bootwright_ownership_packages"], "bootwright_ownership_package_name") {
		t.Fatalf("%s must pass bootwright_ownership_package_name as the only batch entry, got %v", tasks[writeIdx]["name"], vars["bootwright_ownership_packages"])
	}
}

func TestPackageFactsAreNeverCached(t *testing.T) {
	if value, ok := ansibleCfgValue(readRepoFile(t, "ansible/ansible.cfg"), "defaults", "fact_caching"); ok && strings.ToLower(value) != "memory" {
		t.Fatalf("ansible.cfg must not persist gathered facts (fact_caching=%q): a cached package fact set outlives the install it was gathered before and corrupts the ownership preexisting flag", value)
	}
	for _, name := range []string{"package_apply.yml", "package_apply_one.yml", "package_records_write.yml"} {
		if body := readRepoFile(t, ownershipRoleTaskRoot+name); strings.Contains(body, "cacheable") {
			t.Fatalf("%s must not mark package-ownership facts cacheable", name)
		}
	}
}
