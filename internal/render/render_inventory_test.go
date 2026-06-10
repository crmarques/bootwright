package render_test

import (
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/locality"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/desired"
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
		"bootwright_infra_component_hosts",
		"bootwright_infra_hosts",
		"bootwright_boot_hosts",
		"bootwright_controller_hosts",
		"bootwright_ocp_hosts",
		"bootwright_agent_node_hosts",
		"bootwright_machine_task_hosts",
		"bootwright_storage_hosts",
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
	componentHosts, ok := children["bootwright_infra_component_hosts"].(map[string]any)["hosts"].(map[string]any)
	if !ok {
		t.Fatalf("bootwright_infra_component_hosts.hosts is not a map: %v", children["bootwright_infra_component_hosts"])
	}
	if len(componentHosts) == 0 {
		t.Fatal("bootwright_infra_component_hosts should not be empty for the 001-sno-libvirt fixture")
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

func inventoryTestMachine(state v1alpha1.State, name string) (v1alpha1.Machine, bool) {
	for _, machine := range state.Machines {
		if machine.Metadata.Name == name {
			return machine, true
		}
	}
	return v1alpha1.Machine{}, false
}

func TestInventoryOmitsAnsibleUserWhenMachineSSHUserUnset(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "005-3nodes-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	bastion, ok := inventoryTestMachine(state, "bastion")
	if !ok || bastion.Spec.Access.SSH == nil {
		t.Fatal("Machine/bastion spec.access.ssh missing")
	}
	// The bastion omits access.ssh.user; it must NOT be defaulted from the
	// invoking process (that made rendered artifacts non-deterministic), so the
	// inventory carries no ansible_user and Ansible connects as the user running
	// the playbook (root after the sudo re-exec).
	if got := bastion.Spec.Access.SSH.User; got != "" {
		t.Fatalf("Machine.spec.access.ssh.user = %q, want empty (no process-user default)", got)
	}

	inv := render.Inventory(state, "")
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	serviceHost := hosts["bastion"].(map[string]any)
	if _, ok := serviceHost["ansible_user"]; ok {
		t.Fatalf("ansible_user should be omitted when Machine.spec.access.ssh.user is unset: %v", serviceHost)
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
	for i := range state.Machines {
		if state.Machines[i].Metadata.Name == "bastion" && state.Machines[i].Spec.Access.SSH != nil {
			state.Machines[i].Spec.Access.SSH.User = "provider-admin"
		}
	}

	inv := render.InventoryWithLocalityPolicy(state, "", locality.Policy{Deps: locality.Deps{
		Hostname: func() (string, error) {
			return "controller", nil
		},
	}})
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	serviceHost := hosts["bastion"].(map[string]any)
	if got := serviceHost["ansible_user"]; got != "provider-admin" {
		t.Fatalf("ansible_user got %v, want explicit Machine.spec.access.ssh.user", got)
	}
}

func TestInventoryUsesGeneratedHostSSHPrivateKeyPath(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "005-3nodes-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.Environments[0].Spec.Secrets["bastion-host-ssh"] = v1alpha1.EnvironmentSecretSpec{
		Generated: &v1alpha1.EnvironmentSecretGenerated{
			SSHKeyPair: &v1alpha1.GeneratedSSHKeyPairSpec{Type: v1alpha1.SSHKeyPairTypeEd25519},
		},
	}
	secretsDir := t.TempDir()
	inv := render.InventoryWithLocalityPolicy(state, secretsDir, locality.Policy{Deps: locality.Deps{
		Hostname: func() (string, error) {
			return "controller", nil
		},
	}})
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	serviceHost := hosts["bastion"].(map[string]any)
	want := filepath.Join(secretsDir, "bastion-host-ssh")
	if got := serviceHost["ansible_ssh_private_key_file"]; got != want {
		t.Fatalf("ansible_ssh_private_key_file got %v, want %s", got, want)
	}
}

func TestInventoryKeepsLibvirtProviderHostsForContextDestroy(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.Machines = append(state.Machines, v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "unused-host"},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{v1alpha1.MachineCapabilityLibvirt, v1alpha1.MachineCapabilityContainerRuntime},
			OS: v1alpha1.MachineOSSpec{
				Provided: v1alpha1.BoolPtr(true),
			},
			Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "192.0.2.10"}},
			Access: v1alpha1.MachineAccess{
				SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
			},
		},
	})
	state.InfraProviders = append(state.InfraProviders, v1alpha1.InfraProvider{
		Metadata: v1alpha1.Metadata{Name: "unused-provider"},
		Spec: v1alpha1.InfraProviderSpec{
			Type: v1alpha1.ProvisionerLibvirt,
			Libvirt: &v1alpha1.InfraProviderLibvirt{
				MachineRef:      v1alpha1.LocalObjectReference{Name: "unused-host"},
				URI:             "qemu:///system",
				MachineProfiles: []v1alpha1.MachineProfile{{Name: "unused-profile"}},
			},
		},
	})
	state.InfraComponents = append(state.InfraComponents, v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "unused-dns"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.ComponentSlotNameResolution, NameResolution: &v1alpha1.NameResolutionComponent{
				Implementation: v1alpha1.InfraComponentTypeDnsmasq,
				MachineRef:     v1alpha1.LocalObjectReference{Name: "unused-host"},
			}},
	})

	inv := render.Inventory(state, "")
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	if _, ok := hosts["unused-host"]; !ok {
		t.Fatalf("inventory must include libvirt provider host for context destroy cleanup: %v", hosts)
	}
	children := all["children"].(map[string]any)
	providerHosts := children[render.GroupProviderHosts].(map[string]any)["hosts"].(map[string]any)
	if _, ok := providerHosts["unused-host"]; !ok {
		t.Fatalf("%s must include libvirt provider host: %v", render.GroupProviderHosts, providerHosts)
	}
}

func TestHostGroupCountsBareMetalManagedArtifacts(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "002-sno-emul-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	counts := render.HostGroupCounts(state)
	if got := counts[render.GroupProviderHosts]; got != 0 {
		t.Errorf("%s: want 0 for bare-metal provider work, got %d", render.GroupProviderHosts, got)
	}
	if got := counts[render.GroupInfraComponentHosts]; got != 1 {
		t.Errorf("%s: want 1 for bare-metal managed artifacts, got %d", render.GroupInfraComponentHosts, got)
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
	if got := counts[render.GroupProviderHosts]; got != 0 {
		t.Errorf("%s: want 0 for direct bare-metal provider work, got %d", render.GroupProviderHosts, got)
	}
	if got := counts[render.GroupInfraComponentHosts]; got != 1 {
		t.Errorf("%s: want 1 for bastion artifact publication only, got %d", render.GroupInfraComponentHosts, got)
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
		"3-nodes-ocp-baremetal__rack1-srv1",
		"3-nodes-ocp-baremetal__rack1-srv2",
		"3-nodes-ocp-baremetal__rack1-srv3",
	} {
		if _, ok := hosts[name]; !ok {
			t.Fatalf("inventory hosts = %v, want %s", hosts, name)
		}
	}
	node := hosts["3-nodes-ocp-baremetal__rack1-srv1"].(map[string]any)
	if got := node["ansible_host"]; got != "localhost" {
		t.Fatalf("agent node ansible_host = %v, want localhost", got)
	}
	if got := node["ansible_connection"]; got != "local" {
		t.Fatalf("agent node ansible_connection = %v, want local", got)
	}
	if got := node["bootwright_agent_node_cluster_name"]; got != "3-nodes-ocp-baremetal" {
		t.Fatalf("agent node cluster var = %v", got)
	}
	if got := node["bootwright_agent_node_machine_name"]; got != "rack1-srv1" {
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
// fixture with managed services produces non-zero counts for provider,
// infra component, and infra groups, so the workflow does not skip ansible.
func TestHostGroupCountsLibvirtManaged(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	counts := render.HostGroupCounts(state)
	if got := counts[render.GroupProviderHosts]; got == 0 {
		t.Errorf("%s: want >0 for provider setup fixture, got 0", render.GroupProviderHosts)
	}
	if got := counts[render.GroupInfraComponentHosts]; got == 0 {
		t.Errorf("%s: want >0 for managed-services fixture, got 0", render.GroupInfraComponentHosts)
	}
	if got := counts[render.GroupInfraHosts]; got == 0 {
		t.Errorf("%s: want >0 for libvirt-substrate fixture, got 0", render.GroupInfraHosts)
	}
	if got := counts[render.GroupBootHosts]; got == 0 {
		t.Errorf("%s: want >0 for Redfish boot delegation, got 0", render.GroupBootHosts)
	}
}

func TestStorageInventoryUsesManagedCephHosts(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	secretsDir := "/context/secrets"
	inv := render.Inventory(state, secretsDir)
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	seedName := render.StorageSeedHostName(state.StorageClusters[0])
	seed := hosts[seedName].(map[string]any)
	if got := seed["ansible_host"]; got != "192.168.141.30" {
		t.Fatalf("storage seed ansible_host = %v, want 192.168.141.30", got)
	}
	if got := seed["ansible_user"]; got != "root" {
		t.Fatalf("storage seed ansible_user = %v, want root from Machine.spec.access.ssh.user", got)
	}
	if got := seed["ansible_ssh_private_key_file"]; got != filepath.Join(secretsDir, "ceph-storage-cluster-admin-ssh-key") {
		t.Fatalf("storage seed key = %v", got)
	}
	commonArgs, _ := seed["ansible_ssh_common_args"].(string)
	if !strings.Contains(commonArgs, "StrictHostKeyChecking=yes") || !strings.Contains(commonArgs, "UserKnownHostsFile="+filepath.Join(filepath.Dir(secretsDir), "trust", "ssh", "known_hosts")) {
		t.Fatalf("storage seed ansible_ssh_common_args = %q, want strict host key checking with managed trust", commonArgs)
	}
	if strings.Contains(commonArgs, "/dev/null") {
		t.Fatalf("storage seed ansible_ssh_common_args must not discard known hosts: %q", commonArgs)
	}
	nodeName := render.StorageNodeHostName("ceph-storage", "ceph-dc1-1")
	node := hosts[nodeName].(map[string]any)
	if got := node["ansible_host"]; got != "192.168.141.31" {
		t.Fatalf("storage node ansible_host = %v, want 192.168.141.31", got)
	}
	children := all["children"].(map[string]any)
	groupHosts := children[render.GroupStorageHosts].(map[string]any)["hosts"].(map[string]any)
	if _, ok := groupHosts[seedName]; !ok {
		t.Fatalf("%s hosts = %v, want %s", render.GroupStorageHosts, groupHosts, seedName)
	}
	if _, ok := groupHosts[nodeName]; !ok {
		t.Fatalf("%s hosts = %v, want %s", render.GroupStorageHosts, groupHosts, nodeName)
	}
	clusterGroup := render.StorageClusterGroupName("ceph-storage")
	clusterHosts := children[clusterGroup].(map[string]any)["hosts"].(map[string]any)
	if _, ok := clusterHosts[seedName]; !ok {
		t.Fatalf("%s hosts = %v, want %s", clusterGroup, clusterHosts, seedName)
	}
	if _, ok := clusterHosts[nodeName]; !ok {
		t.Fatalf("%s hosts = %v, want %s", clusterGroup, clusterHosts, nodeName)
	}
	counts := render.HostGroupCounts(state)
	if got := counts[render.GroupStorageHosts]; got != 7 {
		t.Fatalf("%s count = %d, want 7", render.GroupStorageHosts, got)
	}
	if got := counts[clusterGroup]; got != 7 {
		t.Fatalf("%s count = %d, want 7", clusterGroup, got)
	}
}
