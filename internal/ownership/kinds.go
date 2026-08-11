package ownership

import "sort"

type Kind string

const (
	KindBMCEmulator              Kind = "bmc-emulator"
	KindControllerNameResolver   Kind = "controller-name-resolver"
	KindInfraComponent           Kind = "infra-component"
	KindInfraComponentTransition Kind = "infra-component-transition"
	KindKubevirtMachine          Kind = "kubevirt-machine"
	KindLibvirtDomain            Kind = "libvirt-domain"
	KindLibvirtNetwork           Kind = "libvirt-network"
	KindManagedOSInstall         Kind = "managed-os-install"
	KindStorageCluster           Kind = "storage-cluster"
	KindVsphereMachine           Kind = "vsphere-machine"
	KindVsphereVMedia            Kind = "vsphere-vmedia"
)

type InventoryGroup string

const (
	GroupNone           InventoryGroup = ""
	GroupProvider       InventoryGroup = "provider"
	GroupInfraComponent InventoryGroup = "infra-component"
	GroupController     InventoryGroup = "controller"
	GroupStorage        InventoryGroup = "storage"
	GroupInfra          InventoryGroup = "infra"
)

var kindInventoryGroup = map[Kind]InventoryGroup{
	KindBMCEmulator:              GroupProvider,
	KindInfraComponent:           GroupInfraComponent,
	KindInfraComponentTransition: GroupInfraComponent,
	KindStorageCluster:           GroupStorage,
	KindLibvirtDomain:            GroupInfra,
	KindLibvirtNetwork:           GroupInfra,
	KindKubevirtMachine:          GroupInfra,
	KindVsphereMachine:           GroupInfra,
	KindVsphereVMedia:            GroupInfra,
	KindManagedOSInstall:         GroupInfra,
	KindControllerNameResolver:   GroupController,
}

func InventoryGroupForKind(kind string) (InventoryGroup, bool) {
	group, ok := kindInventoryGroup[Kind(kind)]
	return group, ok
}

func KnownKinds() []string {
	out := make([]string, 0, len(kindInventoryGroup))
	for kind := range kindInventoryGroup {
		out = append(out, string(kind))
	}
	sort.Strings(out)
	return out
}
