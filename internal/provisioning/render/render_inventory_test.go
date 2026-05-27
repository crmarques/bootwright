package render_test

import (
	"net"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/locality"
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

	inv := render.InventoryWithLocalityPolicy(state, "", locality.Policy{Deps: locality.Deps{
		Hostname: func() (string, error) {
			return "controller", nil
		},
	}})
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
		"bootwright_controller_hosts",
		"bootwright_ocp_hosts",
		"bootwright_agent_node_hosts",
	} {
		if _, ok := children[group].(map[string]any); !ok {
			t.Fatalf("inventory missing required group %q (found groups: %v)", group, mapKeys(children))
		}
	}

	for _, group := range []string{"bootwright_ocp_hosts", "bootwright_controller_hosts"} {
		grp := children[group].(map[string]any)
		hosts, ok := grp["hosts"].(map[string]any)
		if !ok {
			t.Fatalf("%s missing hosts: %v", group, grp)
		}
		if _, ok := hosts["localhost"]; !ok {
			t.Fatalf("%s must include localhost controller: %v", group, hosts)
		}
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
	localHost := allHosts["localhost"].(map[string]any)
	if got := localHost["ansible_connection"]; got != "local" {
		t.Fatalf("localhost ansible_connection = %v, want local", got)
	}
}

func TestInventoryUsesLocalhostForControllerWork(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	inv := render.InventoryWithLocalityPolicy(state, "", locality.Policy{Deps: locality.Deps{
		Hostname: func() (string, error) {
			return "controller", nil
		},
	}})
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	controller := hosts["localhost"].(map[string]any)
	if got := controller["ansible_connection"]; got != "local" {
		t.Fatalf("localhost ansible_connection = %v, want local", got)
	}

	children := all["children"].(map[string]any)
	groupHosts := children[render.GroupControllerHosts].(map[string]any)["hosts"].(map[string]any)
	if _, ok := groupHosts["localhost"]; !ok {
		t.Fatalf("%s hosts = %v, want localhost", render.GroupControllerHosts, groupHosts)
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
	serviceHost := hosts["bastion"].(map[string]any)
	if _, ok := serviceHost["ansible_user"]; ok {
		t.Fatalf("inventory forced ansible_user for omitted Host.spec.ssh.user: %v", serviceHost)
	}
}

func TestInventoryUsesLocalConnectionForLoopbackHostRefs(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "002-sno-emul-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	inv := render.Inventory(state, "/context/secrets")
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	serviceHost := hosts["services-host"].(map[string]any)
	if got := serviceHost["ansible_connection"]; got != "local" {
		t.Fatalf("ansible_connection = %v, want local for loopback service host: %v", got, serviceHost)
	}
	if got := serviceHost["ansible_host"]; got != "localhost" {
		t.Fatalf("ansible_host = %v, want localhost", got)
	}
	if _, ok := serviceHost["ansible_ssh_private_key_file"]; ok {
		t.Fatalf("local service host should not render SSH key material: %v", serviceHost)
	}
	if _, ok := serviceHost["ansible_user"]; ok {
		t.Fatalf("local service host should not force ansible_user: %v", serviceHost)
	}
}

func TestInventoryUsesLocalConnectionForControllerHostnameHostRefs(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "005-3nodes-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	inv := render.InventoryWithLocalityPolicy(state, "/context/secrets", locality.Policy{Deps: locality.Deps{
		Hostname: func() (string, error) {
			return "bastion", nil
		},
	}})
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	serviceHost := hosts["bastion"].(map[string]any)
	if got := serviceHost["ansible_connection"]; got != "local" {
		t.Fatalf("ansible_connection = %v, want local for controller-local bastion: %v", got, serviceHost)
	}
	if got := serviceHost["ansible_host"]; got != "bastion.bootwright.test" {
		t.Fatalf("ansible_host = %v, want bastion.bootwright.test", got)
	}
	if _, ok := serviceHost["ansible_ssh_private_key_file"]; ok {
		t.Fatalf("controller-local bastion should not render SSH key material: %v", serviceHost)
	}
}

func TestInventoryUsesLocalConnectionForControllerAddressAliasHostRefs(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "005-3nodes-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	inv := render.InventoryWithLocalityPolicy(state, "/context/secrets", locality.Policy{Deps: locality.Deps{
		Hostname: func() (string, error) {
			return "fedora", nil
		},
		InterfaceAddrs: func() ([]net.Addr, error) {
			return []net.Addr{
				&net.IPNet{IP: net.ParseIP("192.168.140.5"), Mask: net.CIDRMask(24, 32)},
			}, nil
		},
		LookupIP: func(host string) ([]net.IP, error) {
			if host == "bastion.bootwright.test" {
				return []net.IP{net.ParseIP("192.168.140.5")}, nil
			}
			return nil, &net.DNSError{Err: "not found", Name: host}
		},
	}})
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	serviceHost := hosts["bastion"].(map[string]any)
	if got := serviceHost["ansible_connection"]; got != "local" {
		t.Fatalf("ansible_connection = %v, want local for controller-local bastion alias: %v", got, serviceHost)
	}
	if _, ok := serviceHost["ansible_ssh_private_key_file"]; ok {
		t.Fatalf("controller-local bastion alias should not render SSH key material: %v", serviceHost)
	}
}

func TestInventoryUsesExplicitHostSSHUser(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "005-3nodes-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.Hosts[0].Spec.SSH.User = "provider-admin"

	inv := render.InventoryWithLocalityPolicy(state, "", locality.Policy{Deps: locality.Deps{
		Hostname: func() (string, error) {
			return "controller", nil
		},
	}})
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	serviceHost := hosts["bastion"].(map[string]any)
	if got := serviceHost["ansible_user"]; got != "provider-admin" {
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
	state.InfraComponents = append(state.InfraComponents, v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "unused-dns"},
		Spec: v1alpha1.InfraComponentSpec{NameResolution: &v1alpha1.NameResolutionComponent{
			Type:    v1alpha1.InfraComponentTypeDnsmasq,
			HostRef: v1alpha1.LocalObjectReference{Name: "unused-host"},
		}},
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
		t.Errorf("%s: want 1 for bastion host, got %d", render.GroupOCPHosts, got)
	}
	if got := counts[render.GroupAgentNodeHosts]; got != 1 {
		t.Errorf("%s: want 1 for one agent node host, got %d", render.GroupAgentNodeHosts, got)
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
	if got := counts[render.GroupAgentNodeHosts]; got != 3 {
		t.Errorf("%s: want 3 for agent node fanout, got %d", render.GroupAgentNodeHosts, got)
	}

	inv := render.Inventory(state, "")
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	if len(hosts) != 5 {
		t.Fatalf("inventory hosts = %v, want localhost, bastion, plus three agent node aliases", hosts)
	}
	for _, name := range []string{
		"localhost",
		"bastion",
		"3-nodes-ocp-baremetal__master-0",
		"3-nodes-ocp-baremetal__master-1",
		"3-nodes-ocp-baremetal__master-2",
	} {
		if _, ok := hosts[name]; !ok {
			t.Fatalf("inventory hosts = %v, want %s", hosts, name)
		}
	}
	node := hosts["3-nodes-ocp-baremetal__master-0"].(map[string]any)
	if got := node["ansible_host"]; got != "localhost" {
		t.Fatalf("agent node ansible_host = %v, want localhost", got)
	}
	if got := node["ansible_connection"]; got != "local" {
		t.Fatalf("agent node ansible_connection = %v, want local", got)
	}
	if got := node["bootwright_agent_node_cluster_name"]; got != "3-nodes-ocp-baremetal" {
		t.Fatalf("agent node cluster var = %v", got)
	}
	if got := node["bootwright_agent_node_machine_name"]; got != "master-0" {
		t.Fatalf("agent node machine var = %v", got)
	}
	children := all["children"].(map[string]any)
	groupName := render.AgentNodeGroupName("3-nodes-ocp-baremetal")
	groupHosts := children[groupName].(map[string]any)["hosts"].(map[string]any)
	if len(groupHosts) != 3 {
		t.Fatalf("%s hosts = %v, want three node aliases", groupName, groupHosts)
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
