package support

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type Status string

const (
	StatusSupported Status = "supported"
	StatusScaffold  Status = "scaffold"
	StatusUnknown   Status = "unknown"
)

type Dispatch struct {
	SubstrateRole string
	BMCRole       string
	BootRole      string
}

type RoleContract struct {
	HostSetupRoles       []string
	SubstrateApplyRole   string
	SubstrateDestroyRole string
	BMCApplyRole         string
	BMCDestroyRole       string
	BootApplyRole        string
	MediaPrepareRole     string
	RequiresKVM          bool
}

type DispatchSupport struct {
	Dispatch Dispatch
	Roles    RoleContract
	Status   Status
	Summary  string
}

func (s DispatchSupport) ApplySupported() bool {
	return s.Status == StatusSupported
}

var dispatchSupport = map[Dispatch]DispatchSupport{
	{SubstrateRole: "none", BMCRole: "none", BootRole: "none"}: {
		Dispatch: Dispatch{SubstrateRole: "none", BMCRole: "none", BootRole: "none"},
		Roles: RoleContract{
			BMCApplyRole:   "bmc_none",
			BMCDestroyRole: "bmc_none",
			BootApplyRole:  "boot_none",
		},
		Status:  StatusUnknown,
		Summary: "dispatch triplet is not in Bootwright's apply support registry",
	},
	{SubstrateRole: "libvirt", BMCRole: "emulated", BootRole: "redfish"}: {
		Dispatch: Dispatch{SubstrateRole: "libvirt", BMCRole: "emulated", BootRole: "redfish"},
		Roles: RoleContract{
			HostSetupRoles:       []string{"host_libvirt"},
			SubstrateApplyRole:   "substrate_libvirt",
			SubstrateDestroyRole: "substrate_libvirt",
			BMCApplyRole:         "bmc_emulated",
			BMCDestroyRole:       "bmc_emulated",
			BootApplyRole:        "boot_redfish",
			MediaPrepareRole:     "media_libvirt",
			RequiresKVM:          true,
		},
		Status:  StatusSupported,
		Summary: "libvirt with emulated Redfish BMC",
	},
	{SubstrateRole: "baremetal", BMCRole: "redfish", BootRole: "redfish"}: {
		Dispatch: Dispatch{SubstrateRole: "baremetal", BMCRole: "redfish", BootRole: "redfish"},
		Roles: RoleContract{
			SubstrateApplyRole:   "substrate_baremetal",
			SubstrateDestroyRole: "substrate_baremetal",
			BMCApplyRole:         "bmc_redfish",
			BMCDestroyRole:       "bmc_redfish",
			BootApplyRole:        "boot_redfish",
		},
		Status:  StatusSupported,
		Summary: "bare metal with Redfish virtual media",
	},
	{SubstrateRole: "vsphere", BMCRole: "none", BootRole: "vsphere"}: {
		Dispatch: Dispatch{SubstrateRole: "vsphere", BMCRole: "none", BootRole: "vsphere"},
		Roles: RoleContract{
			SubstrateApplyRole:   "substrate_vsphere",
			SubstrateDestroyRole: "substrate_vsphere",
			BMCApplyRole:         "bmc_none",
			BMCDestroyRole:       "bmc_none",
			BootApplyRole:        "boot_vsphere",
		},
		Status:  StatusScaffold,
		Summary: "vSphere schema and scaffold are present, but apply roles are not converged",
	},
	{SubstrateRole: "kubevirt", BMCRole: "none", BootRole: "kubevirt"}: {
		Dispatch: Dispatch{SubstrateRole: "kubevirt", BMCRole: "none", BootRole: "kubevirt"},
		Roles: RoleContract{
			SubstrateApplyRole:   "substrate_kubevirt",
			SubstrateDestroyRole: "substrate_kubevirt",
			BMCApplyRole:         "bmc_none",
			BMCDestroyRole:       "bmc_none",
			BootApplyRole:        "boot_kubevirt",
		},
		Status:  StatusScaffold,
		Summary: "OpenShift Virtualization schema and scaffold are present, but apply roles are not converged",
	},
}

type ServiceKey struct {
	Kind        string
	Realisation string
}

type ServiceSupport struct {
	Key         ServiceKey
	ApplyRole   string
	DestroyRole string
	Status      Status
	Summary     string
}

var serviceSupport = map[ServiceKey]ServiceSupport{
	{Kind: v1alpha1.ComponentSlotLoadBalancer, Realisation: "haProxy"}: {
		Key:         ServiceKey{Kind: v1alpha1.ComponentSlotLoadBalancer, Realisation: "haProxy"},
		ApplyRole:   "load_balancer_haproxy",
		DestroyRole: "load_balancer_haproxy",
		Status:      StatusSupported,
		Summary:     "HAProxy managed load balancer",
	},
	{Kind: v1alpha1.ComponentSlotArtifacts, Realisation: "http"}: {
		Key:         ServiceKey{Kind: v1alpha1.ComponentSlotArtifacts, Realisation: "http"},
		ApplyRole:   "artifacts_http",
		DestroyRole: "artifacts_http",
		Status:      StatusSupported,
		Summary:     "HTTP generated artifact publisher",
	},
	{Kind: v1alpha1.ComponentSlotProxy, Realisation: "squid"}: {
		Key:         ServiceKey{Kind: v1alpha1.ComponentSlotProxy, Realisation: "squid"},
		ApplyRole:   "proxy_squid",
		DestroyRole: "proxy_squid",
		Status:      StatusSupported,
		Summary:     "Squid managed proxy",
	},
	{Kind: v1alpha1.ComponentSlotNameResolution, Realisation: "dnsmasq"}: {
		Key:         ServiceKey{Kind: v1alpha1.ComponentSlotNameResolution, Realisation: "dnsmasq"},
		ApplyRole:   "dns_dnsmasq",
		DestroyRole: "dns_dnsmasq",
		Status:      StatusSupported,
		Summary:     "dnsmasq managed name resolution",
	},
	{Kind: v1alpha1.ComponentSlotRegistry, Realisation: "mirrorRegistry"}: {
		Key:         ServiceKey{Kind: v1alpha1.ComponentSlotRegistry, Realisation: "mirrorRegistry"},
		ApplyRole:   "mirror_registry",
		DestroyRole: "mirror_registry",
		Status:      StatusSupported,
		Summary:     "registry managed mirror",
	},
}

func LookupProfileProvisioner(kind string) DispatchSupport {
	switch kind {
	case v1alpha1.ProvisionerLibvirt:
		return LookupDispatch("libvirt", "emulated", "redfish")
	case v1alpha1.ProvisionerVSphere:
		return LookupDispatch("vsphere", "none", "vsphere")
	case v1alpha1.ProvisionerKubeVirt:
		return LookupDispatch("kubevirt", "none", "kubevirt")
	default:
		return LookupDispatch("none", "none", "none")
	}
}

func LookupMachineProvisioner(kind string) DispatchSupport {
	switch kind {
	case v1alpha1.ProvisionerBareMetal:
		return LookupDispatch("baremetal", "redfish", "redfish")
	default:
		return LookupDispatch("none", "none", "none")
	}
}

func LookupDispatch(substrateRole, bmcRole, bootRole string) DispatchSupport {
	dispatch := Dispatch{
		SubstrateRole: substrateRole,
		BMCRole:       bmcRole,
		BootRole:      bootRole,
	}
	if support, ok := dispatchSupport[dispatch]; ok {
		return support
	}
	return DispatchSupport{
		Dispatch: dispatch,
		Status:   StatusUnknown,
		Summary:  "dispatch triplet is not in Bootwright's apply support registry",
	}
}

func LookupService(kind, realisation string) ServiceSupport {
	key := ServiceKey{Kind: kind, Realisation: realisation}
	if support, ok := serviceSupport[key]; ok {
		return support
	}
	return ServiceSupport{
		Key:     key,
		Status:  StatusUnknown,
		Summary: "service realisation is not in Bootwright's apply support registry",
	}
}

func Entries() []DispatchSupport {
	out := make([]DispatchSupport, 0, len(dispatchSupport))
	for _, support := range dispatchSupport {
		out = append(out, support)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dispatch.SubstrateRole != out[j].Dispatch.SubstrateRole {
			return out[i].Dispatch.SubstrateRole < out[j].Dispatch.SubstrateRole
		}
		if out[i].Dispatch.BMCRole != out[j].Dispatch.BMCRole {
			return out[i].Dispatch.BMCRole < out[j].Dispatch.BMCRole
		}
		return out[i].Dispatch.BootRole < out[j].Dispatch.BootRole
	})
	return out
}

func ServiceEntries() []ServiceSupport {
	out := make([]ServiceSupport, 0, len(serviceSupport))
	for _, support := range serviceSupport {
		out = append(out, support)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key.Kind != out[j].Key.Kind {
			return out[i].Key.Kind < out[j].Key.Kind
		}
		return out[i].Key.Realisation < out[j].Key.Realisation
	})
	return out
}
