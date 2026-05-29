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
		Status:  StatusSupported,
		Summary: "OpenShift Virtualization with KubeVirt VMs",
	},
}

type ServiceKey struct {
	Kind        string
	Realisation string
}

type ServiceSupport struct {
	Key               ServiceKey
	ApplyRole         string
	DestroyRole       string
	HostCapabilities  []string
	ConflictFields    []string
	MergeStringFields []string
	DefaultPort       int
	Image             ServiceImage
	Status            Status
	Summary           string
}

type ServiceImage struct {
	Category   string
	Type       string
	Repository string
	Version    string
	Source     string
	LookupDate string
}

var serviceSupport = map[ServiceKey]ServiceSupport{
	{Kind: v1alpha1.ProviderServiceKindBMC, Realisation: "emulated"}: {
		Key:              ServiceKey{Kind: v1alpha1.ProviderServiceKindBMC, Realisation: "emulated"},
		ApplyRole:        "bmc_emulated",
		DestroyRole:      "bmc_emulated",
		HostCapabilities: []string{v1alpha1.HostCapabilityLibvirt},
		ConflictFields:   []string{"hostRef", "realisation", "applyRole", "destroyRole", "configKey"},
		Status:           StatusSupported,
		Summary:          "libvirt-hosted emulated Redfish BMC",
	},
	{Kind: v1alpha1.ComponentSlotLoadBalancer, Realisation: v1alpha1.InfraComponentTypeHAProxy}: {
		Key:              ServiceKey{Kind: v1alpha1.ComponentSlotLoadBalancer, Realisation: v1alpha1.InfraComponentTypeHAProxy},
		ApplyRole:        "load_balancer_haproxy",
		DestroyRole:      "load_balancer_haproxy",
		HostCapabilities: []string{v1alpha1.HostCapabilityContainerRuntime},
		ConflictFields:   []string{"hostRef", "realisation", "applyRole", "destroyRole", "capabilityName"},
		Image: ServiceImage{
			Category:   v1alpha1.ComponentImageCategoryLoadBalancer,
			Type:       v1alpha1.ComponentImageTypeHAProxy,
			Repository: "docker.io/library/haproxy",
			Version:    "3.3.10",
			Source:     "https://hub.docker.com/_/haproxy",
			LookupDate: "2026-05-21",
		},
		Status:  StatusSupported,
		Summary: "HAProxy managed load balancer",
	},
	{Kind: v1alpha1.ComponentSlotArtifacts, Realisation: v1alpha1.ArtifactServerProtocolHTTP}: {
		Key:              ServiceKey{Kind: v1alpha1.ComponentSlotArtifacts, Realisation: v1alpha1.ArtifactServerProtocolHTTP},
		ApplyRole:        "artifacts_http",
		DestroyRole:      "artifacts_http",
		HostCapabilities: []string{v1alpha1.HostCapabilityContainerRuntime},
		ConflictFields:   []string{"hostRef", "realisation", "applyRole", "destroyRole", "bindAddress", "listeners", "endpoints"},
		Image: ServiceImage{
			Category:   v1alpha1.ComponentImageCategoryArtifacts,
			Type:       v1alpha1.ComponentImageTypeArtifactsHTTP,
			Repository: "docker.io/library/nginx",
			Version:    "1.29.8-alpine3.23",
			Source:     "https://hub.docker.com/_/nginx",
			LookupDate: "2026-05-26",
		},
		Status:  StatusSupported,
		Summary: "HTTP/HTTPS artifact server",
	},
	{Kind: v1alpha1.ComponentSlotProxy, Realisation: v1alpha1.InfraComponentTypeSquid}: {
		Key:              ServiceKey{Kind: v1alpha1.ComponentSlotProxy, Realisation: v1alpha1.InfraComponentTypeSquid},
		ApplyRole:        "proxy_squid",
		DestroyRole:      "proxy_squid",
		HostCapabilities: []string{v1alpha1.HostCapabilityContainerRuntime},
		ConflictFields:   []string{"hostRef", "realisation", "applyRole", "destroyRole", "bindAddress", "port"},
		DefaultPort:      v1alpha1.DefaultSquidPort,
		Image: ServiceImage{
			Category:   v1alpha1.ComponentImageCategoryProxy,
			Type:       v1alpha1.ComponentImageTypeSquid,
			Repository: "docker.io/openeuler/squid",
			Version:    "7.5-oe2403sp3",
			Source:     "https://hub.docker.com/r/openeuler/squid",
			LookupDate: "2026-05-21",
		},
		Status:  StatusSupported,
		Summary: "Squid managed proxy",
	},
	{Kind: v1alpha1.ComponentSlotNameResolution, Realisation: v1alpha1.InfraComponentTypeDnsmasq}: {
		Key:               ServiceKey{Kind: v1alpha1.ComponentSlotNameResolution, Realisation: v1alpha1.InfraComponentTypeDnsmasq},
		ApplyRole:         "dns_dnsmasq",
		DestroyRole:       "dns_dnsmasq",
		HostCapabilities:  []string{v1alpha1.HostCapabilityContainerRuntime},
		ConflictFields:    []string{"hostRef", "realisation", "applyRole", "destroyRole", "bindAddress", "port"},
		MergeStringFields: []string{"additionalIngressHosts"},
		DefaultPort:       v1alpha1.DefaultDNSPort,
		Image: ServiceImage{
			Category:   v1alpha1.ComponentImageCategoryDNS,
			Type:       v1alpha1.ComponentImageTypeDnsmasq,
			Repository: "docker.io/dockurr/dnsmasq",
			Version:    "2.92_p2",
			Source:     "https://hub.docker.com/r/dockurr/dnsmasq",
			LookupDate: "2026-05-21",
		},
		Status:  StatusSupported,
		Summary: "dnsmasq managed name resolution",
	},
	{Kind: v1alpha1.ComponentSlotRegistry, Realisation: v1alpha1.InfraComponentTypeMirrorRegistry}: {
		Key:              ServiceKey{Kind: v1alpha1.ComponentSlotRegistry, Realisation: v1alpha1.InfraComponentTypeMirrorRegistry},
		ApplyRole:        "mirror_registry",
		DestroyRole:      "mirror_registry",
		HostCapabilities: []string{v1alpha1.HostCapabilityContainerRuntime},
		ConflictFields:   []string{"hostRef", "realisation", "applyRole", "destroyRole", "bindAddress", "port"},
		DefaultPort:      v1alpha1.DefaultMirrorRegistryPort,
		Image: ServiceImage{
			Category:   v1alpha1.ComponentImageCategoryRegistry,
			Type:       v1alpha1.ComponentImageTypeMirrorRegistry,
			Repository: "docker.io/library/registry",
			Version:    "3.1.1",
			Source:     "https://hub.docker.com/_/registry",
			LookupDate: "2026-05-21",
		},
		Status:  StatusSupported,
		Summary: "registry managed mirror",
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

func ServiceHostCapabilities(kind, realisation string) []string {
	return append([]string(nil), LookupService(kind, realisation).HostCapabilities...)
}

func ServiceConflictFields(kind, realisation string) []string {
	fields := LookupService(kind, realisation).ConflictFields
	if len(fields) == 0 {
		return []string{"hostRef", "realisation", "applyRole", "destroyRole"}
	}
	return append([]string(nil), fields...)
}

func ServiceMergeStringFields(kind, realisation string) []string {
	return append([]string(nil), LookupService(kind, realisation).MergeStringFields...)
}

func ServiceImagePin(kind, realisation string) (ServiceImage, bool) {
	image := LookupService(kind, realisation).Image
	return image, image.Type != ""
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
