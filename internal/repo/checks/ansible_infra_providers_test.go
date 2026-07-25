package repocheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/roles"
)

func TestPackageRemovalGuardedByOwnershipAndRequirements(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/package_remove_one.yml")
	remove := tasks[findAnsibleTask(t, tasks, "Remove package that Bootwright introduced")]
	when := fmt.Sprint(remove["when"])
	if !strings.Contains(when, "not (bootwright_ownership_package_record.preexisting") || !strings.Contains(when, "preexisting | default(true)") {
		t.Fatalf("package removal must be gated on preexisting defaulting to true (keep), got when=%v", remove["when"])
	}
	if !strings.Contains(when, "bootwright_ownership_package_remaining_required_by") || !strings.Contains(when, "length == 0") {
		t.Fatalf("package removal must be gated on an empty remaining requiredBy, got when=%v", remove["when"])
	}
}

func TestAuthorizationMembershipRejectsEmptyName(t *testing.T) {
	for _, path := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/main.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/apply_mode.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/install.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/tasks/probe_existing.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/layout.yml",
	} {
		body := readRepoFile(t, path)
		if !strings.Contains(body, "split(',') | map('trim') | reject('equalto', '')") {
			t.Fatalf("%s must reject empty entries from its authorization-list split so an empty cluster name fails closed ('' in [''] is true)", path)
		}
	}
}

func TestInfraDestroyPlaysHonorSkipUnreachable(t *testing.T) {
	for _, path := range []string{
		"ansible/collections/ansible_collections/bootwright/core/playbooks/check_become.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_machine_infra_destroy.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_infra_component_services_destroy.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_provider_services_destroy.yml",
	} {
		plays := readAnsiblePlays(t, path)
		if len(plays) != 1 {
			t.Fatalf("%s play count = %d, want 1", path, len(plays))
		}
		ignoreUnreachable, ok := plays[0]["ignore_unreachable"].(string)
		if !ok || !strings.Contains(ignoreUnreachable, "bootwright_destroy_skip_unreachable") {
			t.Fatalf("%s must template ignore_unreachable from bootwright_destroy_skip_unreachable so --skip-unreachable reaches the infra stage, got %v", path, plays[0]["ignore_unreachable"])
		}
	}
}

func TestMachineInfraPreparePreparesHostPackages(t *testing.T) {
	plays := readAnsiblePlays(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_machine_infra_prepare.yml")
	if len(plays) != 1 {
		t.Fatalf("machine infra prepare play count = %d, want 1", len(plays))
	}
	rawTasks, ok := plays[0]["tasks"].([]any)
	if !ok {
		t.Fatalf("machine infra prepare play missing tasks: %v", plays[0])
	}
	tasks := make([]map[string]any, 0, len(rawTasks))
	for _, raw := range rawTasks {
		task, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("task is not a map: %v", raw)
		}
		tasks = append(tasks, task)
	}

	baseIdx := findAnsibleTask(t, tasks, "Apply base host packages")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve selected container cluster")
	if baseIdx >= resolveIdx {
		t.Fatalf("machine infra prepare must apply base packages before resolving selected work")
	}
	importRole, ok := tasks[baseIdx]["ansible.builtin.import_role"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an import_role task", tasks[baseIdx]["name"])
	}
	if got := importRole["name"]; got != "bootwright.core.machine_base" {
		t.Fatalf("%s imports %v, want bootwright.core.machine_base", tasks[baseIdx]["name"], got)
	}
}

func TestArtifactsHTTPServiceUsesContainerNginxWithTLS(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_artifact_server_http/tasks/main.yml")
	validateIdx := findAnsibleTask(t, tasks, "Validate boot artifact server settings")
	pathsIdx := findAnsibleTask(t, tasks, "Resolve boot artifact HTTPS mode")
	packagesIdx := findAnsibleTask(t, tasks, "Install boot artifact server packages")
	dirsIdx := findAnsibleTask(t, tasks, "Create boot artifact directories")
	tlsCertIdx := findAnsibleTask(t, tasks, "Stat boot artifact TLS certificate")
	tlsKeyIdx := findAnsibleTask(t, tasks, "Stat boot artifact TLS key")
	tlsPresentIdx := findAnsibleTask(t, tasks, "Detect existing boot artifact TLS material")
	tlsConfigIdx := findAnsibleTask(t, tasks, "Render boot artifact TLS OpenSSL config")
	tlsGenerateIdx := findAnsibleTask(t, tasks, "Generate boot artifact TLS certificate")
	keyModeIdx := findAnsibleTask(t, tasks, "Restrict boot artifact TLS key")
	certModeIdx := findAnsibleTask(t, tasks, "Set boot artifact TLS certificate mode")
	nginxIdx := findAnsibleTask(t, tasks, "Render boot artifact nginx configuration")
	volumesIdx := findAnsibleTask(t, tasks, "Resolve boot artifact container volumes")
	containerIdx := findAnsibleTask(t, tasks, "Run boot artifact server container")
	firewallIdx := findAnsibleTask(t, tasks, "Open boot artifact ports on host firewall")
	waitIdx := findAnsibleTask(t, tasks, "Wait for boot artifact endpoints")
	if !(validateIdx < pathsIdx && pathsIdx < packagesIdx && packagesIdx < dirsIdx && dirsIdx < tlsCertIdx && tlsCertIdx < tlsKeyIdx && tlsKeyIdx < tlsPresentIdx && tlsPresentIdx < tlsConfigIdx && tlsConfigIdx < tlsGenerateIdx && tlsGenerateIdx < keyModeIdx && keyModeIdx < certModeIdx && certModeIdx < nginxIdx && nginxIdx < volumesIdx && volumesIdx < containerIdx && containerIdx < firewallIdx && firewallIdx < waitIdx) {
		t.Fatalf("artifacts_http must prepare TLS, render nginx, then start the container")
	}

	packages, ok := tasks[packagesIdx]["ansible.builtin.package"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no package body", tasks[packagesIdx]["name"])
	}
	if got := fmt.Sprint(packages["name"]); !strings.Contains(got, "podman") || !strings.Contains(got, "openssl") {
		t.Fatalf("artifact server packages must include podman and openssl, got %v", packages["name"])
	}

	nginx := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_artifact_server_http/templates/artifacts-nginx.conf.j2")
	for _, want := range []string{"user root;", "listen {{ listen_host }}:{{ listener.port }}", "ssl_certificate", "try_files $uri =404", "autoindex off"} {
		if !strings.Contains(nginx, want) {
			t.Fatalf("artifact nginx template missing %q", want)
		}
	}
	for _, want := range []string{
		"bootwright_component.tls.protocols | default('')",
		"ssl_protocols {{ bootwright_component.tls.protocols }};",
		"bootwright_component.tls.ciphers | default('')",
		"ssl_ciphers {{ bootwright_component.tls.ciphers }};",
	} {
		if !strings.Contains(nginx, want) {
			t.Fatalf("artifact nginx template missing TLS-relaxation directive %q", want)
		}
	}
	tlsTemplate := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_artifact_server_http/templates/artifacts-openssl.cnf.j2")
	for _, want := range []string{"subjectAltName", "bootwright_component.tls.dnsNames", "bootwright_component.tls.ipAddresses"} {
		if !strings.Contains(tlsTemplate, want) {
			t.Fatalf("artifact TLS template must render SANs; missing %q", want)
		}
	}
	if when := fmt.Sprint(tasks[tlsConfigIdx]["when"]); strings.Contains(when, "bootwright_artifacts_tls_material_present") {
		t.Fatalf("%s must render unconditionally to detect SAN/CN drift, got when=%v", tasks[tlsConfigIdx]["name"], tasks[tlsConfigIdx]["when"])
	}
	genWhen := fmt.Sprint(tasks[tlsGenerateIdx]["when"])
	if !strings.Contains(genWhen, "not (bootwright_artifacts_tls_material_present") {
		t.Fatalf("%s must preserve existing TLS material, got when=%v", tasks[tlsGenerateIdx]["name"], tasks[tlsGenerateIdx]["when"])
	}
	if !strings.Contains(genWhen, "bootwright_artifacts_tls_openssl_cnf is changed") {
		t.Fatalf("%s must rotate the certificate when the OpenSSL config changes, got when=%v", tasks[tlsGenerateIdx]["name"], tasks[tlsGenerateIdx]["when"])
	}

	container, ok := tasks[containerIdx]["containers.podman.podman_container"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no podman_container body", tasks[containerIdx]["name"])
	}
	if got := container["network"]; got != "host" {
		t.Fatalf("artifact container must use host networking, got %v", got)
	}
	recreate := fmt.Sprint(container["recreate"])
	if !strings.Contains(recreate, "bootwright_artifacts_config.changed") || !strings.Contains(recreate, "bootwright_artifacts_tls_generated.changed") {
		t.Fatalf("artifact container must recreate on config or TLS changes, got %v", container["recreate"])
	}
	volumes, ok := tasks[volumesIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(volumes["bootwright_artifacts_volumes"]), "/usr/share/nginx/html") {
		t.Fatalf("artifact container volumes must mount artifact root into nginx, got %v", tasks[volumesIdx])
	}
	waitURI, ok := tasks[waitIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", tasks[waitIdx]["name"])
	}
	if got := fmt.Sprint(waitURI["url"]); !strings.Contains(got, "{{ item.protocol }}://") || !strings.Contains(got, "{{ item.port | int }}") {
		t.Fatalf("artifact readiness probe must follow rendered listeners, got %v", got)
	}
	if got := waitURI["validate_certs"]; got != false {
		t.Fatalf("artifact readiness probe must allow self-signed certs, got %v", got)
	}
	if got := waitURI["status_code"]; got != 404 {
		t.Fatalf("artifact readiness probe must expect directory listing rejection, got %v", got)
	}
}

func TestInfraComponentContainerGateUsesLiveProvenanceLabel(t *testing.T) {
	rel := bootwrightCollectionRoleRoot + "/ownership_record/tasks/infra_component_container_gate.yml"
	body := readRepoFile(t, rel)
	for _, banned := range []string{"bootwright_infra_component_owned_stat", "Stat ownership record", "bootwright_ownership_dir", "ansible.builtin.stat"} {
		if strings.Contains(body, banned) {
			t.Fatalf("container gate must not derive ownership from a per-context record; found %q", banned)
		}
	}

	tasks := readAnsibleTasks(t, rel)
	commandArgv := func(task map[string]any) string {
		command, ok := task["ansible.builtin.command"].(map[string]any)
		if !ok {
			t.Fatalf("task %q has no command body", task["name"])
		}
		return fmt.Sprint(command["argv"])
	}
	ownedProbe := tasks[findAnsibleTaskByPrefix(t, tasks, "Probe Bootwright-owned container")]
	ownedArgv := commandArgv(ownedProbe)
	if !strings.Contains(ownedArgv, "label=bootwright.provider=") || !strings.Contains(ownedArgv, "label=bootwright.name=") {
		t.Fatalf("owned probe must filter by bootwright provenance labels, got %v", ownedArgv)
	}

	nameProbe := tasks[findAnsibleTaskByPrefix(t, tasks, "Probe any same-named container")]
	nameArgv := commandArgv(nameProbe)
	if !strings.Contains(nameArgv, "name=^{{ bootwright_gate_container_name }}$") {
		t.Fatalf("foreign-squatter probe must match the exact container name, got %v", nameArgv)
	}
	if when := fmt.Sprint(nameProbe["when"]); !strings.Contains(when, "bootwright_gate_container_name") {
		t.Fatalf("foreign-squatter probe must be guarded by a non-empty container name, got when=%v", nameProbe["when"])
	}

	facts := tasks[findAnsibleTaskByPrefix(t, tasks, "Resolve apply-mode gate facts")]
	setFact, ok := facts["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("gate facts task has no set_fact body")
	}
	owned := fmt.Sprint(setFact["bootwright_gate_owned"])
	if !strings.Contains(owned, "bootwright_infra_component_owned_probe") || strings.Contains(owned, "name_probe") {
		t.Fatalf("bootwright_gate_owned must derive ONLY from the label probe, got %v", setFact["bootwright_gate_owned"])
	}
	exists := fmt.Sprint(setFact["bootwright_gate_exists"])
	if !strings.Contains(exists, "bootwright_infra_component_owned_probe") || !strings.Contains(exists, "bootwright_infra_component_name_probe") {
		t.Fatalf("bootwright_gate_exists must be label-probe OR name-probe, got %v", setFact["bootwright_gate_exists"])
	}
}

func TestInfraComponentRolesPassContainerNameToGate(t *testing.T) {
	roles := []string{
		"infra_component_artifact_server_http",
		"infra_component_registry_mirror",
		"infra_component_proxy_squid",
		"infra_component_load_balancer_haproxy",
		"infra_component_name_resolution_dnsmasq",
	}
	for _, role := range roles {
		tasks := readAnsibleTasks(t, bootwrightCollectionRoleRoot+"/"+role+"/tasks/main.yml")
		var gate map[string]any
		for _, task := range tasks {
			include, ok := task["ansible.builtin.include_role"].(map[string]any)
			if ok && fmt.Sprint(include["tasks_from"]) == "infra_component_container_gate.yml" {
				gate = task
				break
			}
		}
		if gate == nil {
			t.Fatalf("%s: no include of infra_component_container_gate.yml", role)
		}
		vars, ok := gate["vars"].(map[string]any)
		if !ok {
			t.Fatalf("%s: gate include passes no vars", role)
		}
		if name := strings.TrimSpace(fmt.Sprint(vars["bootwright_gate_container_name"])); name == "" {
			t.Fatalf("%s: gate include must pass bootwright_gate_container_name", role)
		}
	}
}

func TestRegisteredAdapterRolesExist(t *testing.T) {
	roles := registeredAdapterRoles()
	for role := range roles {
		if !ansibleRoleDirExists(t, role) {
			t.Fatalf("registered Ansible adapter role %q has no role directory", role)
		}
	}
}

func TestAdapterRoleDirectoriesAreRegistered(t *testing.T) {
	registered := registeredAdapterRoles()
	for _, role := range ansibleAdapterRoleDirs(t) {
		if !registered[role] {
			t.Fatalf("Ansible adapter role %q exists but is not in the driver registry", role)
		}
	}
}

func TestMachineServicePlaybooksDispatchRenderedRoles(t *testing.T) {
	for _, path := range []string{
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_provider_services_apply.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_provider_services_destroy.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_infra_component_services_apply.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_infra_component_services_destroy.yml",
	} {
		body := readRepoFile(t, path)
		for _, forbidden := range []string{
			"load_balancer_haproxy",
			"artifacts_http",
			"proxy_squid",
			"dns_dnsmasq",
			"ntp_chrony",
			"mirror_registry",
			"bmc_{{",
		} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s hardcodes provider service role %q", path, forbidden)
			}
		}
		for _, want := range []string{"bootwright_component.applyRole", "bootwright_component.destroyRole"} {
			if strings.Contains(path, "apply.yml") && want == "bootwright_component.destroyRole" {
				continue
			}
			if strings.Contains(path, "destroy.yml") && want == "bootwright_component.applyRole" {
				continue
			}
			if !strings.Contains(body, want) {
				t.Fatalf("%s must dispatch using rendered %s", path, want)
			}
		}
	}
}

func TestServiceRecordDestroyScopedByClusterScope(t *testing.T) {
	cases := []struct {
		path string
		task string
	}{
		{
			path: "ansible/collections/ansible_collections/bootwright/core/playbooks/task_infra_component_services_destroy.yml",
			task: "Destroy recorded infra component resources",
		},
		{
			path: "ansible/collections/ansible_collections/bootwright/core/playbooks/task_provider_services_destroy.yml",
			task: "Destroy recorded provider resources",
		},
	}
	for _, tc := range cases {
		plays := readAnsiblePlays(t, tc.path)
		if len(plays) != 1 {
			t.Fatalf("%s plays = %d, want 1", tc.path, len(plays))
		}
		tasks := nestedAnsibleTasks(t, plays[0], "tasks")
		recorded := tasks[findAnsibleTask(t, tasks, tc.task)]
		when := fmt.Sprint(recorded["when"])
		if !strings.Contains(when, "bootwright_destroy_cluster_scope") {
			t.Fatalf("%s task %q record cleanup must be scoped by bootwright_destroy_cluster_scope so a scoped infra destroy leaves a sibling cluster's services standing: %v", tc.path, tc.task, recorded["when"])
		}
		if !strings.Contains(when, "record_names") {
			t.Fatalf("%s task %q must match the record name against the in-scope service allowlist: %v", tc.path, tc.task, recorded["when"])
		}
	}
}

func TestInfraComponentDestroyCleanupUsesBootwrightPodmanLabels(t *testing.T) {
	roles := map[string]string{
		"ansible/collections/ansible_collections/bootwright/core/roles/infra_component_artifact_server_http/tasks/destroy.yml":    "artifacts",
		"ansible/collections/ansible_collections/bootwright/core/roles/infra_component_load_balancer_haproxy/tasks/destroy.yml":   "load-balancer",
		"ansible/collections/ansible_collections/bootwright/core/roles/infra_component_name_resolution_dnsmasq/tasks/destroy.yml": "nameResolution",
		"ansible/collections/ansible_collections/bootwright/core/roles/infra_component_proxy_squid/tasks/destroy.yml":             "proxy",
		"ansible/collections/ansible_collections/bootwright/core/roles/infra_component_registry_mirror/tasks/destroy.yml":         "registry",
	}
	helper := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/support_component_teardown/tasks/container.yml")
	if strings.Contains(helper, "bootwright_process_cleanup_pattern") {
		t.Fatalf("component teardown helper must not cleanup provider containers by process pattern")
	}
	for _, want := range []string{
		"bootwright_process_cleanup_podman_filters:",
		"label=bootwright.kind={{ bootwright_component_teardown_kind }}",
		"label=bootwright.provider={{ bootwright_component.providerName }}",
		"label=bootwright.name={{ bootwright_component.name }}",
	} {
		if !strings.Contains(helper, want) {
			t.Fatalf("component teardown helper cleanup missing %q", want)
		}
	}
	for path, kind := range roles {
		body := readRepoFile(t, path)
		if strings.Contains(body, "bootwright_process_cleanup_pattern") {
			t.Fatalf("%s must not cleanup provider containers by process pattern", path)
		}
		for _, want := range []string{
			"bootwright.core.support_component_teardown",
			"tasks_from: container.yml",
			"bootwright_component_teardown_kind: " + kind,
			"bootwright_component_teardown_container:",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s cleanup missing %q", path, want)
			}
		}
	}
}

func TestLibvirtNetworkUsesResolvedNTPIPv4Sources(t *testing.T) {
	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/templates/network.xml.j2")
	for _, want := range []string{
		"bootwright_resolved_ntp_sources",
		"select('match', '^[0-9]+\\\\.[0-9]+\\\\.[0-9]+\\\\.[0-9]+$')",
		"dhcp-option=option:ntp-server",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("libvirt network template missing %q", want)
		}
	}
}

func TestLibvirtNetworkEntrypointBuildsMachineListLocally(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/network.yml")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve libvirt network machines")
	renderIdx := findAnsibleTask(t, tasks, "Render libvirt network XML")
	if resolveIdx > renderIdx {
		t.Fatalf("libvirt network machines must be resolved before rendering network XML")
	}
	setFact, ok := tasks[resolveIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[resolveIdx]["name"])
	}
	expr, ok := setFact["bootwright_libvirt_network_machines"].(string)
	if !ok {
		t.Fatalf("bootwright_libvirt_network_machines fact = %v", setFact["bootwright_libvirt_network_machines"])
	}
	for _, want := range []string{"bootwright_current_machines", "bootwright_prepare_components", "[bootwright_component]"} {
		if !strings.Contains(expr, want) {
			t.Fatalf("libvirt network machine list expression missing %q: %s", want, expr)
		}
	}
	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/templates/network.xml.j2")
	if !strings.Contains(body, "bootwright_libvirt_network_machines") {
		t.Fatalf("libvirt network template must iterate the local network machine list")
	}
	if strings.Contains(body, "bootwright_current_machines") {
		t.Fatalf("libvirt network template must not depend on caller-scoped bootwright_current_machines")
	}
}

func TestLibvirtStorageStaysOutOfPrivateBootwrightState(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml")
	domainXML := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/templates/domain.xml.j2")

	probeIdx := findAnsibleTask(t, tasks, "Probe libvirt runtime users")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve libvirt runtime storage access")
	stopResetIdx := findAnsibleTask(t, tasks, "Stop managed OS libvirt domain for override reinstall")
	undefineResetIdx := findAnsibleTask(t, tasks, "Undefine managed OS libvirt domain for override reinstall")
	removeResetIdx := findAnsibleTask(t, tasks, "Remove managed OS libvirt machine state for override reinstall")
	recreateResetIdx := findAnsibleTask(t, tasks, "Recreate managed OS libvirt machine directories after override reset")
	selectIdx := findAnsibleTask(t, tasks, "Select libvirt runtime user")
	assertIdx := findAnsibleTask(t, tasks, "Assert libvirt runtime user")
	migrateDiskIdx := findAnsibleTask(t, tasks, "Migrate machine disk to libvirt storage")
	migrateDataIdx := findAnsibleTaskByPrefix(t, tasks, "Migrate machine data disks to libvirt storage")
	createDiskIdx := findAnsibleTask(t, tasks, "Create machine disk")
	createDataDiskIdx := findAnsibleTaskByPrefix(t, tasks, "Create machine data disks")
	ownDiskIdx := findAnsibleTask(t, tasks, "Align libvirt disk image ownership")
	renderDomainIdx := findAnsibleTask(t, tasks, "Render libvirt domain XML")

	if !(probeIdx < resolveIdx && resolveIdx < stopResetIdx && stopResetIdx < undefineResetIdx && undefineResetIdx < removeResetIdx && removeResetIdx < recreateResetIdx && recreateResetIdx < selectIdx && selectIdx < assertIdx && assertIdx < migrateDiskIdx && migrateDiskIdx < createDiskIdx && createDataDiskIdx < ownDiskIdx && ownDiskIdx < renderDomainIdx) {
		t.Fatalf("libvirt storage tasks must migrate/create/own disks before domain definition")
	}
	if migrateDataIdx > createDataDiskIdx {
		t.Fatalf("data disk migration must run before data disk creation")
	}

	probe, ok := tasks[probeIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no command body", tasks[probeIdx]["name"])
	}
	for _, want := range []string{"getent", "passwd"} {
		if !strings.Contains(fmt.Sprint(probe["argv"]), want) {
			t.Fatalf("%s argv missing %q: %v", tasks[probeIdx]["name"], want, probe["argv"])
		}
	}
	for _, want := range []string{"qemu", "libvirt-qemu"} {
		if !strings.Contains(fmt.Sprint(tasks[probeIdx]["loop"]), want) {
			t.Fatalf("%s loop missing %q: %v", tasks[probeIdx]["name"], want, tasks[probeIdx]["loop"])
		}
	}

	resolve, ok := tasks[resolveIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[resolveIdx]["name"])
	}
	for _, key := range []string{"bootwright_libvirt_runtime_users", "bootwright_libvirt_domain_name", "bootwright_libvirt_managed_os_reset", "bootwright_libvirt_disk_paths"} {
		if _, ok := resolve[key]; !ok {
			t.Fatalf("%s missing %s", tasks[resolveIdx]["name"], key)
		}
	}
	if got := fmt.Sprint(resolve["bootwright_libvirt_managed_os_reset"]); !strings.Contains(got, "bootwright_component.osManaged") || !strings.Contains(got, "bootwright_apply_mode") || !strings.Contains(got, "bootwright_substrate_reset_clusters") {
		t.Fatalf("%s must gate managed OS disk reset on osManaged, the override apply mode, and the drift-authorized substrate_reset_clusters list, got %v", tasks[resolveIdx]["name"], got)
	}
	if !strings.Contains(fmt.Sprint(resolve["bootwright_libvirt_disk_paths"]), "bootwright_component.profile.dataDisks") {
		t.Fatalf("%s must include data disk paths, got %v", tasks[resolveIdx]["name"], resolve["bootwright_libvirt_disk_paths"])
	}
	if !strings.Contains(fmt.Sprint(resolve["bootwright_libvirt_disk_paths"]), "bootwright_libvirt_storage_root") {
		t.Fatalf("%s must point disk paths at libvirt storage, got %v", tasks[resolveIdx]["name"], resolve["bootwright_libvirt_disk_paths"])
	}

	for _, idx := range []int{stopResetIdx, undefineResetIdx, removeResetIdx, recreateResetIdx} {
		if got := tasks[idx]["when"]; got != "bootwright_libvirt_managed_os_reset | bool" {
			t.Fatalf("%s when got %v", tasks[idx]["name"], got)
		}
	}
	removeReset, ok := tasks[removeResetIdx]["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no file body", tasks[removeResetIdx]["name"])
	}
	if got := removeReset["state"]; got != "absent" {
		t.Fatalf("%s state got %v", tasks[removeResetIdx]["name"], got)
	}
	for _, want := range []string{"bootwright_libvirt_machine_root", "bootwright_libvirt_storage_root"} {
		if !strings.Contains(fmt.Sprint(tasks[removeResetIdx]["loop"]), want) {
			t.Fatalf("%s loop missing %q: %v", tasks[removeResetIdx]["name"], want, tasks[removeResetIdx]["loop"])
		}
	}

	ownership, ok := tasks[ownDiskIdx]["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no file body", tasks[ownDiskIdx]["name"])
	}
	if got := ownership["owner"]; got != "{{ bootwright_libvirt_runtime_user }}" {
		t.Fatalf("%s owner got %v", tasks[ownDiskIdx]["name"], got)
	}
	if got := ownership["mode"]; got != "0600" {
		t.Fatalf("%s mode got %v", tasks[ownDiskIdx]["name"], got)
	}
	if got := tasks[ownDiskIdx]["loop"]; got != "{{ bootwright_libvirt_disk_paths }}" {
		t.Fatalf("%s loop got %v", tasks[ownDiskIdx]["name"], got)
	}

	if !strings.Contains(domainXML, "<source file='{{ bootwright_libvirt_storage_root }}/disk.qcow2'/>") {
		t.Fatalf("domain XML must source root disk from libvirt storage")
	}
	if !strings.Contains(domainXML, "<source file='{{ bootwright_libvirt_storage_root }}/data-{{ disk.name }}.qcow2'/>") {
		t.Fatalf("domain XML must source data disks from libvirt storage")
	}
	if strings.Contains(domainXML, "<source file='{{ bootwright_libvirt_machine_root }}/disk.qcow2'/>") {
		t.Fatalf("domain XML must not source disks from private Bootwright state")
	}
	for _, want := range []string{
		"<metadata>",
		"bootwright:context",
		"{{ bootwright_clusters_dir | dirname | basename }}",
		"bootwright:cluster",
		"bootwright:machine",
	} {
		if !strings.Contains(domainXML, want) {
			t.Fatalf("domain XML missing context ownership metadata %q", want)
		}
	}
}

func TestLibvirtApplyRequiresConclusiveDomainProbe(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml")
	probeIdx := findAnsibleTask(t, tasks, "Require a conclusive libvirt domain probe")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve libvirt domain ownership for apply")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse to mutate a non-Bootwright libvirt domain on apply")
	if !(probeIdx < resolveIdx && probeIdx < refuseIdx) {
		t.Fatalf("conclusive-probe assert (task %d) must run before ownership resolve (%d) and refusal (%d) so an inconclusive dumpxml fails closed", probeIdx, resolveIdx, refuseIdx)
	}
	if _, gated := tasks[probeIdx]["when"]; gated {
		t.Fatalf("conclusive-probe assert must not be gated on rc==0 or it cannot catch an inconclusive probe, got when=%v", tasks[probeIdx]["when"])
	}
	assert, ok := tasks[probeIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("conclusive-probe must be an assert, got %v", tasks[probeIdx])
	}
	that := fmt.Sprint(assert["that"])
	if !strings.Contains(that, "rc | default(1) == 0") || !strings.Contains(that, "Domain not found") || !strings.Contains(that, "failed to get domain") {
		t.Fatalf("conclusive-probe must accept only rc==0 or a domain-absent stderr, got that=%v", that)
	}
}

func TestVsphereManagedOSResetGatedOnDriftAuthorization(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/layout.yml")
	idx := findAnsibleTaskByPrefix(t, tasks, "Resolve")
	resolve, ok := tasks[idx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[idx]["name"])
	}
	got := fmt.Sprint(resolve["bootwright_vsphere_managed_os_reset"])
	for _, want := range []string{"bootwright_component.osManaged", "bootwright_apply_mode", "bootwright_substrate_reset_clusters"} {
		if !strings.Contains(got, want) {
			t.Fatalf("vSphere managed-OS reset must gate on %q so a matching VM is never wiped, got %v", want, got)
		}
	}
}

func TestInfraDestroySweepsCurrentContextLibvirtDomainsOnlyWhenUnscoped(t *testing.T) {
	plays := readAnsiblePlays(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_machine_infra_destroy.yml")
	if len(plays) != 1 {
		t.Fatalf("task_machine_infra_destroy plays = %d, want 1", len(plays))
	}
	if got := plays[0]["hosts"]; got != "bootwright_provider_hosts:bootwright_infra_hosts" {
		t.Fatalf("machine infra destroy hosts = %v", got)
	}
	tasks := nestedAnsibleTasks(t, plays[0], "tasks")
	sweep := tasks[findAnsibleTask(t, tasks, "Sweep current-context libvirt domains")]
	when := fmt.Sprint(sweep["when"])
	for _, want := range []string{
		"bootwright_infra_destroy_context_sweep",
		"bootwright_host_libvirt_providers",
	} {
		if !strings.Contains(when, want) {
			t.Fatalf("context sweep when missing %q: %v", want, sweep["when"])
		}
	}

	recorded := tasks[findAnsibleTask(t, tasks, "Destroy recorded machine infrastructure resources")]
	recordedWhen := fmt.Sprint(recorded["when"])
	if !strings.Contains(recordedWhen, "bootwright_destroy_cluster_scope") {
		t.Fatalf("recorded-resource destroy must be scoped by bootwright_destroy_cluster_scope: %v", recorded["when"])
	}

	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/provider_host_libvirt/tasks/destroy_context.yml")
	for _, want := range []string{
		"/var/lib/libvirt/images/bootwright/{{ bootwright_clusters_dir | dirname | basename }}/clusters",
		"virsh",
		"domblklist",
		"bootwright_libvirt_context_storage_root ~ '/'",
		"destroy",
		"undefine",
		"Remove current-context libvirt storage directory",
		"dumpxml",
		"([A-Za-z0-9_]+:)?context>",
		"bootwright_libvirt_context_owned_domains",
		"item.bootwright_libvirt_domain_name in bootwright_libvirt_context_owned_domains",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("libvirt context sweep missing %q", want)
		}
	}
}

func TestLibvirtContextSweepPreservesForeignDiskUnderRoot(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/provider_host_libvirt/tasks/destroy_context.yml")

	blanket := tasks[findAnsibleTask(t, tasks, "Remove current-context libvirt storage directory")]
	if got := fmt.Sprint(blanket["when"]); !strings.Contains(got, "bootwright_libvirt_context_foreign_storage") || !strings.Contains(got, "length == 0") {
		t.Fatalf("blanket context-root removal must be gated on no foreign domain using the root, got when=%v", blanket["when"])
	}

	owned := tasks[findAnsibleTask(t, tasks, "Remove owned current-context libvirt machine directories")]
	ownedFile, ok := owned["ansible.builtin.file"].(map[string]any)
	if !ok || ownedFile["state"] != "absent" {
		t.Fatalf("owned machine-dir removal must be a file state=absent task, got %v", owned)
	}
	if got := fmt.Sprint(owned["loop"]); !strings.Contains(got, "bootwright_libvirt_context_owned_machine_dirs") {
		t.Fatalf("owned machine-dir removal must loop over the owned machine dirs, got loop=%v", owned["loop"])
	}
	if got := fmt.Sprint(owned["when"]); !strings.Contains(got, "bootwright_libvirt_context_foreign_storage") || !strings.Contains(got, "length > 0") {
		t.Fatalf("owned machine-dir removal must run only when a foreign domain co-resides, got when=%v", owned["when"])
	}
}

func TestKubeVirtResourcesCarryContextLabels(t *testing.T) {
	for _, path := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/templates/virtualmachine.yaml.j2",
		"ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/templates/datavolume-root.yaml.j2",
	} {
		body := readRepoFile(t, path)
		if !strings.Contains(body, "bootwright.io/context: {{ bootwright_clusters_dir | dirname | basename }}") {
			t.Fatalf("%s missing context ownership label", path)
		}
	}
}

func TestKubeVirtApplyPreservesRuntimeState(t *testing.T) {
	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/templates/virtualmachine.yaml.j2")
	if !strings.Contains(body, "runStrategy: Manual") {
		t.Fatal("KubeVirt VM apply must use Manual runStrategy so declarative reconciliation does not stop a running guest")
	}
	if strings.Contains(body, "running: false") {
		t.Fatal("KubeVirt VM apply must not declare running=false because re-apply would stop a running guest")
	}
}

func TestKubeVirtApplyGatesLiveResourcesBeforeMutation(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml")
	vmProbeIdx := findAnsibleTask(t, tasks, "Read existing KubeVirt VirtualMachine identity")
	dvProbeIdx := findAnsibleTask(t, tasks, "Read existing KubeVirt root DataVolume identity")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve KubeVirt live resource ownership")
	vmGateIdx := findAnsibleTask(t, tasks, "Enforce KubeVirt VirtualMachine apply mode")
	dvGateIdx := findAnsibleTask(t, tasks, "Enforce KubeVirt root DataVolume apply mode")
	stopIdx := findAnsibleTask(t, tasks, "Stop KubeVirt VirtualMachine for authorized rebuild")
	vmDeleteIdx := findAnsibleTask(t, tasks, "Delete KubeVirt VirtualMachine for authorized rebuild")
	dvDeleteIdx := findAnsibleTask(t, tasks, "Delete KubeVirt root DataVolume for authorized rebuild")
	namespaceIdx := findAnsibleTask(t, tasks, "Ensure KubeVirt namespace exists")
	applyIdx := findAnsibleTask(t, tasks, "Apply KubeVirt VirtualMachine")
	if !(vmProbeIdx < resolveIdx && dvProbeIdx < resolveIdx &&
		resolveIdx < vmGateIdx && resolveIdx < dvGateIdx &&
		vmGateIdx < stopIdx && dvGateIdx < stopIdx &&
		stopIdx < vmDeleteIdx && vmDeleteIdx < dvDeleteIdx &&
		dvDeleteIdx < namespaceIdx && namespaceIdx < applyIdx) {
		t.Fatalf("KubeVirt apply must probe and gate live identity before rebuild or reconcile mutation (vmProbe=%d dvProbe=%d resolve=%d vmGate=%d dvGate=%d stop=%d vmDelete=%d dvDelete=%d namespace=%d apply=%d)", vmProbeIdx, dvProbeIdx, resolveIdx, vmGateIdx, dvGateIdx, stopIdx, vmDeleteIdx, dvDeleteIdx, namespaceIdx, applyIdx)
	}
	resolve, ok := tasks[resolveIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("KubeVirt live ownership decision must be a set_fact, got %v", tasks[resolveIdx])
	}
	for _, fact := range []string{"bootwright_kubevirt_vm_owned", "bootwright_kubevirt_root_dv_owned"} {
		got := fmt.Sprint(resolve[fact])
		for _, label := range []string{"bootwright.io/managed-by", "bootwright.io/context", "bootwright.io/cluster", "bootwright.io/node"} {
			if !strings.Contains(got, label) {
				t.Fatalf("%s must require exact live identity label %q, got %v", fact, label, resolve[fact])
			}
		}
	}
	for _, idx := range []int{vmGateIdx, dvGateIdx} {
		include, ok := tasks[idx]["ansible.builtin.include_role"].(map[string]any)
		if !ok || fmt.Sprint(include["name"]) != "bootwright.core.ownership_record" || fmt.Sprint(include["tasks_from"]) != "apply_mode_gate.yml" {
			t.Fatalf("KubeVirt live apply gate must use the shared apply-mode contract, got %v", tasks[idx])
		}
	}
	for _, idx := range []int{stopIdx, vmDeleteIdx, dvDeleteIdx} {
		if got := fmt.Sprint(tasks[idx]["when"]); !strings.Contains(got, "bootwright_kubevirt_rebuild_authorized") {
			t.Fatalf("%s must require controller-issued rebuild authorization, got when=%v", tasks[idx]["name"], tasks[idx]["when"])
		}
	}
}

func TestKubeVirtStorageClassValidatedBeforeMutation(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml")
	probeIdx := findAnsibleTask(t, tasks, "Probe configured KubeVirt storage class")
	requireIdx := findAnsibleTask(t, tasks, "Require configured KubeVirt storage class")
	namespaceIdx := findAnsibleTask(t, tasks, "Ensure KubeVirt namespace exists")
	dataVolumeIdx := findAnsibleTask(t, tasks, "Apply KubeVirt root disk DataVolume")
	if !(probeIdx < requireIdx && requireIdx < namespaceIdx && namespaceIdx < dataVolumeIdx) {
		t.Fatalf("storage class must be validated before namespace and DataVolume mutation (probe=%d require=%d namespace=%d dataVolume=%d)", probeIdx, requireIdx, namespaceIdx, dataVolumeIdx)
	}
	probe, ok := tasks[probeIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("storage class probe must be a command, got %v", tasks[probeIdx])
	}
	argv := fmt.Sprint(probe["argv"])
	if !strings.Contains(argv, "storageclass") || !strings.Contains(argv, "bootwright_kubevirt_storage_class") {
		t.Fatalf("storage class probe must query the configured class, got %v", probe["argv"])
	}
	if tasks[probeIdx]["failed_when"] != false {
		t.Fatalf("storage class probe must defer the actionable failure to the assert, got failed_when=%v", tasks[probeIdx]["failed_when"])
	}
	require, ok := tasks[requireIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("storage class requirement must be an assert, got %v", tasks[requireIdx])
	}
	if got := fmt.Sprint(require["that"]); !strings.Contains(got, "bootwright_kubevirt_storage_class_probe.rc") {
		t.Fatalf("storage class requirement must check the probe result, got %v", require["that"])
	}
	if got := fmt.Sprint(require["fail_msg"]); !strings.Contains(got, "storageClassRef") || !strings.Contains(got, "cluster oc") {
		t.Fatalf("storage class requirement must name the bad field and discovery command, got %v", require["fail_msg"])
	}
}

func TestLibvirtMachineDestroyVerifiesOwnershipMarker(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/destroy.yml")
	readIdx := findAnsibleTask(t, tasks, "Read libvirt domain ownership metadata")
	decideIdx := findAnsibleTask(t, tasks, "Resolve libvirt domain ownership for destroy")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse to destroy a non-Bootwright libvirt domain")
	stopIdx := findAnsibleTask(t, tasks, "Stop libvirt domain")
	undefineIdx := findAnsibleTask(t, tasks, "Undefine libvirt domain")
	if !(readIdx < decideIdx && decideIdx < refuseIdx && refuseIdx < stopIdx && stopIdx < undefineIdx) {
		t.Fatalf("libvirt destroy must read, decide, and verify domain ownership before stop/undefine (read=%d decide=%d refuse=%d stop=%d undefine=%d)", readIdx, decideIdx, refuseIdx, stopIdx, undefineIdx)
	}
	read, ok := tasks[readIdx]["ansible.builtin.command"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(read["argv"]), "dumpxml") {
		t.Fatalf("libvirt destroy ownership probe must run virsh dumpxml, got %v", tasks[readIdx])
	}
	decide, ok := tasks[decideIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("libvirt destroy ownership decision must be a set_fact, got %v", tasks[decideIdx])
	}
	owned := fmt.Sprint(decide["bootwright_libvirt_destroy_owned"])
	for _, want := range []string{"([A-Za-z0-9_]+:)?context>", "([A-Za-z0-9_]+:)?cluster>", "([A-Za-z0-9_]+:)?machine>"} {
		if !strings.Contains(owned, want) {
			t.Fatalf("libvirt destroy ownership decision must require the Bootwright ownership marker %q, got %v", want, decide["bootwright_libvirt_destroy_owned"])
		}
	}
	if !strings.Contains(owned, "is search(") {
		t.Fatalf("libvirt destroy ownership decision must match the marker via the search test (prefix-agnostic), got %v", decide["bootwright_libvirt_destroy_owned"])
	}
	refuse, ok := tasks[refuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("libvirt destroy ownership guard must be an assert, got %v", tasks[refuseIdx])
	}
	if got := fmt.Sprint(refuse["that"]); !strings.Contains(got, "bootwright_libvirt_destroy_owned") {
		t.Fatalf("libvirt destroy guard must require the decided ownership fact, got %v", refuse["that"])
	}
	if got := fmt.Sprint(tasks[refuseIdx]["when"]); !strings.Contains(got, "not (bootwright_destroy_force_unowned") {
		t.Fatalf("libvirt destroy guard must be skipped under --include-unowned, got when=%v", tasks[refuseIdx]["when"])
	}
}

func TestKubeVirtDestroyVerifiesOwnershipLabel(t *testing.T) {
	topTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/destroy.yml")
	probeIdx := findAnsibleTask(t, topTasks, "Probe KubeVirt host cluster reachability")
	recordIdx := findAnsibleTask(t, topTasks, "Resolve KubeVirt machine ownership record presence")
	gateIdx := findAnsibleTask(t, topTasks, "Require the KubeVirt host cluster to be reachable")
	blockIdx := findAnsibleTask(t, topTasks, "Tear down KubeVirt guest on the reachable host cluster")
	if !(probeIdx < recordIdx && recordIdx < gateIdx && gateIdx < blockIdx) {
		t.Fatalf("kubevirt destroy must probe, resolve the ownership record, then gate reachability before the teardown (probe=%d record=%d gate=%d block=%d)", probeIdx, recordIdx, gateIdx, blockIdx)
	}
	recordDecide, ok := topTasks[recordIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("kubevirt destroy record presence must be a set_fact, got %v", topTasks[recordIdx])
	}
	if got := fmt.Sprint(recordDecide["bootwright_kubevirt_machine_recorded"]); !strings.Contains(got, "kubevirt-machine") || !strings.Contains(got, "bootwright_ownership_records_by_kind") {
		t.Fatalf("kubevirt destroy must resolve record presence from the in-scope kubevirt-machine ownership records, got %v", recordDecide["bootwright_kubevirt_machine_recorded"])
	}
	gateWhen := fmt.Sprint(topTasks[gateIdx]["when"])
	if !strings.Contains(gateWhen, "not (bootwright_destroy_skip_unreachable") {
		t.Fatalf("host-reachability gate must fail closed unless --skip-unreachable, got when=%v", topTasks[gateIdx]["when"])
	}
	if !strings.Contains(gateWhen, "bootwright_kubevirt_machine_recorded") {
		t.Fatalf("host-reachability gate must only fail closed for a recorded guest, got when=%v", topTasks[gateIdx]["when"])
	}
	if _, ok := topTasks[gateIdx]["ansible.builtin.assert"]; !ok {
		t.Fatalf("host-reachability gate must be a hard assert, got %v", topTasks[gateIdx])
	}
	if got := fmt.Sprint(topTasks[blockIdx]["when"]); !strings.Contains(got, "bootwright_kubevirt_host_reachable") {
		t.Fatalf("guest teardown must be gated on host reachability, got when=%v", topTasks[blockIdx]["when"])
	}
	tasks := nestedAnsibleTasks(t, topTasks[blockIdx], "block")
	readIdx := findAnsibleTask(t, tasks, "Read KubeVirt VirtualMachine ownership label")
	decideIdx := findAnsibleTask(t, tasks, "Resolve KubeVirt VirtualMachine ownership for destroy")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse to delete a non-Bootwright KubeVirt VirtualMachine")
	deleteIdx := findAnsibleTask(t, tasks, "Delete KubeVirt VirtualMachine")
	if !(readIdx < decideIdx && decideIdx < refuseIdx && refuseIdx < deleteIdx) {
		t.Fatalf("kubevirt destroy must read, decide, and verify the ownership label before deleting (read=%d decide=%d refuse=%d delete=%d)", readIdx, decideIdx, refuseIdx, deleteIdx)
	}
	decide, ok := tasks[decideIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("kubevirt destroy ownership decision must be a set_fact, got %v", tasks[decideIdx])
	}
	owned := fmt.Sprint(decide["bootwright_kubevirt_destroy_owned"])
	if !strings.Contains(owned, "bootwright_kubevirt_vm_owner") || !strings.Contains(owned, "'bootwright'") {
		t.Fatalf("kubevirt destroy ownership decision must require the live owner label to equal bootwright, got %v", decide["bootwright_kubevirt_destroy_owned"])
	}
	refuse, ok := tasks[refuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("kubevirt delete guard must be an assert, got %v", tasks[refuseIdx])
	}
	if got := fmt.Sprint(refuse["that"]); !strings.Contains(got, "bootwright_kubevirt_destroy_owned") {
		t.Fatalf("kubevirt delete guard must require the decided ownership fact, got %v", refuse["that"])
	}
	if !strings.Contains(fmt.Sprint(refuse["fail_msg"]), "managed-by") {
		t.Fatalf("kubevirt delete guard message must name the managed-by ownership label, got %v", refuse["fail_msg"])
	}
	if got := fmt.Sprint(tasks[refuseIdx]["when"]); !strings.Contains(got, "not (bootwright_destroy_force_unowned") {
		t.Fatalf("kubevirt delete guard must be skipped under --include-unowned, got when=%v", tasks[refuseIdx]["when"])
	}

	dvReadIdx := findAnsibleTask(t, tasks, "Read KubeVirt DataVolume ownership labels")
	dvRefuseIdx := findAnsibleTask(t, tasks, "Refuse to delete a non-Bootwright KubeVirt DataVolume")
	dvDeleteIdx := findAnsibleTask(t, tasks, "Delete KubeVirt DataVolumes")
	if !(dvReadIdx < dvRefuseIdx && dvRefuseIdx < dvDeleteIdx) {
		t.Fatalf("kubevirt destroy must read and verify each DataVolume ownership label before deleting (read=%d refuse=%d delete=%d)", dvReadIdx, dvRefuseIdx, dvDeleteIdx)
	}
	dvRefuse, ok := tasks[dvRefuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("kubevirt DataVolume delete guard must be an assert, got %v", tasks[dvRefuseIdx])
	}
	if got := fmt.Sprint(dvRefuse["that"]); !strings.Contains(got, "'bootwright'") || !strings.Contains(got, "stdout") {
		t.Fatalf("kubevirt DataVolume delete guard must require the live managed-by label to equal bootwright, got %v", dvRefuse["that"])
	}
	if !strings.Contains(fmt.Sprint(dvRefuse["fail_msg"]), "managed-by") {
		t.Fatalf("kubevirt DataVolume delete guard message must name the managed-by label, got %v", dvRefuse["fail_msg"])
	}
	if got := fmt.Sprint(tasks[dvRefuseIdx]["when"]); !strings.Contains(got, "not (bootwright_destroy_force_unowned") {
		t.Fatalf("kubevirt DataVolume delete guard must be skipped under --include-unowned, got when=%v", tasks[dvRefuseIdx]["when"])
	}

	bootTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml")
	readCAIdx := findAnsibleTask(t, bootTasks, "Read managed KubeVirt host cluster ingress CA bundle")
	validateCAIdx := findAnsibleTask(t, bootTasks, "Validate managed KubeVirt host cluster ingress CA bundle")
	writeCAIdx := findAnsibleTask(t, bootTasks, "Write managed KubeVirt host cluster ingress CA bundle")
	uploadIdx := findAnsibleTask(t, bootTasks, "Upload KubeVirt agent ISO DataVolume")
	labelIdx := findAnsibleTask(t, bootTasks, "Label KubeVirt agent ISO DataVolume as Bootwright-managed")
	if !(readCAIdx < validateCAIdx && validateCAIdx < writeCAIdx && writeCAIdx < uploadIdx) {
		t.Fatalf("boot role must resolve and write the managed host ingress CA before uploading (read=%d validate=%d write=%d upload=%d)", readCAIdx, validateCAIdx, writeCAIdx, uploadIdx)
	}
	readCA, ok := bootTasks[readCAIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("managed host ingress CA read must be a command, got %v", bootTasks[readCAIdx])
	}
	if !stringListContains(readCA["argv"], "default-ingress-cert") || !stringListContains(readCA["argv"], "openshift-config-managed") {
		t.Fatalf("managed host ingress CA read must use the published OpenShift ingress CA, got %v", readCA["argv"])
	}
	if got := fmt.Sprint(bootTasks[readCAIdx]["when"]); !strings.Contains(got, "bootwright_kubevirt_host_cluster_ref") {
		t.Fatalf("managed host ingress CA read must be limited to hostClusterRef providers, got when=%v", bootTasks[readCAIdx]["when"])
	}
	writeCA, ok := bootTasks[writeCAIdx]["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("managed host ingress CA write must be a copy, got %v", bootTasks[writeCAIdx])
	}
	if got := fmt.Sprint(writeCA["dest"]); !strings.Contains(got, "bootwright_kubevirt_kubeconfig | dirname") {
		t.Fatalf("managed host ingress CA must stay in the task runtime material directory, got dest=%v", writeCA["dest"])
	}
	uploadEnvironment := fmt.Sprint(bootTasks[uploadIdx]["environment"])
	if !strings.Contains(uploadEnvironment, "SSL_CERT_FILE") || !strings.Contains(uploadEnvironment, "bootwright_proxy_env") || !strings.Contains(uploadEnvironment, "bootwright_kubevirt_host_cluster_ref") {
		t.Fatalf("image upload must preserve proxy env and verify managed ingress with SSL_CERT_FILE, got environment=%v", bootTasks[uploadIdx]["environment"])
	}
	if !(uploadIdx < labelIdx) {
		t.Fatalf("boot role must label the agent-ISO DataVolume after uploading it (upload=%d label=%d)", uploadIdx, labelIdx)
	}
	label, ok := bootTasks[labelIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("agent-ISO label task must be a command, got %v", bootTasks[labelIdx])
	}
	if !stringListContains(label["argv"], "bootwright.io/managed-by=bootwright") {
		t.Fatalf("agent-ISO label task must stamp bootwright.io/managed-by=bootwright, got %v", label["argv"])
	}
}

func TestKubeVirtBootGatesLiveResourcesBeforeMutation(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml")
	vmProbeIdx := findAnsibleTask(t, tasks, "Read KubeVirt boot VirtualMachine identity")
	vmRequireIdx := findAnsibleTask(t, tasks, "Require an owned KubeVirt boot VirtualMachine")
	isoProbeIdx := findAnsibleTask(t, tasks, "Read existing KubeVirt agent ISO DataVolume identity")
	legacyCandidateIdx := findAnsibleTask(t, tasks, "Resolve legacy KubeVirt agent ISO DataVolume candidate")
	recordReadIdx := findAnsibleTask(t, tasks, "Read KubeVirt machine ownership record")
	recordVerifyIdx := findAnsibleTask(t, tasks, "Verify KubeVirt machine ownership record")
	isoGateIdx := findAnsibleTask(t, tasks, "Enforce KubeVirt agent ISO DataVolume apply mode")
	legacyUpgradeIdx := findAnsibleTask(t, tasks, "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels")
	stopIdx := findAnsibleTask(t, tasks, "Stop KubeVirt VirtualMachine before replacing the agent ISO")
	deleteIdx := findAnsibleTask(t, tasks, "Remove previous KubeVirt agent ISO DataVolume")
	if !(vmProbeIdx < vmRequireIdx && isoProbeIdx < legacyCandidateIdx &&
		legacyCandidateIdx < recordReadIdx && recordReadIdx < recordVerifyIdx &&
		recordVerifyIdx < isoGateIdx &&
		vmRequireIdx < stopIdx && isoGateIdx < legacyUpgradeIdx &&
		legacyUpgradeIdx < stopIdx && stopIdx < deleteIdx) {
		t.Fatalf("KubeVirt boot must prove VM and ISO identity, conditionally verify the legacy record, upgrade eligible labels, then stop/delete (vmProbe=%d vmRequire=%d isoProbe=%d legacyCandidate=%d recordRead=%d recordVerify=%d isoGate=%d legacyUpgrade=%d stop=%d delete=%d)", vmProbeIdx, vmRequireIdx, isoProbeIdx, legacyCandidateIdx, recordReadIdx, recordVerifyIdx, isoGateIdx, legacyUpgradeIdx, stopIdx, deleteIdx)
	}
	recordRead, ok := tasks[recordReadIdx]["ansible.builtin.slurp"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(recordRead["src"]), "kubevirt_machine_record_path") {
		t.Fatalf("legacy recovery must read the exact kubevirt-machine ownership record path, got %v", tasks[recordReadIdx])
	}
	if got := fmt.Sprint(tasks[recordReadIdx]["when"]); !strings.Contains(got, "bootwright_kubevirt_boot_iso_legacy_candidate") {
		t.Fatalf("KubeVirt machine ownership record read must run only for a legacy candidate, got when=%v", tasks[recordReadIdx]["when"])
	}
	recordVerify, ok := tasks[recordVerifyIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("KubeVirt machine ownership record verification must be a set_fact, got %v", tasks[recordVerifyIdx])
	}
	recorded := fmt.Sprint(recordVerify["bootwright_kubevirt_machine_recorded"])
	for _, want := range []string{"apiVersion", "kind", "name", "owner", "context", "cluster", "machine"} {
		if !strings.Contains(recorded, want) {
			t.Fatalf("KubeVirt machine record verification must require %q, got %v", want, recordVerify["bootwright_kubevirt_machine_recorded"])
		}
	}
	require, ok := tasks[vmRequireIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("KubeVirt boot VM ownership guard must be an assert, got %v", tasks[vmRequireIdx])
	}
	if got := fmt.Sprint(require["that"]); !strings.Contains(got, "bootwright_kubevirt_boot_vm_owned") {
		t.Fatalf("KubeVirt boot VM ownership guard must require exact live ownership, got %v", require["that"])
	}
	include, ok := tasks[isoGateIdx]["ansible.builtin.include_role"].(map[string]any)
	if !ok || fmt.Sprint(include["name"]) != "bootwright.core.ownership_record" || fmt.Sprint(include["tasks_from"]) != "apply_mode_gate.yml" {
		t.Fatalf("KubeVirt agent ISO gate must use the shared apply-mode contract, got %v", tasks[isoGateIdx])
	}
	legacyResolveIdx := findAnsibleTask(t, tasks, "Resolve legacy KubeVirt agent ISO DataVolume ownership")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve KubeVirt agent ISO DataVolume ownership")
	if !(legacyResolveIdx < resolveIdx && resolveIdx < isoGateIdx) {
		t.Fatalf("legacy agent ISO ownership must resolve before the final ownership decision and gate (legacyResolve=%d resolve=%d gate=%d)", legacyResolveIdx, resolveIdx, isoGateIdx)
	}
	legacyCandidate, ok := tasks[legacyCandidateIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("legacy KubeVirt agent ISO candidate resolution must be a set_fact, got %v", tasks[legacyCandidateIdx])
	}
	legacyResolve, ok := tasks[legacyResolveIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("legacy KubeVirt agent ISO ownership resolution must be a set_fact, got %v", tasks[legacyResolveIdx])
	}
	resolve, ok := tasks[resolveIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("KubeVirt agent ISO ownership resolution must be a set_fact, got %v", tasks[resolveIdx])
	}
	legacyCandidateExpr := fmt.Sprint(legacyCandidate["bootwright_kubevirt_boot_iso_legacy_candidate"])
	for _, want := range []string{
		"bootwright_kubevirt_boot_vm_owned",
		"bootwright.io/managed-by",
		"bootwright.io/cluster",
		"bootwright.io/role",
		"bootwright.io/context' not in",
		"bootwright.io/node' not in",
	} {
		if !strings.Contains(legacyCandidateExpr, want) {
			t.Fatalf("legacy agent ISO candidate must require %q, got %v", want, legacyCandidate["bootwright_kubevirt_boot_iso_legacy_candidate"])
		}
	}
	legacyOwned := fmt.Sprint(legacyResolve["bootwright_kubevirt_boot_iso_legacy_owned"])
	for _, want := range []string{"bootwright_kubevirt_boot_iso_legacy_candidate", "bootwright_kubevirt_machine_recorded"} {
		if !strings.Contains(legacyOwned, want) {
			t.Fatalf("legacy agent ISO ownership must require %q, got %v", want, legacyResolve["bootwright_kubevirt_boot_iso_legacy_owned"])
		}
	}
	owned := fmt.Sprint(resolve["bootwright_kubevirt_boot_iso_owned"])
	if !strings.Contains(owned, "bootwright_kubevirt_boot_iso_legacy_owned") {
		t.Fatalf("agent ISO ownership must admit the fully-proven legacy identity, got %v", resolve["bootwright_kubevirt_boot_iso_owned"])
	}
	upgrade := fmt.Sprint(tasks[legacyUpgradeIdx]["ansible.builtin.command"])
	for _, want := range []string{"bootwright.io/context=", "bootwright.io/node=", "--overwrite"} {
		if !strings.Contains(upgrade, want) {
			t.Fatalf("legacy agent ISO upgrade must stamp %q, got %v", want, tasks[legacyUpgradeIdx])
		}
	}
	if got := fmt.Sprint(tasks[legacyUpgradeIdx]["when"]); !strings.Contains(got, "bootwright_kubevirt_boot_iso_legacy_owned") {
		t.Fatalf("legacy agent ISO upgrade must require the proven legacy identity, got when=%v", tasks[legacyUpgradeIdx]["when"])
	}
	labelIdx := findAnsibleTask(t, tasks, "Label KubeVirt agent ISO DataVolume as Bootwright-managed")
	label := fmt.Sprint(tasks[labelIdx]["ansible.builtin.command"])
	for _, want := range []string{"bootwright.io/context=", "bootwright.io/cluster=", "bootwright.io/node=", "bootwright.io/role=agent-iso"} {
		if !strings.Contains(label, want) {
			t.Fatalf("agent ISO must carry exact identity label %q, got %v", want, tasks[labelIdx])
		}
	}
}

func TestBaremetalSubstrateDestroyNoUnconditionalFail(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_baremetal/tasks/destroy.yml")

	for _, task := range tasks {
		if _, ok := task["ansible.builtin.fail"]; ok {
			t.Fatalf("bare-metal substrate destroy must not fail closed, found fail task %q", task["name"])
		}
		if _, ok := task["ansible.builtin.command"]; ok {
			t.Fatalf("bare-metal substrate destroy must not contact the host, found command task %q", task["name"])
		}
		if _, ok := task["ansible.builtin.shell"]; ok {
			t.Fatalf("bare-metal substrate destroy must not contact the host, found shell task %q", task["name"])
		}
	}

	reclaimIdx := findAnsibleTask(t, tasks, "Remove managed OS install artifacts")
	reclaim := tasks[reclaimIdx]
	inc, ok := reclaim["ansible.builtin.include_role"].(map[string]any)
	if !ok || fmt.Sprint(inc["name"]) != "bootwright.core.ownership_record" {
		t.Fatalf("reclaim task must include the ownership_record role, got %v", reclaim["ansible.builtin.include_role"])
	}
	if got := fmt.Sprint(inc["tasks_from"]); got != "destroy_resource.yml" {
		t.Fatalf("reclaim task must run destroy_resource.yml (idempotent, no-op on a never-provisioned node), got %q", got)
	}
	recordVars, ok := reclaim["vars"].(map[string]any)
	if !ok {
		t.Fatalf("reclaim task must set the ownership record vars, got %v", reclaim["vars"])
	}
	record, ok := recordVars["bootwright_ownership_record"].(map[string]any)
	if !ok || fmt.Sprint(record["kind"]) != "managed-os-install" {
		t.Fatalf("reclaim task must target the managed-os-install record, got %v", recordVars["bootwright_ownership_record"])
	}
	if got := fmt.Sprint(reclaim["when"]); !strings.Contains(got, "bootwright_component.osManaged") {
		t.Fatalf("reclaim task must be gated on osManaged, got when=%v", reclaim["when"])
	}

	stateIdx := findAnsibleTask(t, tasks, "Remove cluster baremetal state directory (idempotent)")
	fileTask, ok := tasks[stateIdx]["ansible.builtin.file"].(map[string]any)
	if !ok || fmt.Sprint(fileTask["state"]) != "absent" {
		t.Fatalf("bare-metal substrate destroy must remove the local provider-state file, got %v", tasks[stateIdx])
	}
}

func TestOwnershipDestroyReadsPreRenameVMediaAttrs(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_resource.yml")

	stopIdx := findAnsibleTask(t, tasks, "Stop recorded systemd units")
	unitItem := "{{ bootwright_ownership_destroy_attrs.vMediaUnit | default(bootwright_ownership_destroy_attrs.vmediaUnit) | default('') }}"
	if !stringListContains(tasks[stopIdx]["loop"], unitItem) {
		t.Fatalf("%s must read both vMediaUnit and the pre-rename vmediaUnit, got %v", tasks[stopIdx]["name"], tasks[stopIdx]["loop"])
	}

	closeIdx := findAnsibleTask(t, tasks, "Close recorded firewalld ports")
	if !stringListItemContains(tasks[closeIdx]["loop"], "bootwright_ownership_destroy_attrs.vMediaPort | default(bootwright_ownership_destroy_attrs.vmediaPort)") {
		t.Fatalf("%s must read both vMediaPort and the pre-rename vmediaPort, got %v", tasks[closeIdx]["name"], tasks[closeIdx]["loop"])
	}
}

func TestOwnershipHelperWritesTimezoneQualifiedTimestamps(t *testing.T) {
	for _, path := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/resource.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/package_apply_one.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/package_remove_one.yml",
	} {
		body := readRepoFile(t, path)
		if strings.Contains(body, "now(utc=true).isoformat()") {
			t.Fatalf("%s must not write naive UTC timestamps with isoformat()", path)
		}
		if !strings.Contains(body, "now(utc=true).strftime('%Y-%m-%dT%H:%M:%S.%fZ')") {
			t.Fatalf("%s must write RFC3339 UTC timestamps with a Z suffix", path)
		}
	}
}

func registeredAdapterRoles() map[string]bool {
	out := map[string]bool{}
	add := func(role string) {
		if role != "" {
			out[role] = true
		}
	}
	for _, entry := range roles.Entries() {
		for _, role := range entry.Roles.MachineSetupRoles {
			add(role)
		}
		add(entry.Roles.SubstrateApplyRole)
		add(entry.Roles.SubstrateDestroyRole)
		add(entry.Roles.BMCApplyRole)
		add(entry.Roles.BMCDestroyRole)
		add(entry.Roles.BootApplyRole)
		add(entry.Roles.MediaPrepareRole)
	}
	for _, entry := range roles.ServiceEntries() {
		add(entry.ApplyRole)
		add(entry.DestroyRole)
	}
	return out
}

func ansibleRoleDirExists(t *testing.T, role string) bool {
	t.Helper()
	info, err := os.Stat(filepath.Join(repoRoot(t), filepath.FromSlash(bootwrightCollectionRoleRoot), strings.TrimPrefix(role, "bootwright.core.")))
	return err == nil && info.IsDir()
}

func ansibleAdapterRoleDirs(t *testing.T) []string {
	t.Helper()
	type rule struct {
		base     string
		prefixes []string
	}
	rules := []rule{
		{base: bootwrightCollectionRoleRoot, prefixes: []string{"provider_host_libvirt"}},
		{base: bootwrightCollectionRoleRoot, prefixes: []string{"provider_service_bmc_"}},
		{base: bootwrightCollectionRoleRoot, prefixes: []string{"infra_component_"}},
		{base: bootwrightCollectionRoleRoot, prefixes: []string{"machine_substrate_"}},
		{base: bootwrightCollectionRoleRoot, prefixes: []string{"container_cluster_boot_", "container_cluster_media_"}},
	}
	var out []string
	for _, r := range rules {
		entries, err := os.ReadDir(filepath.Join(repoRoot(t), r.base))
		if err != nil {
			t.Fatalf("read role dir %s: %v", r.base, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			for _, prefix := range r.prefixes {
				if prefix == "" || strings.HasPrefix(entry.Name(), prefix) {
					out = append(out, "bootwright.core."+entry.Name())
					break
				}
			}
		}
	}
	return out
}
