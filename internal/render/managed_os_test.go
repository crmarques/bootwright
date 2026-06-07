package render

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
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
	marker := osInstall["marker"].(map[string]any)
	if marker["owner"] != "bootwright" || marker["path"] != "/etc/bootwright/install-marker.json" {
		t.Fatalf("marker vars = %v", marker)
	}
	if hash, ok := marker["desiredHash"].(string); !ok || !strings.HasPrefix(hash, "sha256:") {
		t.Fatalf("marker desiredHash = %v, want sha256", marker["desiredHash"])
	}
	image := osInstall["image"].(map[string]any)
	if image["kind"] != "media" || image["key"] != "rhel-9.7-x86_64-dvd.iso" {
		t.Fatalf("image vars = %v", image)
	}
	if image["mediaType"] != "dvd" {
		t.Fatalf("image mediaType = %v", image["mediaType"])
	}
	if !strings.HasSuffix(image["path"].(string), "/media/rhel-9.7-x86_64-dvd.iso") {
		t.Fatalf("image path = %v", image["path"])
	}
	if image["sourceOnTarget"] != true {
		t.Fatalf("sourceOnTarget = %v, want true for controller-local provider host", image["sourceOnTarget"])
	}
	installer := osInstall["installer"].(map[string]any)
	if _, ok := installer["sourceURL"]; ok {
		t.Fatalf("installer.sourceURL = %v, want omitted for DVD media", installer["sourceURL"])
	}
	repositories := installer["repositories"].([]any)
	if len(repositories) != 0 {
		t.Fatalf("installer.repositories = %v", repositories)
	}
	ks := osInstall["kickstart"].(map[string]any)
	if ks["hostname"] != "ceph-0" {
		t.Fatalf("kickstart hostname = %v", ks["hostname"])
	}
	network := ks["network"].(map[string]any)
	if network["ip"] != "192.168.134.20" || network["netmask"] != "255.255.255.0" {
		t.Fatalf("kickstart network = %v", network)
	}
	if got := network["device"]; got != "52:54:00:0f:07:d2" {
		t.Fatalf("kickstart network device = %v, want deterministic libvirt MAC", got)
	}
	storage := ks["storage"].(map[string]any)
	if storage["rootDisk"] != "vda" {
		t.Fatalf("kickstart storage = %v", storage)
	}
	boot := first["boot"].(map[string]any)
	iso := boot["agentIso"].(map[string]any)
	if !strings.Contains(iso["stagePath"].(string), "os-ceph-libvirt-ceph-0.iso") {
		t.Fatalf("managed OS stagePath = %v", iso["stagePath"])
	}
	readiness := boot["readiness"].(map[string]any)
	if readiness["type"] != "none" {
		t.Fatalf("managed OS boot readiness = %v, want none", readiness)
	}
	redfish := boot["redfish"].(map[string]any)
	if redfish["setBootSource"] != false {
		t.Fatalf("managed OS libvirt Redfish setBootSource = %v, want false", redfish["setBootSource"])
	}
}

func TestManagedOSInstallKeepsProfileRepositoriesAdditional(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.MachineInstallProfiles[0].Spec.Installer.Anaconda.Repositories = append(state.MachineInstallProfiles[0].Spec.Installer.Anaconda.Repositories,
		v1alpha1.MachineInstallRepository{ID: "extras", BaseURL: "https://repos.example.test/rhel/9/extras/x86_64/os/"},
	)

	vars := VarsWithSecretsDir(state, "/context/secrets")
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	first := groups[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	installer := first["osInstall"].(map[string]any)["installer"].(map[string]any)
	if _, ok := installer["sourceURL"]; ok {
		t.Fatalf("installer.sourceURL = %v, want omitted for DVD media", installer["sourceURL"])
	}
	repositories := installer["repositories"].([]any)
	if len(repositories) != 1 {
		t.Fatalf("repositories = %v", repositories)
	}
}

func TestManagedOSInstallUsesImageSourceURL(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.MachineImages[0].Spec.InstallSource = v1alpha1.MachineImageInstallSource{
		URL: "https://repos.example.test/rhel/9/BaseOS/x86_64/os/",
		Repositories: []v1alpha1.MachineInstallRepository{
			{ID: "appstream", BaseURL: "https://repos.example.test/rhel/9/AppStream/x86_64/os/"},
		},
	}

	vars := VarsWithSecretsDir(state, "/context/secrets")
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	first := groups[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	installer := first["osInstall"].(map[string]any)["installer"].(map[string]any)
	if got := installer["sourceURL"]; got != "https://repos.example.test/rhel/9/BaseOS/x86_64/os/" {
		t.Fatalf("installer.sourceURL = %v", got)
	}
	repositories := installer["repositories"].([]any)
	if len(repositories) != 1 {
		t.Fatalf("repositories = %v", repositories)
	}
}

func TestManagedOSInstallUsesRHSMInstallSource(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.Environments[0].Spec.Secrets["redhat-org"] = v1alpha1.EnvironmentSecretSpec{}
	state.Environments[0].Spec.Secrets["redhat-activation-key"] = v1alpha1.EnvironmentSecretSpec{}
	state.Environments[0].Spec.Entitlements = append(state.Environments[0].Spec.Entitlements, v1alpha1.EnvironmentEntitlement{
		Name:     "rhel",
		Provider: v1alpha1.EntitlementProviderRedHat,
		Product:  v1alpha1.EntitlementProductRHEL,
		RHSM: &v1alpha1.EnvironmentEntitlementRHSM{
			OrganizationRef:   v1alpha1.SecretRef{Name: "redhat-org"},
			ActivationKeyRef:  v1alpha1.SecretRef{Name: "redhat-activation-key"},
			ConnectToInsights: true,
		},
	})
	state.MachineImages[0].Spec.MediaType = v1alpha1.MachineImageMediaTypeBoot
	state.MachineImages[0].Spec.InstallSource = v1alpha1.MachineImageInstallSource{
		Type:           v1alpha1.MachineImageInstallSourceTypeRHSM,
		EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
	}

	vars := VarsWithSecretsDir(state, "/context/secrets")
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	first := groups[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	installer := first["osInstall"].(map[string]any)["installer"].(map[string]any)
	rhsm := installer["rhsm"].(map[string]any)
	if rhsm["organizationPath"] != "/context/secrets/redhat-org" {
		t.Fatalf("rhsm.organizationPath = %v", rhsm["organizationPath"])
	}
	if rhsm["activationKeyPath"] != "/context/secrets/redhat-activation-key" {
		t.Fatalf("rhsm.activationKeyPath = %v", rhsm["activationKeyPath"])
	}
	if rhsm["connectToInsights"] != true {
		t.Fatalf("rhsm.connectToInsights = %v", rhsm["connectToInsights"])
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
