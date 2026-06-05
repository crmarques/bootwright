package render

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/state/desired"
)

func TestManagedOSInstallVarsFromCephLibvirtFixture(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := VarsWithSecretsDir(state, "/context/secrets")
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("managed OS groups = %v", groups)
	}
	group := groups[0].(map[string]any)
	if got := group["name"]; got != "ceph-libvirt" {
		t.Fatalf("group name = %v", got)
	}
	components := group["components"].([]any)
	if len(components) != 3 {
		t.Fatalf("components = %v", components)
	}
	first := components[0].(map[string]any)
	if got := first["substratePrepareRole"]; got != "bootwright.core.machine_substrate_libvirt" {
		t.Fatalf("substratePrepareRole = %v", got)
	}
	if got := first["substratePrepareFrom"]; got != "network" {
		t.Fatalf("substratePrepareFrom = %v", got)
	}
	if got := first["substrateApplyFrom"]; got != "machine" {
		t.Fatalf("substrateApplyFrom = %v", got)
	}
	profile := first["profile"].(map[string]any)
	dataDisks := profile["dataDisks"].([]any)
	if len(dataDisks) != 2 {
		t.Fatalf("profile dataDisks = %v", dataDisks)
	}
	osInstall := first["osInstall"].(map[string]any)
	image := osInstall["image"].(map[string]any)
	if image["kind"] != "media" || image["key"] != "rhel-9.8-x86_64-boot.iso" {
		t.Fatalf("image vars = %v", image)
	}
	if !strings.HasSuffix(image["path"].(string), "/media/rhel-9.8-x86_64-boot.iso") {
		t.Fatalf("image path = %v", image["path"])
	}
	if image["sourceOnTarget"] != true {
		t.Fatalf("sourceOnTarget = %v, want true for controller-local provider host", image["sourceOnTarget"])
	}
	ks := osInstall["kickstart"].(map[string]any)
	if ks["hostname"] != "ceph-0" {
		t.Fatalf("kickstart hostname = %v", ks["hostname"])
	}
	network := ks["network"].(map[string]any)
	if network["ip"] != "192.168.134.20" || network["netmask"] != "255.255.255.0" {
		t.Fatalf("kickstart network = %v", network)
	}
	boot := first["boot"].(map[string]any)
	iso := boot["agentIso"].(map[string]any)
	if !strings.Contains(iso["stagePath"].(string), "os-ceph-libvirt-ceph-0.iso") {
		t.Fatalf("managed OS stagePath = %v", iso["stagePath"])
	}
}

func TestManagedStorageOSMachinesEnterInfraInventory(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	members := HostGroupMembers(state)
	if got := strings.Join(members[GroupInfraHosts], ","); got != "lab-host" {
		t.Fatalf("infra hosts = %v", members[GroupInfraHosts])
	}
	if got := strings.Join(members[GroupProviderHosts], ","); got != "lab-host" {
		t.Fatalf("provider hosts = %v", members[GroupProviderHosts])
	}
	wantMachineTaskHosts := strings.Join([]string{
		ManagedOSHostName("ceph-libvirt", "ceph-0"),
		ManagedOSHostName("ceph-libvirt", "ceph-1"),
		ManagedOSHostName("ceph-libvirt", "ceph-2"),
	}, ",")
	if got := strings.Join(members[GroupMachineTaskHosts], ","); got != wantMachineTaskHosts {
		t.Fatalf("machine task hosts = %v, want %s", members[GroupMachineTaskHosts], wantMachineTaskHosts)
	}
	if got := strings.Join(members[ManagedOSGroupName("ceph-libvirt")], ","); got != wantMachineTaskHosts {
		t.Fatalf("managed OS group hosts = %v, want %s", members[ManagedOSGroupName("ceph-libvirt")], wantMachineTaskHosts)
	}

	inv := Inventory(state, "/context/secrets")
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	pseudoHost := hosts[ManagedOSHostName("ceph-libvirt", "ceph-0")].(map[string]any)
	if got := pseudoHost["bootwright_machine_task_cluster_name"]; got != "ceph-libvirt" {
		t.Fatalf("machine task cluster = %v", got)
	}
	if got := pseudoHost["bootwright_machine_task_machine_name"]; got != "ceph-0" {
		t.Fatalf("machine task machine = %v", got)
	}
	if got := pseudoHost["bootwright_machine_task_provider_host_name"]; got != "lab-host" {
		t.Fatalf("machine task provider host = %v", got)
	}
}
