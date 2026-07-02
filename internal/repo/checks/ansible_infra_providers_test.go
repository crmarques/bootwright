package repocheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/roles"
)

// TestPackageRemovalGuardedByOwnershipAndRequirements pins the host-package safety
// contract: Bootwright removes a package only when its ownership record proves
// Bootwright introduced it (preexisting is false, defaulting to true so an
// operator-preexisting package is kept) AND nothing else still requires it. A
// regression that inverts the preexisting default or drops the requiredBy gate
// would silently start removing operator-preexisting packages (chrony, podman, ...).
func TestPackageRemovalGuardedByOwnershipAndRequirements(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/package_remove_one.yml")
	remove := tasks[findAnsibleTask(t, tasks, "Remove package that Bootwright introduced")]
	when := fmt.Sprint(remove["when"])
	// Removal only when the package is NOT operator-preexisting; the default is true
	// so a missing flag keeps the package.
	if !strings.Contains(when, "not (bootwright_ownership_package_record.preexisting") || !strings.Contains(when, "preexisting | default(true)") {
		t.Fatalf("package removal must be gated on preexisting defaulting to true (keep), got when=%v", remove["when"])
	}
	// Removal only when nothing else still requires the package.
	if !strings.Contains(when, "bootwright_ownership_package_remaining_required_by") || !strings.Contains(when, "length == 0") {
		t.Fatalf("package removal must be gated on an empty remaining requiredBy, got when=%v", remove["when"])
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
	// Legacy-BMC TLS relaxation: ssl_protocols/ssl_ciphers render only when the
	// component sets them, so the default keeps the server's built-in TLS.
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
	// The OpenSSL config renders unconditionally so a change to the desired
	// SANs/CN surfaces as a content change; it must not be gated on existing
	// material or an edited SAN list would keep serving a stale certificate.
	if when := fmt.Sprint(tasks[tlsConfigIdx]["when"]); strings.Contains(when, "bootwright_artifacts_tls_material_present") {
		t.Fatalf("%s must render unconditionally to detect SAN/CN drift, got when=%v", tasks[tlsConfigIdx]["name"], tasks[tlsConfigIdx]["when"])
	}
	// Cert generation preserves existing material unless it is absent or the
	// rendered OpenSSL config (SANs/CN) changed, which rotates the certificate.
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

// TestInfraComponentContainerGateUsesLiveProvenanceLabel locks the multi-context fix:
// the container apply-mode gate must read ownership from the LIVE container's
// Bootwright provenance labels, not from a per-context ownership-record stat. The
// bastion runs one global podman store shared by every context while each context
// keeps its own ownership/ dir, so a per-context stat misreports a shared container
// created by another context as foreign and blocks the second context's apply.
func TestInfraComponentContainerGateUsesLiveProvenanceLabel(t *testing.T) {
	rel := bootwrightCollectionRoleRoot + "/ownership_record/tasks/infra_component_container_gate.yml"
	body := readRepoFile(t, rel)
	// The per-context ownership-record stat oracle must be gone entirely.
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

// TestInfraComponentRolesPassContainerNameToGate verifies every container-backed
// infra-component role feeds its container name to the gate, so the foreign-squatter
// name probe is armed (an empty name degrades to label-probe-only).
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

func TestInfraComponentDestroyCleanupUsesBootwrightPodmanLabels(t *testing.T) {
	roles := map[string]string{
		"ansible/collections/ansible_collections/bootwright/core/roles/infra_component_artifact_server_http/tasks/destroy.yml":    "artifacts",
		"ansible/collections/ansible_collections/bootwright/core/roles/infra_component_load_balancer_haproxy/tasks/destroy.yml":   "load-balancer",
		"ansible/collections/ansible_collections/bootwright/core/roles/infra_component_name_resolution_dnsmasq/tasks/destroy.yml": "nameResolution",
		"ansible/collections/ansible_collections/bootwright/core/roles/infra_component_proxy_squid/tasks/destroy.yml":             "proxy",
		"ansible/collections/ansible_collections/bootwright/core/roles/infra_component_registry_mirror/tasks/destroy.yml":         "registry",
	}
	for path, kind := range roles {
		body := readRepoFile(t, path)
		if strings.Contains(body, "bootwright_process_cleanup_pattern") {
			t.Fatalf("%s must not cleanup provider containers by process pattern", path)
		}
		for _, want := range []string{
			"bootwright_process_cleanup_podman_filters:",
			"label=bootwright.kind=" + kind,
			"label=bootwright.provider={{ bootwright_component.providerName }}",
			"label=bootwright.name={{ bootwright_component.name }}",
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
	if got := fmt.Sprint(resolve["bootwright_libvirt_managed_os_reset"]); !strings.Contains(got, "bootwright_component.osManaged") || !strings.Contains(got, "bootwright_apply_mode") {
		t.Fatalf("%s must gate managed OS disk reset on osManaged and the override apply mode, got %v", tasks[resolveIdx]["name"], got)
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

	// A scoped destroy must restrict recorded-resource cleanup to the selected
	// roots so it cannot tear down a co-located cluster's VMs/disks on a shared
	// host; an unscoped destroy leaves the var undefined and cleans everything.
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
		// The sweep must require the live Bootwright ownership marker, not just a
		// disk-path match, so a foreign VM parked under the context root is not
		// undefined.
		"dumpxml",
		// Prefix-agnostic: a managed-OS install rewrites the namespace prefix
		// (ns0:), so the sweep keys on the element local name, not <bootwright:...>.
		"([A-Za-z0-9_]+:)?context>",
		"bootwright_libvirt_context_owned_domains",
		"item.bootwright_libvirt_domain_name in bootwright_libvirt_context_owned_domains",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("libvirt context sweep missing %q", want)
		}
	}
}

// TestLibvirtContextSweepPreservesForeignDiskUnderRoot pins that the infra-destroy
// context sweep never deletes a foreign (non-Bootwright) domain's disk parked under
// the namespaced context root: the blanket root removal is gated on no foreign
// domain using the root, and when one co-resides only the Bootwright-owned machine
// subtrees are removed.
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
	// The marker must be matched prefix-agnostically: the managed-OS media insert
	// round-trips the domain XML through ElementTree, which rewrites the bootwright
	// namespace prefix (ns0:), so a literal <bootwright:...> match would reject a
	// genuinely Bootwright-owned VM.
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
	// --force-unowned relaxes the refusal so a renamed/unmarked domain is still
	// torn down; the marker mismatch must otherwise stay fail-closed.
	if got := fmt.Sprint(tasks[refuseIdx]["when"]); !strings.Contains(got, "not (bootwright_destroy_force_unowned") {
		t.Fatalf("libvirt destroy guard must be skipped under --force-unowned, got when=%v", tasks[refuseIdx]["when"])
	}
}

func TestKubeVirtDestroyVerifiesOwnershipLabel(t *testing.T) {
	topTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/destroy.yml")
	// Fail closed by default: an unreachable host cluster aborts with guidance (not
	// a raw kubectl error); --skip-unreachable opts into treating the guests as
	// already-absent. The reachability probe and assert run before the teardown,
	// which is gated on the host being reachable.
	probeIdx := findAnsibleTask(t, topTasks, "Probe KubeVirt host cluster reachability")
	gateIdx := findAnsibleTask(t, topTasks, "Require the KubeVirt host cluster to be reachable")
	blockIdx := findAnsibleTask(t, topTasks, "Tear down KubeVirt guest on the reachable host cluster")
	if !(probeIdx < gateIdx && gateIdx < blockIdx) {
		t.Fatalf("kubevirt destroy must probe and gate host reachability before the teardown (probe=%d gate=%d block=%d)", probeIdx, gateIdx, blockIdx)
	}
	if got := fmt.Sprint(topTasks[gateIdx]["when"]); !strings.Contains(got, "not (bootwright_destroy_skip_unreachable") {
		t.Fatalf("host-reachability gate must fail closed unless --skip-unreachable, got when=%v", topTasks[gateIdx]["when"])
	}
	if _, ok := topTasks[gateIdx]["ansible.builtin.assert"]; !ok {
		t.Fatalf("host-reachability gate must be a hard assert, got %v", topTasks[gateIdx])
	}
	if got := fmt.Sprint(topTasks[blockIdx]["when"]); !strings.Contains(got, "bootwright_kubevirt_host_reachable") {
		t.Fatalf("guest teardown must be gated on host reachability, got when=%v", topTasks[blockIdx]["when"])
	}
	// The ownership read/decide/refuse/delete tasks live inside that reachable-host
	// block, so a no-op on an unreachable host never reaches a kubectl delete.
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
		t.Fatalf("kubevirt delete guard must be skipped under --force-unowned, got when=%v", tasks[refuseIdx]["when"])
	}

	// The DataVolume deletes must be gated the same way: read each DV's ownership
	// label, refuse a non-Bootwright DV, and only then delete.
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
		t.Fatalf("kubevirt DataVolume delete guard must be skipped under --force-unowned, got when=%v", tasks[dvRefuseIdx]["when"])
	}

	// The agent-ISO DataVolume is created by virtctl image-upload, which stamps no
	// labels, so the boot role must label it managed-by=bootwright — otherwise the
	// destroy gate above could never recognize it as owned.
	bootTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml")
	uploadIdx := findAnsibleTask(t, bootTasks, "Upload KubeVirt agent ISO DataVolume")
	labelIdx := findAnsibleTask(t, bootTasks, "Label KubeVirt agent ISO DataVolume as Bootwright-managed")
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

func TestOwnershipDestroyReadsPreRenameVMediaAttrs(t *testing.T) {
	// Ownership records are persisted JSON written by previous applies, so the
	// vMediaUnit/vMediaPort attr rename must keep read compatibility with the
	// lowercase vmediaUnit/vmediaPort keys recorded by earlier versions;
	// otherwise destroy silently leaves the recorded vmedia unit running and
	// its firewall port open.
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
