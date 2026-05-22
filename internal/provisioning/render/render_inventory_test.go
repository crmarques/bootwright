package render_test

import (
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/provisioning/render"
)

// TestInventoryStructure pins the Ansible inventory groups that the
// layer playbooks select against (ansibleLimitForScope wires these into
// the --limit flag). Renaming a group here is a breaking change.
func TestInventoryStructure(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	inv := render.Inventory(state, "")
	all, ok := inv["all"].(map[string]any)
	if !ok {
		t.Fatalf("inventory missing 'all' root: %v", inv)
	}
	children, ok := all["children"].(map[string]any)
	if !ok {
		t.Fatalf("inventory 'all' missing children: %v", all)
	}
	for _, group := range []string{
		"bootwright_provider_hosts",
		"bootwright_infra_hosts",
		"bootwright_boot_hosts",
		"bootwright_ocp_hosts",
	} {
		if _, ok := children[group].(map[string]any); !ok {
			t.Fatalf("inventory missing required group %q (found groups: %v)", group, mapKeys(children))
		}
	}

	ocp := children["bootwright_ocp_hosts"].(map[string]any)
	hosts, ok := ocp["hosts"].(map[string]any)
	if !ok {
		t.Fatalf("bootwright_ocp_hosts missing hosts: %v", ocp)
	}
	if _, ok := hosts["localhost"]; !ok {
		t.Fatalf("bootwright_ocp_hosts must include localhost (openshift-install is controller-driven): %v", hosts)
	}

	provHosts, ok := children["bootwright_provider_hosts"].(map[string]any)["hosts"].(map[string]any)
	if !ok {
		t.Fatalf("bootwright_provider_hosts.hosts is not a map: %v", children["bootwright_provider_hosts"])
	}
	if len(provHosts) == 0 {
		t.Fatal("bootwright_provider_hosts should not be empty for the 001-sno-libvirt fixture")
	}
	allHosts := all["hosts"].(map[string]any)
	for name, raw := range allHosts {
		host := raw.(map[string]any)
		if _, ok := host["ansible_become"]; ok {
			t.Fatalf("inventory host %q should not force become; playbooks own privilege escalation: %v", name, host)
		}
	}
}

func TestInventoryDoesNotForceSSHUserWhenHostUserOmitted(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "005-3nodes-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	inv := render.Inventory(state, "")
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	bastion := hosts["bastion"].(map[string]any)
	if _, ok := bastion["ansible_user"]; ok {
		t.Fatalf("inventory forced ansible_user for omitted Host.spec.ssh.user: %v", bastion)
	}
}

func TestInventoryUsesExplicitHostSSHUser(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "005-3nodes-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.Hosts[0].Spec.SSH.User = "provider-admin"

	inv := render.Inventory(state, "")
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	bastion := hosts["bastion"].(map[string]any)
	if got := bastion["ansible_user"]; got != "provider-admin" {
		t.Fatalf("ansible_user got %v, want explicit Host.spec.ssh.user", got)
	}
}

func TestInventoryIgnoresUnusedProviderCapabilities(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.Hosts = append(state.Hosts, v1alpha1.Host{
		Metadata: v1alpha1.Metadata{Name: "unused-host"},
		Spec: v1alpha1.HostSpec{
			Addresses:    []v1alpha1.HostAddress{{Name: "ssh", Address: "192.0.2.10"}},
			SSH:          &v1alpha1.HostSSHSpec{AddressName: "ssh"},
			Capabilities: []string{v1alpha1.HostCapabilityLibvirt, v1alpha1.HostCapabilityContainerRuntime},
		},
	})
	state.InfraProviders[0].Spec.MachineProfiles = append(state.InfraProviders[0].Spec.MachineProfiles, v1alpha1.MachineProfileCapability{
		Name:    "unused-profile",
		Libvirt: &v1alpha1.MachineProfileLibvirtProvisioner{HostRef: v1alpha1.LocalObjectReference{Name: "unused-host"}},
	})
	state.InfraProviders[0].Spec.DNS = append(state.InfraProviders[0].Spec.DNS, v1alpha1.DNSCapability{
		Name:    "unused-dns",
		Dnsmasq: &v1alpha1.DnsmasqCapability{HostRef: v1alpha1.LocalObjectReference{Name: "unused-host"}},
	})

	inv := render.Inventory(state, "")
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	if _, ok := hosts["unused-host"]; ok {
		t.Fatalf("inventory included unused provider capability host: %v", hosts)
	}
}

func TestHostGroupCountsBareMetalManagedArtifacts(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "002-sno-emul-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	counts := render.HostGroupCounts(state)
	if got := counts[render.GroupProviderHosts]; got != 1 {
		t.Errorf("%s: want 1 for bare-metal managed artifacts, got %d", render.GroupProviderHosts, got)
	}
	if got := counts[render.GroupInfraHosts]; got != 0 {
		t.Errorf("%s: want 0 for bare-metal substrate, got %d", render.GroupInfraHosts, got)
	}
	if got := counts[render.GroupBootHosts]; got != 1 {
		t.Errorf("%s: want 1 for Redfish artifact staging, got %d", render.GroupBootHosts, got)
	}
	if got := counts[render.GroupOCPHosts]; got != 1 {
		t.Errorf("%s: localhost must always count as 1, got %d", render.GroupOCPHosts, got)
	}
}

func TestBareMetalCorporateFixtureInventoriesOnlyBastionServices(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "005-3nodes-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	counts := render.HostGroupCounts(state)
	if got := counts[render.GroupProviderHosts]; got != 1 {
		t.Errorf("%s: want 1 for bastion artifact publication only, got %d", render.GroupProviderHosts, got)
	}
	if got := counts[render.GroupInfraHosts]; got != 0 {
		t.Errorf("%s: want 0 for direct bare-metal machines, got %d", render.GroupInfraHosts, got)
	}
	if got := counts[render.GroupBootHosts]; got != 1 {
		t.Errorf("%s: want 1 for Redfish artifact staging, got %d", render.GroupBootHosts, got)
	}

	inv := render.Inventory(state, "")
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	if len(hosts) != 1 {
		t.Fatalf("inventory hosts = %v, want only bastion", hosts)
	}
	if _, ok := hosts["bastion"]; !ok {
		t.Fatalf("inventory hosts = %v, want bastion", hosts)
	}
}

// TestHostGroupCountsLibvirtManaged pins the symmetric case: a libvirt
// fixture with managed services produces non-zero counts for both
// provider and infra groups, so the workflow does not skip ansible.
func TestHostGroupCountsLibvirtManaged(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	counts := render.HostGroupCounts(state)
	if got := counts[render.GroupProviderHosts]; got == 0 {
		t.Errorf("%s: want >0 for managed-services fixture, got 0", render.GroupProviderHosts)
	}
	if got := counts[render.GroupInfraHosts]; got == 0 {
		t.Errorf("%s: want >0 for libvirt-substrate fixture, got 0", render.GroupInfraHosts)
	}
	if got := counts[render.GroupBootHosts]; got == 0 {
		t.Errorf("%s: want >0 for Redfish boot delegation, got 0", render.GroupBootHosts)
	}
}
