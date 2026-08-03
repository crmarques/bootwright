package inventory

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

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
	if got := staging["datastore"]; got != "datastore1" {
		t.Fatalf("isoStaging datastore = %v, want the object name", got)
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
	if !strings.HasPrefix(fetchURL, "[datastore1] bootwright-vmedia/") {
		t.Fatalf("managed OS fetchUrl = %v, want a datastore-name attach path", fetchURL)
	}
	readiness := boot["readiness"].(map[string]any)
	if readiness["type"] != "none" {
		t.Fatalf("managed OS boot readiness = %v, want none", readiness)
	}
	if _, ok := boot["redfish"]; ok {
		t.Fatalf("managed OS boot redfish = %v, want omitted for vsphere", boot["redfish"])
	}
}

func TestManagedOSTemplateCloneComponentCarriesNoInstallMedia(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "008-ceph-3nodes-vsphere-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	for i := range state.MachineInstallProfiles {
		installerArm := &state.MachineInstallProfiles[i].Spec.Installer
		installerArm.Anaconda = nil
		installerArm.TemplateClone = &v1alpha1.MachineInstallTemplateClone{
			Seed: v1alpha1.MachineInstallCloneSeed{CloudInit: &v1alpha1.MachineInstallCloudInitSeed{}},
		}
	}
	vars := VarsWithSecretsDir(state, "/context/secrets")
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	component := groups[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	if got := component["osInstallRole"]; got != "bootwright.core.machine_os_install_clone" {
		t.Fatalf("osInstallRole = %v, want the clone role", got)
	}
	if _, ok := component["osInstall"].(map[string]any)["guest"]; !ok {
		t.Fatalf("clone component osInstall = %v, want a guest seed", component["osInstall"])
	}
	for _, key := range []string{"boot", "mediaPrepareRole", "cleanupMediaRole"} {
		if _, ok := component[key]; ok {
			t.Fatalf("clone component carries %q = %v; a clone consumes no installer media and never boots from virtual media", key, component[key])
		}
	}
}

func TestManagedOSInstallNetworkFollowsTheDefaultRouteInterface(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "008-ceph-3nodes-vsphere-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	for i := range state.NetworkConfigs {
		template := state.NetworkConfigs[i].Spec.Template.NetworkConfig
		unrouted := map[string]any{
			"name":  "storage",
			"type":  "ethernet",
			"state": "up",
			"ipv4": map[string]any{
				"enabled": true,
				"dhcp":    false,
				"address": []any{map[string]any{"ip": "10.99.99.9", "prefix-length": 24}},
			},
		}
		existing, _ := template["interfaces"].([]any)
		template["interfaces"] = append([]any{unrouted}, existing...)
	}
	vars := VarsWithSecretsDir(state, "/context/secrets")
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	component := groups[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	network := component["osInstall"].(map[string]any)["kickstart"].(map[string]any)["network"].(map[string]any)
	if got := network["ip"]; got != "192.168.142.20" {
		t.Fatalf("network ip = %v, want the default-route interface address; the seed statics whichever NIC this names", got)
	}
	if got := network["prefix"]; got != 24 {
		t.Fatalf("network prefix = %v, want 24", got)
	}
	var routedMAC string
	for _, raw := range component["interfaces"].([]any) {
		entry := raw.(map[string]any)
		if entry["name"] == "primary" {
			routedMAC, _ = entry["macAddress"].(string)
		}
	}
	if routedMAC == "" {
		t.Fatalf("component interfaces = %v, want a MAC on the routed interface", component["interfaces"])
	}
	if got := network["device"]; got != routedMAC {
		t.Fatalf("network device = %v, want the routed interface MAC %s", got, routedMAC)
	}
}
