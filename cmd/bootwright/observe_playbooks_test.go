package main

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"go.yaml.in/yaml/v3"
)

const (
	observeRoleRoot   = "collections/ansible_collections/bootwright/core/roles/"
	observeCollection = "bootwright.core."
)

type observeTaskKey struct {
	role, file, module string
}

type observeAllowance struct {
	reason string
	holds  func(params, task map[string]any) bool
}

var (
	observeCustodyWhen = regexp.MustCompile(`^bootwright_[a-z_]+_output is defined$`)
	observePlayKeys    = []string{"any_errors_fatal", "gather_facts", "hosts", "name", "serial", "strategy", "tasks"}
	observeTaskKeys    = []string{
		"always", "any_errors_fatal", "args", "become", "become_user", "block", "changed_when", "check_mode",
		"delay", "delegate_to", "diff", "environment", "failed_when", "ignore_errors", "loop", "loop_control",
		"name", "no_log", "notify", "register", "rescue", "retries", "run_once", "tags", "throttle", "timeout",
		"until", "vars", "when",
	}
	observeImports = []string{"ansible.builtin.import_tasks", "ansible.builtin.include_tasks"}
	observeRoles   = []string{"ansible.builtin.import_role", "ansible.builtin.include_role"}
)

// The read-only allowlist: every module an observe entrypoint may reach, keyed
// by where it runs, with why that use changes nothing the observation reads.
func observeAllowlist() map[observeTaskKey]observeAllowance {
	read := func(map[string]any, map[string]any) bool { return true }
	allow := map[observeTaskKey]observeAllowance{}
	add := func(role, file, module, reason string, holds func(map[string]any, map[string]any) bool) {
		allow[observeTaskKey{role, file, module}] = observeAllowance{reason, holds}
	}
	for role, protocol := range map[string]string{
		"cluster_objects":                "cluster_objects_protocol",
		"containercluster_install_agent": "containercluster_install_protocol",
		"containercluster_media_agent":   "containercluster_media_protocol",
		"infra_artifact_server_nginx":    "artifact_server_protocol",
		"infra_managed_service":          "infra_service_protocol",
		"managedos_install_anaconda":     "managedos_install_protocol",
		"managedos_package_tree":         "managedos_tree_protocol",
		"storage_ceph_core":              "ceph_cluster_protocol",
		"storage_ceph_host":              "ceph_cluster_protocol",
		"storage_ceph_image":             "ceph_image_protocol",
		"storage_ceph_smb":               "ceph_smb_protocol",
		"storage_ceph_replication":       "ceph_replication_protocol",
		"substrate_baremetal_machine":    "substrate_physical_protocol",
		"substrate_libvirt_host":         "substrate_host_protocol",
		"substrate_libvirt_machine":      "substrate_machine_protocol",
	} {
		add(role, "observe.yml", observeCollection+protocol, "publishes the handoff and evidence on the runner's own channel", read)
	}
	for _, role := range []string{
		"addons_instance", "containercluster_network", "containercluster_install_agent", "containercluster_media_agent", "managedos_install_anaconda",
		"managedos_package_tree", "storage_ceph_core", "storage_ceph_host", "storage_ceph_image",
		"storage_ceph_smb", "storage_ceph_replication", "substrate_baremetal_machine", "substrate_libvirt_host", "substrate_libvirt_machine",
	} {
		add(role, "observe.yml", "ansible.builtin.assert", "validates the frozen request in memory", read)
	}
	for _, role := range []string{"storage_ceph_core", "storage_ceph_host", "storage_ceph_image", "storage_ceph_smb", "storage_ceph_replication"} {
		add(role, "observe.yml", "ansible.builtin.fail", "refuses an unproved observation without touching the host", read)
	}
	add("cluster_objects", "observe.yml", "ansible.builtin.assert", "validates the frozen shared program version", read)
	add("cluster_objects", "observe.yml", "ansible.builtin.fail", "reports the bounded read-only adapter refusal", read)
	add("cluster_objects", "observe.yml", observeCollection+"cluster_objects_read", "reads the frozen object program through projected oc get operations", read)
	add("physical_erase", "observe.yml", observeCollection+"physical_erase", "reads durable receipt/custody without boot, overwrite, acknowledgement, shutdown or deletion", func(params, _ map[string]any) bool {
		return params["phase"] == "observe"
	})
	add("physical_erase", "observe.yml", observeCollection+"physical_erase_protocol", "publishes loaded or read-only completion, never a durable effect preparation", func(params, _ map[string]any) bool {
		return params["phase"] == "loaded" || params["phase"] == "completed" && params["observed"] == true
	})
	addObserveReads(add, read)
	addObserveScratch(add, read)
	return allow
}

func addObserveReads(add func(string, string, string, string, func(map[string]any, map[string]any) bool), read func(map[string]any, map[string]any) bool) {
	add("containercluster_install_agent", "observe.yml", observeCollection+"containercluster_install_inspect", "reads what the installation recorded", read)
	add("containercluster_install_agent", "observe.yml", "ansible.builtin.copy", "hands the kept kubeconfig to custody through the declared output", observeCustodyCopy)
	add("containercluster_install_agent", "cluster_state.yml", "ansible.builtin.command", "runs oc get against the cluster with a request timeout", read)
	add("containercluster_install_agent", "cluster_state.yml", "ansible.builtin.set_fact", "resolves the cluster answers in memory", read)
	add("containercluster_install_agent", "state.yml", observeCollection+"containercluster_media_token", "compares image device and inode without changing publication", observeRegistrationInspect)
	add("containercluster_install_agent", "state.yml", observeCollection+"redfish_system_read", "reads each node's power and media from its controller", read)
	add("containercluster_install_agent", "state.yml", "ansible.builtin.fail", "names a controller that did not answer", read)
	add("containercluster_install_agent", "state.yml", "ansible.builtin.set_fact", "resolves the answers in memory", read)
	add("containercluster_media_agent", "observe.yml", observeCollection+"containercluster_media_inspect", "reads what the media block published", read)
	add("containercluster_media_agent", "observe.yml", "ansible.builtin.stat", "reads the kept kubeconfig's metadata without following links", read)
	add("containercluster_media_agent", "observe.yml", "ansible.builtin.copy", "hands the kept kubeconfig to custody through the declared output", observeCustodyCopy)
	add("infra_artifact_server_nginx", "observe.yml", observeCollection+"artifact_server_inspect", "reads the served unit, container and content", read)
	add("infra_managed_service", "observe.yml", observeCollection+"infra_service_inspect", "reads the managed service's unit, container and answers", read)
	add("managedos_install_anaconda", "observe.yml", observeCollection+"managedos_install_inspect", "reads the published installation content", read)
	add("managedos_install_anaconda", "observe.yml", observeCollection+"redfish_system_read", "reads the machine's power and media from its controller", read)
	add("managedos_install_anaconda", "observe.yml", "ansible.builtin.set_fact", "names, in memory, a removal's read of a machine holding nothing of this installation", read)
	add("managedos_install_anaconda", "work.yml", "ansible.builtin.stat", "reads a work-area ancestor without following links", read)
	add("managedos_install_anaconda", "work.yml", "ansible.builtin.assert", "refuses an ancestor that is not private to root", read)
	add("managedos_install_anaconda", "identity.yml", "ansible.builtin.fail", "refuses an identity channel with no binding", read)
	add("managedos_install_anaconda", "identity.yml", "ansible.builtin.set_fact", "takes the channel's answer in memory", read)
	add("managedos_install_anaconda", "reachable.yml", observeCollection+"ssh_key_policy", "reads the host's effective crypto policy", read)
	add("managedos_install_anaconda", "reachable.yml", "ansible.builtin.set_fact", "names the pinned key's algorithms in memory", read)
	add("managedos_install_anaconda", "reachable.yml", "ansible.builtin.command", "runs /usr/bin/true over ssh pinned to the reported key", read)
	add("managedos_install_anaconda", "network_observe.yml", "ansible.builtin.set_fact", "names the network answers in memory", read)
	add("managedos_install_anaconda", "network_observe.yml", observeCollection+"managedos_network_compare", "compares the declared network with the running one in memory", read)
	add("managedos_install_anaconda", "guest.yml", observeCollection+"ssh_key_policy", "reads the host's effective crypto policy", read)
	add("managedos_install_anaconda", "guest.yml", "ansible.builtin.set_fact", "names the pinned key's algorithms in memory", read)
	add("managedos_install_anaconda", "guest.yml", "ansible.builtin.command", "runs over ssh pinned to the reported key the fixed read network_observe.yml, host_security.yml or root_encryption.yml names; test_observe_reads_network_read_only, test_the_fips_read_only_reads and test_the_root_encryption_read_only_reads hold it to those reads", read)
	add("managedos_install_anaconda", "host_security.yml", "ansible.builtin.set_fact", "names the FIPS answers in memory", read)
	add("managedos_install_anaconda", "root_encryption.yml", "ansible.builtin.set_fact", "names the root-encryption answers in memory", read)
	add("managedos_install_anaconda", "registration_observe.yml", observeCollection+"ssh_key_policy", "reads the host's effective crypto policy", read)
	add("managedos_install_anaconda", "registration_observe.yml", "ansible.builtin.set_fact", "names the pinned key's algorithms and the registration answers in memory", read)
	add("managedos_install_anaconda", "registration_observe.yml", observeCollection+"managedos_registration", "runs the registration worker's read-only inspect operation over ssh pinned to the reported key", observeRegistrationInspect)
	add("managedos_package_tree", "observe.yml", observeCollection+"managedos_tree_inspect", "reads the shared tree and its staging area", read)
	add("storage_ceph_core", "observe.yml", observeCollection+"ceph_cluster", "runs the adapter's read-only observe operation", observeOperation)
	add("storage_ceph_host", "observe.yml", observeCollection+"ceph_host", "runs the adapter's read-only observe operation", observeOperation)
	add("storage_ceph_image", "observe.yml", observeCollection+"ceph_image_resolve", "re-validates the frozen resolution offline", observeOperation)
	add("storage_ceph_smb", "observe.yml", observeCollection+"ceph_smb", "runs the adapter's read-only observe operation", observeOperation)
	add("storage_ceph_replication", "observe.yml", observeCollection+"ceph_replication", "reads both endpoints without native effects or marker writes", observeOperation)
	add("substrate_baremetal_machine", "observe.yml", observeCollection+"redfish_system_inspect", "reads the machine from its controller", read)
	add("substrate_baremetal_machine", "identity_read.yml", observeCollection+"ssh_key_policy", "reads the host's effective crypto policy", read)
	add("substrate_baremetal_machine", "identity_read.yml", "ansible.builtin.set_fact", "names the answers in memory", read)
	add("substrate_baremetal_machine", "identity_read.yml", "ansible.builtin.command", "runs /usr/bin/cat of the marker over ssh pinned to the delivered key", read)
	add("substrate_libvirt_host", "observe.yml", observeCollection+"libvirt_host_inspect", "reads the provider host", read)
	add("substrate_libvirt_machine", "observe.yml", observeCollection+"libvirt_domain_inspect", "reads the domain", read)
	add("substrate_libvirt_machine", "observe.yml", observeCollection+"redfish_system_read", "reads the machine's power from its controller", read)
	add("substrate_libvirt_machine", "identity_read.yml", observeCollection+"guest_agent_read", "reads a guest file through the guest agent", read)
	add("substrate_libvirt_machine", "identity_read.yml", "ansible.builtin.set_fact", "publishes the answers in memory", read)
}

func addObserveScratch(add func(string, string, string, string, func(map[string]any, map[string]any) bool), read func(map[string]any, map[string]any) bool) {
	add("managedos_install_anaconda", "observe.yml", observeCollection+"lifecycle_scratch", "creates and removes this invocation's private bootwright_run_scratch", observeScratchRoot)
	add("managedos_install_anaconda", "work.yml", "ansible.builtin.file", "creates a work ancestor only when the caller sets managedos_install_anaconda_work_create, which observe sets false", observeWorkCreate)
	add("managedos_install_anaconda", "reachable.yml", "ansible.builtin.copy", "writes one connection's files under bootwright_run_scratch", observeUnder("dest", "{{ managedos_install_anaconda_scratch }}/"))
	add("managedos_install_anaconda", "reachable.yml", "ansible.builtin.file", "removes one connection's files under bootwright_run_scratch", observeUnder("path", "{{ managedos_install_anaconda_scratch }}/"))
	add("managedos_install_anaconda", "guest.yml", "ansible.builtin.copy", "writes one connection's files under bootwright_run_scratch", observeUnder("dest", "{{ managedos_install_anaconda_scratch }}/"))
	add("managedos_install_anaconda", "guest.yml", "ansible.builtin.file", "removes one connection's files under bootwright_run_scratch", observeUnder("path", "{{ managedos_install_anaconda_scratch }}/"))
	add("managedos_install_anaconda", "registration_observe.yml", "ansible.builtin.copy", "writes one connection's files under bootwright_run_scratch", observeUnder("dest", "{{ managedos_install_anaconda_scratch }}/"))
	add("managedos_install_anaconda", "registration_observe.yml", "ansible.builtin.file", "removes one connection's files under bootwright_run_scratch", observeUnder("path", "{{ managedos_install_anaconda_scratch }}/"))
	add("substrate_baremetal_machine", "identity_read.yml", "ansible.builtin.copy", "writes one connection's files under the work directory the caller passes, bootwright_run_scratch on observe", observeUnder("dest", "{{ substrate_baremetal_machine_identity_work }}/"))
	add("substrate_baremetal_machine", "identity_read.yml", "ansible.builtin.file", "removes one connection's files under the work directory the caller passes", observeUnder("path", "{{ substrate_baremetal_machine_identity_work }}/"))
}

func observeParams(task map[string]any, module string) map[string]any {
	params, _ := task[module].(map[string]any)
	return params
}

func observeOperation(params, _ map[string]any) bool {
	return params["operation"] == "observe"
}

func observeRegistrationInspect(params, _ map[string]any) bool {
	return params["operation"] == "inspect"
}

func observeScratchRoot(params, _ map[string]any) bool {
	return params["root"] == "{{ bootwright_run_scratch }}"
}

func observeWhens(task map[string]any) []string {
	switch when := task["when"].(type) {
	case string:
		return []string{when}
	case []any:
		var conditions []string
		for _, condition := range when {
			conditions = append(conditions, fmt.Sprint(condition))
		}
		return conditions
	}
	return nil
}

func observeCustodyCopy(_, task map[string]any) bool {
	return slices.ContainsFunc(observeWhens(task), observeCustodyWhen.MatchString)
}

func observeWorkCreate(_, task map[string]any) bool {
	return slices.Contains(observeWhens(task), "managedos_install_anaconda_work_create | bool")
}

func observeUnder(field, prefix string) func(map[string]any, map[string]any) bool {
	return func(params, _ map[string]any) bool {
		value, ok := params[field].(string)
		return ok && strings.HasPrefix(value, prefix) && !strings.Contains(value, "..")
	}
}

type observeWalk struct {
	assets  map[string][]byte
	allow   map[observeTaskKey]observeAllowance
	used    map[observeTaskKey]bool
	visited map[string]bool
	errs    []string
}

func (w *observeWalk) reportf(format string, args ...any) {
	w.errs = append(w.errs, fmt.Sprintf(format, args...))
}

func (w *observeWalk) playbook(key, playbook string) {
	var plays []map[string]any
	if err := yaml.Unmarshal(w.assets[playbookRoot+playbook], &plays); err != nil || len(plays) != 1 {
		w.reportf("%s: %s is not one play (%v)", key, playbook, err)
		return
	}
	for field := range plays[0] {
		if !slices.Contains(observePlayKeys, field) {
			w.reportf("%s: %s carries play keyword %q", key, playbook, field)
		}
	}
	tasks, _ := plays[0]["tasks"].([]any)
	if len(tasks) != 1 {
		w.reportf("%s: %s runs %d tasks, not one import_role", key, playbook, len(tasks))
		return
	}
	task, _ := tasks[0].(map[string]any)
	params := observeParams(task, "ansible.builtin.import_role")
	role, ok := params["name"].(string)
	if !ok || params["tasks_from"] != "observe" || len(params) != 2 {
		w.reportf("%s: %s is not a single import_role with tasks_from: observe", key, playbook)
		return
	}
	w.role(key, role, "observe")
}

func (w *observeWalk) role(key, role, entry string) {
	name, ok := strings.CutPrefix(role, observeCollection)
	if !ok || strings.Contains(name, ".") {
		w.reportf("%s: includes role %q outside the collection", key, role)
		return
	}
	w.file(key, name, entry+".yml")
}

func (w *observeWalk) file(key, role, file string) {
	path := observeRoleRoot + role + "/tasks/" + file
	if w.visited[path] {
		return
	}
	w.visited[path] = true
	data, ok := w.assets[path]
	if !ok {
		w.reportf("%s: %s/tasks/%s is not in the embedded collection", key, role, file)
		return
	}
	var tasks []any
	if err := yaml.Unmarshal(data, &tasks); err != nil {
		w.reportf("%s: %s/tasks/%s does not parse: %v", key, role, file, err)
		return
	}
	w.tasks(key, role, file, tasks)
}

func (w *observeWalk) tasks(key, role, file string, tasks []any) {
	for _, item := range tasks {
		task, ok := item.(map[string]any)
		if !ok {
			w.reportf("%s: %s/tasks/%s holds a task that is not a mapping", key, role, file)
			continue
		}
		w.task(key, role, file, task)
	}
}

func (w *observeWalk) task(key, role, file string, task map[string]any) {
	if _, ok := task["block"]; ok {
		for _, section := range []string{"block", "rescue", "always"} {
			nested, _ := task[section].([]any)
			w.tasks(key, role, file, nested)
		}
		return
	}
	var modules []string
	for field := range task {
		if !slices.Contains(observeTaskKeys, field) {
			modules = append(modules, field)
		}
	}
	if len(modules) != 1 {
		w.reportf("%s: %s/tasks/%s task %q names modules %v", key, role, file, task["name"], modules)
		return
	}
	module := modules[0]
	switch {
	case slices.Contains(observeImports, module):
		w.include(key, role, file, task[module])
	case slices.Contains(observeRoles, module):
		params := observeParams(task, module)
		name, _ := params["name"].(string)
		entry, _ := params["tasks_from"].(string)
		w.role(key, name, entry)
	default:
		w.module(key, observeTaskKey{role, file, module}, task)
	}
}

func (w *observeWalk) include(key, role, file string, target any) {
	if params, ok := target.(map[string]any); ok {
		target = params["file"]
	}
	name, ok := target.(string)
	if !ok || strings.Contains(name, "{{") || strings.Contains(name, "/") {
		w.reportf("%s: %s/tasks/%s includes %v, not a literal sibling task file", key, role, file, target)
		return
	}
	w.file(key, role, name)
}

func (w *observeWalk) module(key string, at observeTaskKey, task map[string]any) {
	allowance, ok := w.allow[at]
	if !ok {
		w.reportf("%s: %s/tasks/%s runs %s, which the read-only allowlist does not name", key, at.role, at.file, at.module)
		return
	}
	w.used[at] = true
	if !allowance.holds(observeParams(task, at.module), task) {
		w.reportf("%s: %s/tasks/%s runs %s (%q) outside its constraint: %s", key, at.role, at.file, at.module, task["name"], allowance.reason)
	}
}

func walkObservePlaybooks(assets map[string][]byte) []string {
	walk := &observeWalk{assets: assets, allow: observeAllowlist(), used: map[observeTaskKey]bool{}}
	bindings := operationPlaybook()
	for _, key := range slices.Sorted(maps.Keys(bindings)) {
		if strings.HasSuffix(key, "/observe") {
			walk.visited = map[string]bool{}
			walk.playbook(key, bindings[key])
		}
	}
	for at := range walk.allow {
		if !walk.used[at] {
			walk.reportf("allowlist names %s/tasks/%s %s, which no observe entrypoint reaches", at.role, at.file, at.module)
		}
	}
	slices.Sort(walk.errs)
	return walk.errs
}

func observeAssetsWith(path, appended string) map[string][]byte {
	assets := maps.Clone(ansible.Assets())
	assets[path] = append(slices.Clone(assets[path]), appended...)
	return assets
}

func TestEveryObservePlaybookRunsOnlyAllowedTasks(t *testing.T) {
	for _, problem := range walkObservePlaybooks(ansible.Assets()) {
		t.Error(problem)
	}

	t.Run("a planted package task is reported", func(t *testing.T) {
		planted := "\n- name: Install a package\n  ansible.builtin.dnf:\n    name: podman\n    state: present\n"
		problems := walkObservePlaybooks(observeAssetsWith(observeRoleRoot+"storage_ceph_image/tasks/observe.yml", planted))
		if !slices.ContainsFunc(problems, func(problem string) bool {
			return strings.Contains(problem, "storage_ceph_image/tasks/observe.yml runs ansible.builtin.dnf")
		}) {
			t.Fatalf("the planted dnf task was not reported: %v", problems)
		}
	})

	t.Run("an adapter call naming another operation is reported", func(t *testing.T) {
		planted := "\n- name: Apply instead\n  bootwright.core.ceph_cluster:\n    operation: apply\n"
		problems := walkObservePlaybooks(observeAssetsWith(observeRoleRoot+"storage_ceph_core/tasks/observe.yml", planted))
		if !slices.ContainsFunc(problems, func(problem string) bool {
			return strings.Contains(problem, `ceph_cluster ("Apply instead") outside its constraint`)
		}) {
			t.Fatalf("the apply operation was not reported: %v", problems)
		}
	})

	t.Run("a token mutation is reported", func(t *testing.T) {
		planted := "\n- name: Publish a token\n  bootwright.core.containercluster_media_token:\n    operation: publish\n"
		problems := walkObservePlaybooks(observeAssetsWith(observeRoleRoot+"containercluster_install_agent/tasks/state.yml", planted))
		if !slices.ContainsFunc(problems, func(problem string) bool {
			return strings.Contains(problem, `containercluster_media_token ("Publish a token") outside its constraint`)
		}) {
			t.Fatalf("the token publication was not reported: %v", problems)
		}
	})

	t.Run("a token sweep is reported", func(t *testing.T) {
		planted := "\n- name: Sweep the tokens\n  bootwright.core.containercluster_media_token:\n    presented: []\n    operation: sweep\n"
		problems := walkObservePlaybooks(observeAssetsWith(observeRoleRoot+"containercluster_install_agent/tasks/state.yml", planted))
		if !slices.ContainsFunc(problems, func(problem string) bool {
			return strings.Contains(problem, `containercluster_media_token ("Sweep the tokens") outside its constraint`)
		}) {
			t.Fatalf("the token sweep was not reported: %v", problems)
		}
	})

	t.Run("a custody copy no output gates is reported", func(t *testing.T) {
		planted := "\n- name: Copy anyway\n  ansible.builtin.copy:\n    src: /etc/passwd\n    dest: /root/passwd\n"
		problems := walkObservePlaybooks(observeAssetsWith(observeRoleRoot+"containercluster_media_agent/tasks/observe.yml", planted))
		if !slices.ContainsFunc(problems, func(problem string) bool {
			return strings.Contains(problem, `copy ("Copy anyway") outside its constraint`)
		}) {
			t.Fatalf("the ungated copy was not reported: %v", problems)
		}
	})
}

func TestPhysicalEraseObservationRefusesEffectPhases(t *testing.T) {
	for _, phase := range []string{"prepare", "build", "start", "await", "ack", "stop-proof", "finish"} {
		planted := "\n- name: Mutate erase custody\n  bootwright.core.physical_erase:\n    phase: " + phase + "\n"
		problems := walkObservePlaybooks(observeAssetsWith(observeRoleRoot+"physical_erase/tasks/observe.yml", planted))
		if !slices.ContainsFunc(problems, func(problem string) bool {
			return strings.Contains(problem, `physical_erase ("Mutate erase custody") outside its constraint`)
		}) {
			t.Fatalf("effect phase %s was admitted: %v", phase, problems)
		}
	}
	planted := "\n- name: Prepare a receipt\n  bootwright.core.physical_erase_protocol:\n    phase: prepared\n"
	problems := walkObservePlaybooks(observeAssetsWith(observeRoleRoot+"physical_erase/tasks/observe.yml", planted))
	if !slices.ContainsFunc(problems, func(problem string) bool {
		return strings.Contains(problem, `physical_erase_protocol ("Prepare a receipt") outside its constraint`)
	}) {
		t.Fatalf("write handshake was admitted: %v", problems)
	}
}
