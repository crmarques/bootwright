package roles

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type Status string

const (
	StatusSupported Status = "supported"
	StatusUnknown   Status = "unknown"
)

type Dispatch struct {
	SubstrateRole string
	BMCRole       string
	BootRole      string
}

func (d Dispatch) ExternalBMC() bool {
	return d.BMCRole == "redfish"
}

type RoleContract struct {
	MachineSetupRoles    []string
	SubstratePrepareRole string
	SubstratePrepareFrom string
	SubstrateApplyRole   string
	SubstrateApplyFrom   string
	SubstrateDestroyRole string
	BMCApplyRole         string
	BMCDestroyRole       string
	BootApplyRole        string
	MediaPrepareRole     string
	CleanupMediaRole     string
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
			BMCApplyRole:   "bootwright.core.provider_service_bmc_none",
			BMCDestroyRole: "bootwright.core.provider_service_bmc_none",
			BootApplyRole:  "bootwright.core.container_cluster_boot_none",
		},
		Status:  StatusUnknown,
		Summary: "dispatch triplet is not in Bootwright's apply support registry",
	},
	{SubstrateRole: "libvirt", BMCRole: "emulated", BootRole: "redfish"}: {
		Dispatch: Dispatch{SubstrateRole: "libvirt", BMCRole: "emulated", BootRole: "redfish"},
		Roles: RoleContract{
			MachineSetupRoles:    []string{"bootwright.core.provider_host_libvirt"},
			SubstratePrepareRole: "bootwright.core.machine_substrate_libvirt",
			SubstratePrepareFrom: "network",
			SubstrateApplyRole:   "bootwright.core.machine_substrate_libvirt",
			SubstrateApplyFrom:   "machine",
			SubstrateDestroyRole: "bootwright.core.machine_substrate_libvirt",
			BMCApplyRole:         "bootwright.core.provider_service_bmc_emulated",
			BMCDestroyRole:       "bootwright.core.provider_service_bmc_emulated",
			BootApplyRole:        "bootwright.core.container_cluster_boot_redfish",
			MediaPrepareRole:     "bootwright.core.container_cluster_media_libvirt",
			CleanupMediaRole:     "bootwright.core.container_cluster_boot_redfish",
			RequiresKVM:          true,
		},
		Status:  StatusSupported,
		Summary: "libvirt with emulated Redfish BMC",
	},
	{SubstrateRole: "baremetal", BMCRole: "redfish", BootRole: "redfish"}: {
		Dispatch: Dispatch{SubstrateRole: "baremetal", BMCRole: "redfish", BootRole: "redfish"},
		Roles: RoleContract{
			SubstrateApplyRole:   "bootwright.core.machine_substrate_baremetal",
			SubstrateDestroyRole: "bootwright.core.machine_substrate_baremetal",
			BMCApplyRole:         "bootwright.core.provider_service_bmc_external_redfish",
			BMCDestroyRole:       "bootwright.core.provider_service_bmc_external_redfish",
			BootApplyRole:        "bootwright.core.container_cluster_boot_redfish",
			CleanupMediaRole:     "bootwright.core.container_cluster_boot_redfish",
		},
		Status:  StatusSupported,
		Summary: "bare metal with Redfish virtual media",
	},
	{SubstrateRole: "vsphere", BMCRole: "none", BootRole: "vsphere"}: {
		Dispatch: Dispatch{SubstrateRole: "vsphere", BMCRole: "none", BootRole: "vsphere"},
		Roles: RoleContract{
			SubstrateApplyRole:   "bootwright.core.machine_substrate_vsphere",
			SubstrateDestroyRole: "bootwright.core.machine_substrate_vsphere",
			BMCApplyRole:         "bootwright.core.provider_service_bmc_none",
			BMCDestroyRole:       "bootwright.core.provider_service_bmc_none",
			BootApplyRole:        "bootwright.core.container_cluster_boot_vsphere",
			MediaPrepareRole:     "bootwright.core.container_cluster_media_vsphere",
			CleanupMediaRole:     "bootwright.core.container_cluster_boot_vsphere",
		},
		Status:  StatusSupported,
		Summary: "vCenter-managed vSphere VMs",
	},
	{SubstrateRole: "kubevirt", BMCRole: "none", BootRole: "kubevirt"}: {
		Dispatch: Dispatch{SubstrateRole: "kubevirt", BMCRole: "none", BootRole: "kubevirt"},
		Roles: RoleContract{
			SubstrateApplyRole:   "bootwright.core.machine_substrate_kubevirt",
			SubstrateDestroyRole: "bootwright.core.machine_substrate_kubevirt",
			BMCApplyRole:         "bootwright.core.provider_service_bmc_none",
			BMCDestroyRole:       "bootwright.core.provider_service_bmc_none",
			BootApplyRole:        "bootwright.core.container_cluster_boot_kubevirt",
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
		ApplyRole:        "bootwright.core.provider_service_bmc_emulated",
		DestroyRole:      "bootwright.core.provider_service_bmc_emulated",
		HostCapabilities: []string{v1alpha1.MachineCapabilityLibvirt},
		ConflictFields:   []string{"machineRef", "realisation", "applyRole", "destroyRole", "configKey"},
		Status:           StatusSupported,
		Summary:          "libvirt-hosted emulated Redfish BMC",
	},
	{Kind: v1alpha1.ComponentSlotLoadBalancer, Realisation: v1alpha1.InfraComponentTypeHAProxy}: {
		Key:              ServiceKey{Kind: v1alpha1.ComponentSlotLoadBalancer, Realisation: v1alpha1.InfraComponentTypeHAProxy},
		ApplyRole:        "bootwright.core.infra_component_load_balancer_haproxy",
		DestroyRole:      "bootwright.core.infra_component_load_balancer_haproxy",
		HostCapabilities: []string{v1alpha1.MachineCapabilityContainerRuntime},
		ConflictFields:   []string{"machineRef", "realisation", "applyRole", "destroyRole", "capabilityName"},
		Image: ServiceImage{
			Category:   v1alpha1.ComponentSlotLoadBalancer,
			Type:       v1alpha1.InfraComponentTypeHAProxy,
			Repository: "docker.io/library/haproxy",
			Version:    "3.3.10",
			Source:     "https://hub.docker.com/_/haproxy",
			LookupDate: "2026-05-21",
		},
		Status:  StatusSupported,
		Summary: "HAProxy managed load balancer",
	},
	{Kind: v1alpha1.ComponentSlotArtifactServer, Realisation: v1alpha1.ArtifactServerProtocolHTTP}: {
		Key:              ServiceKey{Kind: v1alpha1.ComponentSlotArtifactServer, Realisation: v1alpha1.ArtifactServerProtocolHTTP},
		ApplyRole:        "bootwright.core.infra_component_artifact_server_http",
		DestroyRole:      "bootwright.core.infra_component_artifact_server_http",
		HostCapabilities: []string{v1alpha1.MachineCapabilityContainerRuntime},
		ConflictFields:   []string{"machineRef", "realisation", "applyRole", "destroyRole", "bindAddress", "tls", "listeners", "endpoints"},
		Image: ServiceImage{
			Category:   v1alpha1.ComponentSlotArtifactServer,
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
		ApplyRole:        "bootwright.core.infra_component_proxy_squid",
		DestroyRole:      "bootwright.core.infra_component_proxy_squid",
		HostCapabilities: []string{v1alpha1.MachineCapabilityContainerRuntime},
		ConflictFields:   []string{"machineRef", "realisation", "applyRole", "destroyRole", "bindAddress", "port"},
		DefaultPort:      v1alpha1.DefaultSquidPort,
		Image: ServiceImage{
			Category:   v1alpha1.ComponentSlotProxy,
			Type:       v1alpha1.InfraComponentTypeSquid,
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
		ApplyRole:         "bootwright.core.infra_component_name_resolution_dnsmasq",
		DestroyRole:       "bootwright.core.infra_component_name_resolution_dnsmasq",
		HostCapabilities:  []string{v1alpha1.MachineCapabilityContainerRuntime},
		ConflictFields:    []string{"machineRef", "realisation", "applyRole", "destroyRole", "bindAddress", "port"},
		MergeStringFields: []string{"additionalIngressHosts"},
		DefaultPort:       v1alpha1.DefaultDNSPort,
		Image: ServiceImage{
			Category:   v1alpha1.ComponentSlotNameResolution,
			Type:       v1alpha1.InfraComponentTypeDnsmasq,
			Repository: "docker.io/dockurr/dnsmasq",
			Version:    "2.92_p2",
			Source:     "https://hub.docker.com/r/dockurr/dnsmasq",
			LookupDate: "2026-05-21",
		},
		Status:  StatusSupported,
		Summary: "dnsmasq managed name resolution",
	},
	{Kind: v1alpha1.ComponentSlotNTP, Realisation: v1alpha1.InfraComponentTypeChrony}: {
		Key:            ServiceKey{Kind: v1alpha1.ComponentSlotNTP, Realisation: v1alpha1.InfraComponentTypeChrony},
		ApplyRole:      "bootwright.core.infra_component_ntp_chrony",
		DestroyRole:    "bootwright.core.infra_component_ntp_chrony",
		ConflictFields: []string{"machineRef", "realisation", "applyRole", "destroyRole", "bindAddress", "port"},
		DefaultPort:    v1alpha1.DefaultNTPPort,
		Status:         StatusSupported,
		Summary:        "chrony managed NTP service",
	},
	{Kind: v1alpha1.ComponentSlotRegistry, Realisation: v1alpha1.InfraComponentTypeMirrorRegistry}: {
		Key:              ServiceKey{Kind: v1alpha1.ComponentSlotRegistry, Realisation: v1alpha1.InfraComponentTypeMirrorRegistry},
		ApplyRole:        "bootwright.core.infra_component_registry_mirror",
		DestroyRole:      "bootwright.core.infra_component_registry_mirror",
		HostCapabilities: []string{v1alpha1.MachineCapabilityContainerRuntime},
		ConflictFields:   []string{"machineRef", "realisation", "applyRole", "destroyRole", "bindAddress", "port"},
		DefaultPort:      v1alpha1.DefaultMirrorRegistryPort,
		Image: ServiceImage{
			Category:   v1alpha1.ComponentSlotRegistry,
			Type:       v1alpha1.InfraComponentTypeMirrorRegistry,
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
		return []string{"machineRef", "realisation", "applyRole", "destroyRole"}
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

func PinnableServiceKeys() []ServiceKey {
	var out []ServiceKey
	for _, s := range serviceSupport {
		if s.Image.Type != "" {
			out = append(out, s.Key)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Realisation < out[j].Realisation
	})
	return out
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
