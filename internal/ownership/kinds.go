package ownership

import "sort"

// Kind is a Bootwright ownership resource kind. The Ansible ownership_record
// role writes these literals (bootwright_ownership_kind: <Kind>) on apply; the
// Go side reads them back to classify recorded resources for destroy targeting
// and orphan reporting. This file is the single source of truth for the kind
// taxonomy: render/inventory consumes the classification table here instead of
// an inline switch, and a fitness test (internal/repo/checks) asserts every kind
// the roles emit is registered below, so a new kind cannot drift in on one side
// only.
type Kind string

const (
	KindBMCEmulator            Kind = "bmc-emulator"
	KindControllerNameResolver Kind = "controller-name-resolver"
	KindInfraComponent         Kind = "infra-component"
	KindKubevirtMachine        Kind = "kubevirt-machine"
	KindLibvirtDomain          Kind = "libvirt-domain"
	KindLibvirtNetwork         Kind = "libvirt-network"
	KindManagedOSInstall       Kind = "managed-os-install"
	KindStorageCluster         Kind = "storage-cluster"
	KindVsphereMachine         Kind = "vsphere-machine"
	KindVsphereVMedia          Kind = "vsphere-vmedia"
)

// InventoryGroup names the inventory host-set a recorded resource's host joins so
// a destroy can target it even after the desired-state object is gone. GroupNone
// marks a kind reclaimed by a dedicated play (not the ownership host-group
// sweep), so its recorded host is intentionally not merged into any group.
type InventoryGroup string

const (
	GroupNone           InventoryGroup = ""
	GroupProvider       InventoryGroup = "provider"
	GroupInfraComponent InventoryGroup = "infra-component"
	GroupStorage        InventoryGroup = "storage"
	GroupInfra          InventoryGroup = "infra"
)

// kindInventoryGroup classifies every known Kind into the inventory host-set its
// recorded host joins. It replaces the inline switch the renderer used to carry
// and documents kinds intentionally kept out of the sweep.
var kindInventoryGroup = map[Kind]InventoryGroup{
	KindBMCEmulator:      GroupProvider,
	KindInfraComponent:   GroupInfraComponent,
	KindStorageCluster:   GroupStorage,
	KindLibvirtDomain:    GroupInfra,
	KindLibvirtNetwork:   GroupInfra,
	KindKubevirtMachine:  GroupInfra,
	KindVsphereMachine:   GroupInfra,
	KindVsphereVMedia:    GroupInfra,
	KindManagedOSInstall: GroupInfra,
	// Reclaimed by the container-cluster agent-destroy play on the controller
	// host, not the ownership host-group sweep, so its host joins no group.
	KindControllerNameResolver: GroupNone,
}

// InventoryGroupForKind returns the inventory host-set classification for a
// recorded kind and whether the kind is registered. An unregistered kind returns
// (GroupNone, false) so the caller can fall back deliberately and surface drift
// rather than silently dropping the record from every group.
func InventoryGroupForKind(kind string) (InventoryGroup, bool) {
	group, ok := kindInventoryGroup[Kind(kind)]
	return group, ok
}

// KnownKinds returns the registered ownership kinds, sorted, for fitness tests
// and tooling that needs to enumerate the taxonomy.
func KnownKinds() []string {
	out := make([]string, 0, len(kindInventoryGroup))
	for kind := range kindInventoryGroup {
		out = append(out, string(kind))
	}
	sort.Strings(out)
	return out
}
