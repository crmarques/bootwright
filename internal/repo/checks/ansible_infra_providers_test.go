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
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/apply_mode.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/install.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/machine_os_identity/tasks/probe_existing.yml",
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
		if len(plays) == 0 {
			t.Fatalf("%s has no plays", path)
		}
		for i, play := range plays {
			ignoreUnreachable, ok := play["ignore_unreachable"].(string)
			if !ok || !strings.Contains(ignoreUnreachable, "bootwright_destroy_skip_unreachable") {
				t.Fatalf("%s play %d must template ignore_unreachable from bootwright_destroy_skip_unreachable so --authorize unreachable-nodes reaches the infra stage, got %v", path, i, play["ignore_unreachable"])
			}
		}
	}
}

func TestMachineInfraApplyPlayGroupsTaskBannersAcrossMachines(t *testing.T) {
	plays := readAnsiblePlays(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_machine_infra_apply.yml")
	if len(plays) != 1 {
		t.Fatalf("machine infra apply play count = %d, want 1", len(plays))
	}
	if got := plays[0]["hosts"]; got != "bootwright_machine_task_hosts" {
		t.Fatalf("machine infra apply hosts = %v, want the machine pseudo-host group", got)
	}
	if got := plays[0]["strategy"]; got != "linear" {
		t.Fatalf("machine infra apply must match its destroy counterpart so one TASK banner covers every machine in the play, got strategy=%v", got)
	}
	if _, ok := plays[0]["any_errors_fatal"]; ok {
		t.Fatal("one machine failing must not abort its peers' provisions")
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
	for _, banned := range []string{"bootwright_infra_component_owned_stat", "Stat ownership record", "bootwright_ownership_dir"} {
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

func TestInfraComponentReferenceDestroyOnlyReleasesReference(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/playbooks/task_infra_component_services_destroy.yml"
	plays := readAnsiblePlays(t, path)
	if len(plays) != 1 {
		t.Fatalf("%s plays = %d, want 1", path, len(plays))
	}
	tasks := nestedAnsibleTasks(t, plays[0], "tasks")
	serviceDestroy := tasks[findAnsibleTask(t, tasks, "Destroy infra component service roles")]
	if when := fmt.Sprint(serviceDestroy["when"]); !strings.Contains(when, "bootwright_host_infra_component_reference_names") || !strings.Contains(when, "not in") {
		t.Fatalf("reference-owned service must be excluded from destructive component roles: %v", serviceDestroy["when"])
	}
	release := tasks[findAnsibleTask(t, tasks, "Release recorded infra component references")]
	if role := fmt.Sprint(release["ansible.builtin.include_role"]); !strings.Contains(role, "remove_resource.yml") {
		t.Fatalf("reference release must remove only its role-specific record: %v", release)
	}
	if vars := fmt.Sprint(release["vars"]); !strings.Contains(vars, "reference") {
		t.Fatalf("reference release must preserve the reference filename convention: %v", release["vars"])
	}
	destroy := tasks[findAnsibleTask(t, tasks, "Destroy recorded infra component resources")]
	if when := fmt.Sprint(destroy["when"]); !strings.Contains(when, "!= 'reference'") {
		t.Fatalf("reference records must never enter destructive recorded-resource cleanup: %v", destroy["when"])
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
	stopResetRefuseIdx := findAnsibleTask(t, tasks, "Refuse managed OS disk reset when the libvirt domain could not be stopped")
	undefineResetIdx := findAnsibleTask(t, tasks, "Undefine managed OS libvirt domain for override reinstall")
	undefineResetRefuseIdx := findAnsibleTask(t, tasks, "Refuse managed OS disk reset when the libvirt domain could not be undefined")
	verifyResetIdx := findAnsibleTask(t, tasks, "Verify the reset libvirt domain is absent before disk deletion")
	requireAbsentIdx := findAnsibleTask(t, tasks, "Require proven reset libvirt domain absence before disk deletion")
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

	if !(probeIdx < resolveIdx && resolveIdx < stopResetIdx && stopResetIdx < stopResetRefuseIdx && stopResetRefuseIdx < undefineResetIdx && undefineResetIdx < undefineResetRefuseIdx && undefineResetRefuseIdx < verifyResetIdx && verifyResetIdx < requireAbsentIdx && requireAbsentIdx < removeResetIdx && removeResetIdx < recreateResetIdx && recreateResetIdx < selectIdx && selectIdx < assertIdx && assertIdx < migrateDiskIdx && migrateDiskIdx < createDiskIdx && createDataDiskIdx < ownDiskIdx && ownDiskIdx < renderDomainIdx) {
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
	for _, tc := range []struct {
		mutate int
		refuse int
		result string
	}{
		{stopResetIdx, stopResetRefuseIdx, "bootwright_libvirt_managed_os_reset_stop"},
		{undefineResetIdx, undefineResetRefuseIdx, "bootwright_libvirt_managed_os_reset_undefine"},
	} {
		if tasks[tc.mutate]["failed_when"] != false {
			t.Fatalf("%s must defer the actionable error to its following assertion, got failed_when=%v", tasks[tc.mutate]["name"], tasks[tc.mutate]["failed_when"])
		}
		assertion, ok := tasks[tc.refuse]["ansible.builtin.assert"].(map[string]any)
		if !ok || !strings.Contains(fmt.Sprint(assertion["that"]), tc.result+".rc") {
			t.Fatalf("%s must hard-fail on the remover result before disk deletion, got %v", tasks[tc.refuse]["name"], tasks[tc.refuse])
		}
		message := fmt.Sprint(assertion["fail_msg"])
		if !strings.Contains(message, "bootwright_apply_rebuild_invocation") {
			t.Fatalf("%s diagnostic must name the exact rebuild invocation: %v", tasks[tc.refuse]["name"], assertion["fail_msg"])
		}
		if strings.Contains(message, "same bootwright apply invocation") || strings.Contains(message, "bootwright_apply_rebuild_invocation | default") {
			t.Fatalf("%s diagnostic must not fall back to a context-free rebuild suggestion: %v", tasks[tc.refuse]["name"], assertion["fail_msg"])
		}
	}
	absent, ok := tasks[requireAbsentIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(absent["that"]), "bootwright_libvirt_managed_os_reset_absence.rc") {
		t.Fatalf("libvirt rebuild must positively prove domain absence before removing disks, got %v", tasks[requireAbsentIdx])
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

func TestProviderBackedMachineApplyGatesLiveExistenceInCreateMode(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		probe      string
		conclusive string
		resolve    string
		foreign    string
		gate       string
		mutation   string
		existsFact string
		ownedFact  string
	}{
		{
			name:       "libvirt",
			path:       "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml",
			probe:      "Read libvirt domain ownership metadata for apply",
			conclusive: "Require a conclusive libvirt domain probe",
			resolve:    "Resolve libvirt domain ownership for apply",
			foreign:    "Refuse to mutate a non-Bootwright libvirt domain on apply",
			gate:       "Enforce libvirt domain apply mode against live state",
			mutation:   "Create per-machine libvirt state directories",
			existsFact: "bootwright_libvirt_apply_domain_xml.rc",
			ownedFact:  "bootwright_libvirt_apply_owned",
		},
		{
			name:       "vsphere",
			path:       "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/probe.yml",
			probe:      "Probe vSphere virtual machine",
			conclusive: "Require a conclusive vSphere VM probe",
			resolve:    "Resolve vSphere VM ownership for apply",
			foreign:    "Refuse to mutate a non-Bootwright vSphere VM",
			gate:       "Enforce vSphere VM apply mode against live state",
			mutation:   "Stop managed OS vSphere VM for override reinstall",
			existsFact: "bootwright_vsphere_probe.instance is defined",
			ownedFact:  "bootwright_vsphere_apply_owned",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tasks := readAnsibleTasks(t, tc.path)
			probeIdx := findAnsibleTask(t, tasks, tc.probe)
			conclusiveIdx := findAnsibleTask(t, tasks, tc.conclusive)
			resolveIdx := findAnsibleTask(t, tasks, tc.resolve)
			foreignIdx := findAnsibleTask(t, tasks, tc.foreign)
			gateIdx := findAnsibleTask(t, tasks, tc.gate)
			mutationIdx := findAnsibleTask(t, tasks, tc.mutation)
			if !(probeIdx < conclusiveIdx && conclusiveIdx < resolveIdx && resolveIdx < foreignIdx && foreignIdx < gateIdx && gateIdx < mutationIdx) {
				t.Fatalf("live probe and create gate must precede provider mutation: probe=%d conclusive=%d resolve=%d foreign=%d gate=%d mutation=%d", probeIdx, conclusiveIdx, resolveIdx, foreignIdx, gateIdx, mutationIdx)
			}
			conclusiveMessage := ansibleFailureMessage(t, tasks[conclusiveIdx])
			if !strings.Contains(conclusiveMessage, "bootwright_mutating_invocation") || strings.Contains(conclusiveMessage, "re-run apply") {
				t.Fatalf("inconclusive provider probe must name the exact current invocation, got %s", conclusiveMessage)
			}
			foreignWhen := fmt.Sprint(tasks[foreignIdx]["when"])
			if !strings.Contains(foreignWhen, "bootwright_apply_mode") || !strings.Contains(foreignWhen, "!= 'create'") {
				t.Fatalf("provider-specific foreign refusal must yield create mode to the live greenfield gate, got when=%v", tasks[foreignIdx]["when"])
			}
			foreignMessage := ansibleFailureMessage(t, tasks[foreignIdx])
			for _, want := range []string{"outside Bootwright", "No Bootwright retry command", "authorization token"} {
				if !strings.Contains(foreignMessage, want) {
					t.Fatalf("foreign provider refusal missing command-free remedy %q: %s", want, foreignMessage)
				}
			}
			if strings.Contains(foreignMessage, "_invocation") {
				t.Fatalf("foreign provider refusal must not render a Bootwright retry command: %s", foreignMessage)
			}
			include, ok := tasks[gateIdx]["ansible.builtin.include_role"].(map[string]any)
			if !ok || include["name"] != "bootwright.core.ownership_record" || include["tasks_from"] != "apply_mode_gate.yml" {
				t.Fatalf("live create gate must use the shared apply-mode gate, got %v", tasks[gateIdx])
			}
			vars, ok := tasks[gateIdx]["vars"].(map[string]any)
			if !ok || !strings.Contains(fmt.Sprint(vars["bootwright_gate_exists"]), tc.existsFact) || !strings.Contains(fmt.Sprint(vars["bootwright_gate_owned"]), tc.ownedFact) {
				t.Fatalf("live create gate must consume provider presence and ownership facts, got vars=%v", tasks[gateIdx]["vars"])
			}
		})
	}
}

func TestSharedApplyModeGateDoesNotRecommendRebuildForForeignCreateState(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/apply_mode_gate.yml")
	owned := tasks[findAnsibleTask(t, tasks, "Refuse greenfield apply over existing owned {{ bootwright_gate_object }}")]
	ownedMessage := ansibleFailureMessage(t, owned)
	if !strings.Contains(ownedMessage, "bootwright_apply_reconcile_invocation") {
		t.Fatalf("owned create-mode refusal must render the exact reconcile invocation: %s", ownedMessage)
	}
	for _, forbidden := range []string{"bootwright_apply_rebuild_invocation", "bootwright_mutating_invocation", "| default"} {
		if strings.Contains(ownedMessage, forbidden) {
			t.Fatalf("owned create-mode refusal must offer only the exact reconcile invocation, found %q in %s", forbidden, ownedMessage)
		}
	}
	ownedWhen := fmt.Sprint(owned["when"])
	if !strings.Contains(ownedWhen, "bootwright_gate_owned") || strings.Contains(ownedWhen, "not (bootwright_gate_owned") {
		t.Fatalf("owned create-mode refusal must require positive ownership, got when=%v", owned["when"])
	}

	for _, name := range []string{
		"Refuse greenfield apply over existing foreign {{ bootwright_gate_object }}",
		"Refuse to modify foreign {{ bootwright_gate_object }}",
	} {
		task := tasks[findAnsibleTask(t, tasks, name)]
		message := ansibleFailureMessage(t, task)
		for _, want := range []string{"outside Bootwright", "No Bootwright retry command", "authorization token"} {
			if !strings.Contains(message, want) {
				t.Fatalf("%s missing command-free foreign remedy %q: %s", name, want, message)
			}
		}
		if strings.Contains(message, "_invocation") {
			t.Fatalf("%s must not render a Bootwright retry command: %s", name, message)
		}
	}
}

func TestProviderStructuralRefusalsUseExactRebuildInvocation(t *testing.T) {
	cases := []struct {
		path string
		task string
	}{
		{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", task: "Refuse managed OS disk reset when the libvirt domain could not be stopped"},
		{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", task: "Refuse managed OS disk reset when the libvirt domain could not be undefined"},
		{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", task: "Require proven reset libvirt domain absence before disk deletion"},
		{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", task: "Refuse undeclared machine data disks"},
		{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", task: "Refuse an in-place libvirt root disk resize"},
		{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/gate.yml", task: "Refuse an in-place vSphere root disk resize"},
		{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/gate.yml", task: "Refuse undeclared vSphere machine data disks"},
	}
	for _, tc := range cases {
		t.Run(tc.task, func(t *testing.T) {
			tasks := readAnsibleTasks(t, tc.path)
			message := ansibleFailureMessage(t, tasks[findAnsibleTask(t, tasks, tc.task)])
			if !strings.Contains(message, "bootwright_apply_rebuild_invocation") {
				t.Fatalf("structural refusal must render the exact selected rebuild invocation: %s", message)
			}
			for _, forbidden := range []string{"bootwright_mutating_invocation", "bootwright_apply_reconcile_invocation", "bootwright_apply_rebuild_invocation | default", "same bootwright apply invocation"} {
				if strings.Contains(message, forbidden) {
					t.Fatalf("structural refusal must use only the resolved rebuild invocation, found %q in %s", forbidden, message)
				}
			}
		})
	}
}

func ansibleFailureMessage(t *testing.T, task map[string]any) string {
	t.Helper()
	for _, module := range []string{"ansible.builtin.assert", "ansible.builtin.fail"} {
		body, ok := task[module].(map[string]any)
		if !ok {
			continue
		}
		for _, field := range []string{"fail_msg", "msg"} {
			if message, ok := body[field]; ok {
				return fmt.Sprint(message)
			}
		}
	}
	t.Fatalf("task %v has no Ansible failure message", task["name"])
	return ""
}

func TestInfraComponentContainersCarryAndEnforceContextIdentity(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/infra_component_container_gate.yml"
	gate := readRepoFile(t, path)
	for _, want := range []string{
		"Refuse unreadable container ownership probes",
		"Refuse unreadable service context claim",
		"Refuse unclaimed existing service state",
		"Refuse unclaimed existing container",
		"podman\n      - container\n      - inspect",
		"bootwright.context",
		"Refuse cross-context overwrite",
		"Refuse cross-context service claim",
		"bootwright_mutating_invocation",
		"no apply authorization adopts another context's service",
	} {
		if !strings.Contains(gate, want) {
			t.Fatalf("infra-component live gate missing %q", want)
		}
	}
	tasks := readAnsibleTasks(t, path)
	claimProbe := findAnsibleTaskByPrefix(t, tasks, "Probe service context marker")
	liveRefusal := findAnsibleTaskByPrefix(t, tasks, "Refuse cross-context overwrite")
	claimRefusal := findAnsibleTaskByPrefix(t, tasks, "Refuse cross-context service claim")
	modeGate := findAnsibleTaskByPrefix(t, tasks, "Enforce apply mode")
	claimDir := findAnsibleTaskByPrefix(t, tasks, "Create service claim directory")
	claim := findAnsibleTaskByPrefix(t, tasks, "Claim service for this context before mutation")
	if !(claimProbe < liveRefusal && liveRefusal < claimRefusal && claimRefusal < modeGate && modeGate < claimDir && claimDir < claim) {
		t.Fatalf("read-only context and apply-mode gates must precede the durable claim: probe=%d live=%d claim-refusal=%d mode=%d dir=%d claim=%d", claimProbe, liveRefusal, claimRefusal, modeGate, claimDir, claim)
	}
	roles := []string{
		"infra_component_artifact_server_http",
		"infra_component_load_balancer_haproxy",
		"infra_component_name_resolution_dnsmasq",
		"infra_component_proxy_squid",
		"infra_component_registry_mirror",
	}
	for _, role := range roles {
		body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/"+role+"/tasks/main.yml")
		if !strings.Contains(body, "bootwright.context: \"{{ bootwright_clusters_dir | dirname | basename }}\"") {
			t.Fatalf("%s must stamp the context identity before a partial apply can release the global lease", role)
		}
	}
}

func TestNTPContextClaimPrecedesEveryServiceMutation(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_ntp_chrony/tasks/main.yml"
	tasks := readAnsibleTasks(t, path)
	rootProbe := findAnsibleTask(t, tasks, "Probe NTP service claim directory")
	probe := findAnsibleTask(t, tasks, "Probe NTP service context marker")
	unreadable := findAnsibleTask(t, tasks, "Refuse unreadable NTP service claim")
	unknown := findAnsibleTask(t, tasks, "Refuse unclaimed existing NTP service state")
	refusal := findAnsibleTask(t, tasks, "Refuse cross-context NTP overwrite")
	modeGate := findAnsibleTask(t, tasks, "Enforce apply mode for this NTP service")
	claimDir := findAnsibleTask(t, tasks, "Create NTP ownership-claim directory")
	claim := findAnsibleTask(t, tasks, "Claim NTP service for this context before mutation")
	packages := findAnsibleTask(t, tasks, "Install chrony packages")
	if !(rootProbe < probe && probe < unreadable && unreadable < unknown && unknown < refusal && refusal < modeGate && modeGate < claimDir && claimDir < claim && claim < packages) {
		t.Fatalf("NTP read and mode gates plus durable context claim must precede package/config/service mutation: root=%d marker=%d unreadable=%d unknown=%d refusal=%d mode=%d dir=%d claim=%d package=%d", rootProbe, probe, unreadable, unknown, refusal, modeGate, claimDir, claim, packages)
	}
	if body := readRepoFile(t, path); !strings.Contains(body, "bootwright_mutating_invocation") || !strings.Contains(body, "no apply authorization adopts another context's service") {
		t.Fatalf("NTP context refusal must carry the exact controller-built retry and no bypass")
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
	if len(plays) != 3 {
		t.Fatalf("task_machine_infra_destroy plays = %d, want 3", len(plays))
	}
	if got := plays[0]["hosts"]; got != "bootwright_provider_hosts:bootwright_infra_hosts" {
		t.Fatalf("machine infra destroy preparation hosts = %v", got)
	}
	if got := plays[0]["strategy"]; got != "linear" {
		t.Fatalf("machine infra destroy preparation must keep child-before-host cluster passes in lockstep, got strategy=%v", got)
	}
	prepareTasks := nestedAnsibleTasks(t, plays[0], "tasks")
	ordered := prepareTasks[findAnsibleTask(t, prepareTasks, "Prepare substrate destroy in dependency order")]
	if got := fmt.Sprint(ordered["loop"]); !strings.Contains(got, "bootwright_destroy_cluster_order") {
		t.Fatalf("machine infra destroy preparation must consume the planner's dependency order, got loop=%v", ordered["loop"])
	}
	if got := fmt.Sprint(ordered["when"]); !strings.Contains(got, "bootwright_destroy_cluster_scope") {
		t.Fatalf("ordered machine infra destroy preparation must remain selected-root scoped, got when=%v", ordered["when"])
	}
	if got := plays[1]["hosts"]; got != "bootwright_machine_task_hosts" {
		t.Fatalf("parallel machine infra destroy hosts = %v", got)
	}
	if got := plays[1]["strategy"]; got != "linear" {
		t.Fatalf("parallel machine infra destroy must keep cluster dependency passes in lockstep, got strategy=%v", got)
	}
	if got := plays[1]["gather_facts"]; got != false {
		t.Fatalf("synthetic machine-task hosts all resolve to one provider machine and no substrate destroy task reads ansible_facts, so the play must not gather, got gather_facts=%v", got)
	}
	if got, ok := plays[1]["gather_subset"]; ok {
		t.Fatalf("machine substrate destroy play must not carry a dead gather_subset, got %v", got)
	}
	machineTasks := nestedAnsibleTasks(t, plays[1], "tasks")
	machineDestroy := machineTasks[findAnsibleTask(t, machineTasks, "Destroy machine substrate in dependency order")]
	if got := fmt.Sprint(machineDestroy["loop"]); !strings.Contains(got, "bootwright_destroy_cluster_levels") {
		t.Fatalf("parallel machine infra destroy must consume the planner's dependency levels, got loop=%v", machineDestroy["loop"])
	}
	machineWhen := fmt.Sprint(machineDestroy["when"])
	for _, want := range []string{
		"bootwright_machine_task_cluster_name",
		"bootwright_destroy_cluster_scope",
		"bootwright_destroy_machine_scope",
	} {
		if !strings.Contains(machineWhen, want) {
			t.Fatalf("parallel machine infra destroy when missing %q: %v", want, machineDestroy["when"])
		}
	}
	machineBody := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/tasks/machine_infra/destroy_machine.yml")
	if strings.Contains(machineBody, "loop: \"{{ bootwright_current_machines }}\"") {
		t.Fatal("machine infra destroy must dispatch one selected machine per synthetic host instead of serially looping over a provider host's machines")
	}
	if !strings.Contains(machineBody, "bootwright_component.substrateDestroyRole") {
		t.Fatal("machine infra destroy must dispatch the selected machine substrate role")
	}
	if got := plays[2]["hosts"]; got != "bootwright_provider_hosts:bootwright_infra_hosts" {
		t.Fatalf("machine infra destroy cleanup hosts = %v", got)
	}
	tasks := nestedAnsibleTasks(t, plays[2], "tasks")
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
	blockProbeIdx := findAnsibleTask(t, tasks, "Inspect libvirt domain block devices")
	metadataProbeIdx := findAnsibleTask(t, tasks, "Inspect libvirt domain ownership metadata")
	blockGateIdx := findAnsibleTask(t, tasks, "Require conclusive libvirt block-device probes before context sweep")
	metadataGateIdx := findAnsibleTask(t, tasks, "Require conclusive libvirt ownership probes before context sweep")
	consistencyGateIdx := findAnsibleTask(t, tasks, "Require consistent libvirt probe results before context sweep")
	selectOwnedIdx := findAnsibleTask(t, tasks, "Select all Bootwright-owned current-context libvirt domains")
	stopIdx := findAnsibleTask(t, tasks, "Stop current-context libvirt domains")
	undefineIdx := findAnsibleTask(t, tasks, "Undefine current-context libvirt domains")
	verifyIdx := findAnsibleTask(t, tasks, "Verify swept current-context libvirt domains are absent")
	absentGateIdx := findAnsibleTask(t, tasks, "Require swept libvirt domain absence before context storage deletion")

	blanket := tasks[findAnsibleTask(t, tasks, "Remove current-context libvirt storage directory")]
	blanketIdx := findAnsibleTask(t, tasks, "Remove current-context libvirt storage directory")
	if !(blockProbeIdx < metadataProbeIdx && metadataProbeIdx < blockGateIdx && blockGateIdx < metadataGateIdx && metadataGateIdx < consistencyGateIdx && consistencyGateIdx < selectOwnedIdx && selectOwnedIdx < stopIdx && stopIdx < undefineIdx && undefineIdx < verifyIdx && verifyIdx < absentGateIdx && absentGateIdx < blanketIdx) {
		t.Fatalf("libvirt context sweep must prove every probe conclusive and every owned domain absent before broad storage deletion")
	}
	for _, idx := range []int{blockGateIdx, metadataGateIdx} {
		gate, ok := tasks[idx]["ansible.builtin.assert"].(map[string]any)
		if !ok {
			t.Fatalf("%s must be a hard assertion, got %v", tasks[idx]["name"], tasks[idx])
		}
		for _, want := range []string{"rc", "Domain not found", "failed to get domain", "no domain with matching name"} {
			if !strings.Contains(fmt.Sprint(gate["that"]), want) {
				t.Fatalf("%s must accept only success or a proven-absent domain; missing %q in %v", tasks[idx]["name"], want, gate["that"])
			}
		}
	}
	consistencyGate, ok := tasks[consistencyGateIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("context sweep probe consistency gate must be a hard assertion, got %v", tasks[consistencyGateIdx])
	}
	for _, want := range []string{"bootwright_libvirt_domain_blocklists.results", "bootwright_libvirt_domain_name", "item.rc"} {
		if !strings.Contains(fmt.Sprint(consistencyGate["that"]), want) {
			t.Fatalf("context sweep consistency gate must compare both observations by domain; missing %q in %v", want, consistencyGate["that"])
		}
	}
	if tasks[stopIdx]["failed_when"] == false {
		t.Fatalf("context sweep must not ignore a domain-stop failure before undefine and disk deletion: %v", tasks[stopIdx])
	}
	selected, ok := tasks[selectOwnedIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok || fmt.Sprint(selected["bootwright_libvirt_context_domains"]) != "{{ bootwright_libvirt_context_owned_domains }}" {
		t.Fatalf("context sweep must remove every marker-owned domain, including one whose disk moved outside the standard root, got %v", tasks[selectOwnedIdx])
	}
	if got := fmt.Sprint(tasks[stopIdx]["loop"]); !strings.Contains(got, "bootwright_libvirt_context_domains") {
		t.Fatalf("context sweep must stop the complete marker-owned domain set, got loop=%v", tasks[stopIdx]["loop"])
	}
	absentGate, ok := tasks[absentGateIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(absentGate["that"]), "item.rc") || !strings.Contains(fmt.Sprint(absentGate["that"]), "Domain not found") {
		t.Fatalf("context sweep must positively prove each removed domain absent, got %v", tasks[absentGateIdx])
	}
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

func TestBMCProviderDestroyRetainsEvidenceOnRuntimeFailure(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy.yml")
	runtimeIdx := findAnsibleTask(t, tasks, "Tear down BMC runtime before deleting state and ownership")
	stateIdx := findAnsibleTask(t, tasks, "Remove BMC state directory")
	recordIdx := findAnsibleTask(t, tasks, "Remove BMC emulator ownership record")
	if !(runtimeIdx < stateIdx && stateIdx < recordIdx) {
		t.Fatalf("BMC runtime must finish before state and ownership deletion (runtime=%d state=%d record=%d)", runtimeIdx, stateIdx, recordIdx)
	}

	runtime := nestedAnsibleTasks(t, tasks[runtimeIdx], "block")
	for _, name := range []string{
		"Stop sushy-emulator service",
		"Stop vmedia HTTP service",
		"Remove sushy + vmedia systemd units",
		"Reload systemd after removing BMC units",
		"Destroy libvirt vmedia pool",
	} {
		idx := findAnsibleTask(t, runtime, name)
		if runtime[idx]["failed_when"] == false {
			t.Fatalf("%s must raise a genuine failure into the rescue before BMC evidence deletion", name)
		}
	}

	rescue := nestedAnsibleTasks(t, tasks[runtimeIdx], "rescue")
	refuse := rescue[findAnsibleTask(t, rescue, "Refuse BMC state and ownership deletion after runtime teardown failure")]
	fail, ok := refuse["ansible.builtin.fail"].(map[string]any)
	if !ok {
		t.Fatalf("BMC teardown rescue must hard-fail, got %v", refuse)
	}
	for _, want := range []string{"ansible_failed_task.name", "ansible_failed_result.msg", "bootwright_mutating_invocation", "same bootwright destroy invocation"} {
		if !strings.Contains(fmt.Sprint(fail["msg"]), want) {
			t.Fatalf("BMC teardown rescue missing actionable fragment %q: %v", want, fail["msg"])
		}
	}
}

func TestVSphereVMediaDestroyRetainsEvidenceOnDatastoreFailure(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/destroy.yml")
	deleteIdx := findAnsibleTask(t, tasks, "Delete recorded vSphere virtual media from the datastore")
	evidenceIdx := findAnsibleTask(t, tasks, "Remove recorded vSphere virtual media staging paths and records")
	if deleteIdx >= evidenceIdx {
		t.Fatalf("vSphere datastore media deletion must finish before local staging and ownership deletion (delete=%d evidence=%d)", deleteIdx, evidenceIdx)
	}
	if got := fmt.Sprint(tasks[deleteIdx]["ansible.builtin.include_tasks"]); got != "destroy_vmedia.yml" {
		t.Fatalf("vSphere media deletion must dispatch its fail-closed remover, got %v", tasks[deleteIdx])
	}

	removerTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/destroy_vmedia.yml")
	guardIdx := findAnsibleTask(t, removerTasks, "Delete recorded vSphere virtual media before releasing ownership")
	removeBlock := nestedAnsibleTasks(t, removerTasks[guardIdx], "block")
	remove := removeBlock[findAnsibleTask(t, removeBlock, "Delete recorded vSphere virtual media file")]
	if remove["failed_when"] == false {
		t.Fatalf("vSphere media deletion must raise a genuine failure into its rescue, got %v", remove)
	}
	rescue := nestedAnsibleTasks(t, removerTasks[guardIdx], "rescue")
	refuse := rescue[findAnsibleTask(t, rescue, "Refuse vSphere virtual media evidence deletion after datastore failure")]
	fail, ok := refuse["ansible.builtin.fail"].(map[string]any)
	if !ok {
		t.Fatalf("vSphere media teardown rescue must hard-fail, got %v", refuse)
	}
	for _, want := range []string{"bootwright_mutating_invocation", "same bootwright destroy invocation", "Refusing to delete local staging state or ownership"} {
		if !strings.Contains(fmt.Sprint(fail["msg"]), want) {
			t.Fatalf("vSphere media teardown rescue missing actionable fragment %q: %v", want, fail["msg"])
		}
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
	if got := fmt.Sprint(require["fail_msg"]); !strings.Contains(got, "storageClassRef") || !strings.Contains(got, "container-cluster oc") {
		t.Fatalf("storage class requirement must name the bad field and discovery command, got %v", require["fail_msg"])
	}
}

func TestLibvirtMachineDestroyVerifiesOwnershipMarker(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/destroy.yml")
	readIdx := findAnsibleTask(t, tasks, "Read libvirt domain ownership metadata")
	decideIdx := findAnsibleTask(t, tasks, "Resolve libvirt domain ownership for destroy")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse to destroy a non-Bootwright libvirt domain")
	stopIdx := findAnsibleTask(t, tasks, "Stop libvirt domain")
	stopRefuseIdx := findAnsibleTask(t, tasks, "Refuse disk deletion when the libvirt domain could not be stopped")
	undefineIdx := findAnsibleTask(t, tasks, "Undefine libvirt domain")
	undefineRefuseIdx := findAnsibleTask(t, tasks, "Refuse disk deletion when the libvirt domain could not be undefined")
	verifyIdx := findAnsibleTask(t, tasks, "Verify the libvirt domain is absent before disk deletion")
	absentIdx := findAnsibleTask(t, tasks, "Require proven libvirt domain absence before disk deletion")
	removeStateIdx := findAnsibleTask(t, tasks, "Remove machine state directory")
	removeStorageIdx := findAnsibleTask(t, tasks, "Remove machine storage directory")
	removeRecordIdx := findAnsibleTask(t, tasks, "Remove libvirt domain ownership record")
	if !(readIdx < decideIdx && decideIdx < refuseIdx && refuseIdx < stopIdx && stopIdx < stopRefuseIdx && stopRefuseIdx < undefineIdx && undefineIdx < undefineRefuseIdx && undefineRefuseIdx < verifyIdx && verifyIdx < absentIdx && absentIdx < removeStateIdx && removeStateIdx < removeStorageIdx && removeStorageIdx < removeRecordIdx) {
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
	if got := fmt.Sprint(tasks[refuseIdx]["when"]); !strings.Contains(got, "not (bootwright_destroy_authorize_unowned_vms") {
		t.Fatalf("libvirt destroy guard must be skipped under --authorize unowned-vms, got when=%v", tasks[refuseIdx]["when"])
	}
	for _, tc := range []struct {
		mutate int
		refuse int
		result string
	}{
		{stopIdx, stopRefuseIdx, "bootwright_libvirt_destroy_domain_stop"},
		{undefineIdx, undefineRefuseIdx, "bootwright_libvirt_destroy_domain_undefine"},
	} {
		if tasks[tc.mutate]["failed_when"] != false {
			t.Fatalf("%s must defer the actionable failure to its assertion, got failed_when=%v", tasks[tc.mutate]["name"], tasks[tc.mutate]["failed_when"])
		}
		gate, ok := tasks[tc.refuse]["ansible.builtin.assert"].(map[string]any)
		if !ok || !strings.Contains(fmt.Sprint(gate["that"]), tc.result+".rc") {
			t.Fatalf("%s must consume the remover result before disk deletion, got %v", tasks[tc.refuse]["name"], tasks[tc.refuse])
		}
		for _, want := range []string{"bootwright_mutating_invocation", "same bootwright destroy invocation"} {
			if !strings.Contains(fmt.Sprint(gate["fail_msg"]), want) {
				t.Fatalf("%s diagnostic missing scoped retry fragment %q: %v", tasks[tc.refuse]["name"], want, gate["fail_msg"])
			}
		}
	}
	absent, ok := tasks[absentIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(absent["that"]), "bootwright_libvirt_destroy_domain_absence.rc") {
		t.Fatalf("libvirt destroy must positively prove domain absence before removing storage or ownership, got %v", tasks[absentIdx])
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
	if strings.Contains(gateWhen, "bootwright_destroy_skip_unreachable") {
		t.Fatalf("host-reachability gate must not let --authorize unreachable-nodes discard a recorded guest, got when=%v", topTasks[gateIdx]["when"])
	}
	if !strings.Contains(gateWhen, "bootwright_kubevirt_machine_recorded") {
		t.Fatalf("host-reachability gate must only fail closed for a recorded guest, got when=%v", topTasks[gateIdx]["when"])
	}
	if _, ok := topTasks[gateIdx]["ansible.builtin.assert"]; !ok {
		t.Fatalf("host-reachability gate must be a hard assert, got %v", topTasks[gateIdx])
	}
	if got := fmt.Sprint(topTasks[gateIdx]["ansible.builtin.assert"]); !strings.Contains(got, "--authorize unreachable-nodes cannot prove the guest absent") {
		t.Fatalf("host-reachability refusal must explain why skipping cannot release guest ownership, got %v", topTasks[gateIdx])
	}
	kubeconfigGateIdx := findAnsibleTask(t, topTasks, "Require a captured kubeconfig for the recorded KubeVirt guest")
	if !(recordIdx < kubeconfigGateIdx && kubeconfigGateIdx < gateIdx) {
		t.Fatalf("the captured-kubeconfig gate must sit between the ownership record and the reachability gate (record=%d kubeconfig=%d gate=%d)", recordIdx, kubeconfigGateIdx, gateIdx)
	}
	kubeconfigGate, ok := topTasks[kubeconfigGateIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the captured-kubeconfig gate must be a hard assert, got %v", topTasks[kubeconfigGateIdx])
	}
	if got := fmt.Sprint(kubeconfigGate["that"]); !strings.Contains(got, "bootwright_kubevirt_host_kubeconfig_available") {
		t.Fatalf("the captured-kubeconfig gate must require the resolved availability fact, got %v", kubeconfigGate["that"])
	}
	kubeconfigGateWhen := fmt.Sprint(topTasks[kubeconfigGateIdx]["when"])
	if !strings.Contains(kubeconfigGateWhen, "bootwright_kubevirt_machine_recorded") {
		t.Fatalf("a host cluster with no captured kubeconfig must only fail closed for a recorded guest, got when=%v", topTasks[kubeconfigGateIdx]["when"])
	}
	if strings.Contains(kubeconfigGateWhen, "bootwright_destroy_skip_unreachable") {
		t.Fatalf("the captured-kubeconfig gate must not let --authorize unreachable-nodes discard a recorded guest, got when=%v", topTasks[kubeconfigGateIdx]["when"])
	}
	if got := fmt.Sprint(topTasks[gateIdx]["when"]); !strings.Contains(got, "bootwright_kubevirt_host_kubeconfig_available") {
		t.Fatalf("the reachability gate must defer to the captured-kubeconfig gate when no kubeconfig exists, got when=%v", topTasks[gateIdx]["when"])
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
	if got := fmt.Sprint(tasks[refuseIdx]["when"]); !strings.Contains(got, "not (bootwright_destroy_authorize_unowned_vms") {
		t.Fatalf("kubevirt delete guard must be skipped under --authorize unowned-vms, got when=%v", tasks[refuseIdx]["when"])
	}

	for _, task := range tasks {
		body, ok := task["ansible.builtin.command"].(map[string]any)
		if !ok {
			continue
		}
		if strings.Contains(fmt.Sprint(body["argv"]), "virtctl") {
			t.Fatalf("%v: deleting the VirtualMachine already stops the VMI, so destroy must not pay for a separate virtctl stop", task["name"])
		}
	}

	dvReadIdx := findAnsibleTask(t, tasks, "Read KubeVirt DataVolume ownership labels")
	dvRefuseIdx := findAnsibleTask(t, tasks, "Refuse to delete a non-Bootwright KubeVirt DataVolume")
	dvDeleteIdx := findAnsibleTask(t, tasks, "Delete KubeVirt DataVolumes")
	if _, ok := tasks[dvReadIdx]["loop"]; !ok {
		t.Fatal("KubeVirt DataVolume probes must stay per-item so one NotFound cannot mask the other's failure")
	}
	dvDelete := tasks[dvDeleteIdx]
	if _, ok := dvDelete["loop"]; ok {
		t.Fatalf("KubeVirt DataVolume deletion must be a single idempotent kubectl call, got loop=%v", dvDelete["loop"])
	}
	dvDeleteBody, ok := dvDelete["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("KubeVirt DataVolume deletion must be a command task, got %v", dvDelete)
	}
	dvDeleteArgv := fmt.Sprint(dvDeleteBody["argv"])
	for _, want := range []string{
		"{{ bootwright_kubevirt_root_dv_name }}",
		"{{ bootwright_kubevirt_iso_dv_name }}",
		"--ignore-not-found=true",
	} {
		if !strings.Contains(dvDeleteArgv, want) {
			t.Fatalf("KubeVirt DataVolume deletion argv missing %q: %v", want, dvDeleteBody["argv"])
		}
	}
	if got := fmt.Sprint(dvDelete["when"]); !strings.Contains(got, "bootwright_kubevirt_dv_owner") {
		t.Fatalf("KubeVirt DataVolume deletion must stay gated on the per-item probes, got when=%v", dvDelete["when"])
	}
	if got := fmt.Sprint(tasks[findAnsibleTask(t, tasks, "Delete KubeVirt VirtualMachine")]["ansible.builtin.command"]); strings.Contains(got, "--wait=false") {
		t.Fatal("KubeVirt VirtualMachine deletion must block until the guest is provably absent")
	}
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
	if got := fmt.Sprint(tasks[dvRefuseIdx]["when"]); !strings.Contains(got, "not (bootwright_destroy_authorize_unowned_networks") {
		t.Fatalf("kubevirt DataVolume delete guard must be skipped only under --authorize unowned-networks, whose blast radius reaches another context, got when=%v", tasks[dvRefuseIdx]["when"])
	}
	if got := fmt.Sprint(tasks[dvRefuseIdx]["when"]); strings.Contains(got, "bootwright_destroy_authorize_unowned_vms") {
		t.Fatalf("kubevirt DataVolume delete guard must not be lifted by the VM authorization, got when=%v", tasks[dvRefuseIdx]["when"])
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

func TestOwnershipDestroyStopsBeforeEvidenceDeletionOnRemoverFailure(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_resource.yml")
	removePathsIdx := findAnsibleTask(t, tasks, "Remove recorded owned paths")
	removeRecordIdx := findAnsibleTask(t, tasks, "Remove destroyed ownership resource record")
	if removePathsIdx >= removeRecordIdx {
		t.Fatalf("owned paths must be removed before the record only after every remover succeeds (paths=%d record=%d)", removePathsIdx, removeRecordIdx)
	}
	for _, name := range []string{
		"Stop recorded libvirt domain",
		"Undefine recorded libvirt domain",
		"Stop recorded libvirt network",
		"Undefine recorded libvirt network",
		"Stop recorded systemd units",
		"Remove recorded podman container",
		"Close recorded firewalld ports",
		"List active mounts before removing owned paths",
		"Unmount active mounts under recorded owned paths",
	} {
		idx := findAnsibleTask(t, tasks, name)
		if idx >= removePathsIdx {
			t.Fatalf("%s must finish before any owned path or record is removed (task=%d paths=%d)", name, idx, removePathsIdx)
		}
		if tasks[idx]["failed_when"] == false {
			t.Fatalf("%s must not suppress a genuine remover/probe failure before evidence deletion", name)
		}
	}
	for _, name := range []string{
		"Stop recorded libvirt domain",
		"Undefine recorded libvirt domain",
		"Stop recorded libvirt network",
		"Undefine recorded libvirt network",
	} {
		idx := findAnsibleTask(t, tasks, name)
		failedWhen := fmt.Sprint(tasks[idx]["failed_when"])
		if !strings.Contains(failedWhen, "not found") && !strings.Contains(failedWhen, "not in") {
			t.Fatalf("%s must remain idempotent for an exactly absent resource, got failed_when=%v", name, tasks[idx]["failed_when"])
		}
	}
}

func TestOwnershipHelperWritesTimezoneQualifiedTimestamps(t *testing.T) {
	for _, path := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/resource.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/package_records_write.yml",
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
