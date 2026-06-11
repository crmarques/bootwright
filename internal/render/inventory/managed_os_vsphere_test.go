package inventory

import (
	"path/filepath"
	"strings"
	"testing"

	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

// TestManagedOSInstallVarsFromCephVSphereFixture pins the managed-OS shape
// for vCenter-managed machines: the machine task runs on the controller,
// the install ISO stages under the controller-local provider-state vmedia
// path, and the vsphere media/boot roles get their component contract.
func TestManagedOSInstallVarsFromCephVSphereFixture(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "008-ceph-3nodes-vsphere-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := VarsWithSecretsDir(state, "/context/secrets")
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("managed OS groups = %v", groups)
	}
	group := groups[0].(map[string]any)
	if got := group["name"]; got != "ceph-vsphere" {
		t.Fatalf("group name = %v", got)
	}
	components := group["components"].([]any)
	if len(components) != 3 {
		t.Fatalf("components = %v", components)
	}
	first := components[0].(map[string]any)
	if got := first["substrateApplyRole"]; got != "bootwright.core.machine_substrate_vsphere" {
		t.Fatalf("substrateApplyRole = %v", got)
	}
	if _, ok := first["substratePrepareRole"]; ok {
		t.Fatalf("substratePrepareRole = %v, want omitted: vsphere portgroups pre-exist", first["substratePrepareRole"])
	}
	if got := first["mediaPrepareRole"]; got != "bootwright.core.container_cluster_media_vsphere" {
		t.Fatalf("mediaPrepareRole = %v", got)
	}
	if got := first["machineRef"]; got != "localhost" {
		t.Fatalf("machineRef = %v, want localhost", got)
	}
	profile := first["profile"].(map[string]any)
	if dataDisks := profile["dataDisks"].([]any); len(dataDisks) != 2 {
		t.Fatalf("profile dataDisks = %v", dataDisks)
	}
	vsphere := first["vsphere"].(map[string]any)
	if got := vsphere["credentialsPath"]; got != "/context/secrets/vcenter-credentials" {
		t.Fatalf("vsphere credentialsPath = %v", got)
	}
	staging := vsphere["isoStaging"].(map[string]any)
	if got := staging["datastore"]; got != "/dc1/datastore/datastore1" {
		t.Fatalf("isoStaging datastore = %v", got)
	}
	osInstall := first["osInstall"].(map[string]any)
	image := osInstall["image"].(map[string]any)
	if image["sourceOnTarget"] != true {
		t.Fatalf("sourceOnTarget = %v, want true for the controller-local machine task host", image["sourceOnTarget"])
	}
	ks := osInstall["kickstart"].(map[string]any)
	storage := ks["storage"].(map[string]any)
	if storage["rootDisk"] != "sda" {
		t.Fatalf("kickstart storage = %v, want the pvscsi root disk sda", storage)
	}
	network := ks["network"].(map[string]any)
	device, _ := network["device"].(string)
	if !strings.HasPrefix(device, "00:50:56:") {
		t.Fatalf("kickstart network device = %v, want a vCenter manual-range MAC", device)
	}
	boot := first["boot"].(map[string]any)
	iso := boot["agentIso"].(map[string]any)
	stagePath, _ := iso["stagePath"].(string)
	if !strings.HasPrefix(stagePath, "{{ bootwright_provider_state_dir }}/vsphere/lab-vsphere-provider/vmedia/") || !strings.Contains(stagePath, "os-ceph-vsphere-ceph-0.iso") {
		t.Fatalf("managed OS stagePath = %v", stagePath)
	}
	fetchURL, _ := iso["fetchUrl"].(string)
	if !strings.HasPrefix(fetchURL, "[/dc1/datastore/datastore1] bootwright-vmedia/") {
		t.Fatalf("managed OS fetchUrl = %v, want a datastore attach path", fetchURL)
	}
	readiness := boot["readiness"].(map[string]any)
	if readiness["type"] != "none" {
		t.Fatalf("managed OS boot readiness = %v, want none", readiness)
	}
	if _, ok := boot["redfish"]; ok {
		t.Fatalf("managed OS boot redfish = %v, want omitted for vsphere", boot["redfish"])
	}
}
