package render

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/artifactpub"
	"github.com/crmarques/bootwright/internal/support"
)

// componentsVars walks every component slot on a cluster and emits the
// per-component vars consumed by ansible/playbooks/layers/.
func componentsVars(state v1alpha1.State, ci v1alpha1.ClusterInfra, ocp v1alpha1.ContainerCluster) []any {
	var out []any
	clusterName := ocp.Metadata.Name

	for _, m := range ci.Spec.Components.Machines {
		out = append(out, machineComponentVars(state, ci, m, clusterName))
	}
	for _, c := range ci.Spec.Components.LoadBalancers {
		lb := loadBalancerComponentVars(state, c)
		lb["clusterName"] = clusterName
		lb["frontends"] = loadBalancerFrontends(state, ci, c.Name, clusterName, ci.Spec.Components.Machines, clusterNodesForCI(state, ci))
		out = append(out, lb)
	}
	if artifactpub.ClusterNeedsPublication(state, ci, ocp) {
		if publisher, ok := artifactpub.Select(state); ok {
			out = append(out, artifactPublisherComponentVars(state, ci, publisher))
		}
	}
	if c := ci.Spec.Components.Proxy; c != nil {
		out = append(out, proxyComponentVars(state, c))
	}
	if c := ci.Spec.Components.NameResolution; c != nil {
		out = append(out, nameResolutionComponentVars(state, c))
	}
	if c := ci.Spec.Components.Registry; c != nil {
		out = append(out, registryComponentVars(state, c))
	}
	return out
}

func endpointsVars(ci v1alpha1.ClusterInfra) []any {
	out := make([]any, 0, len(ci.Spec.Endpoints))
	for _, name := range standardEndpointNames {
		e, ok := ci.Spec.Endpoints[name]
		if !ok {
			continue
		}
		entry := map[string]any{
			"name":    name,
			"address": endpointAddress(ci, name),
		}
		if e.VIP != "" {
			entry["vip"] = e.VIP
		}
		if e.ExternalVIP != "" {
			entry["externalVip"] = e.ExternalVIP
		}
		if e.ProvidedBy != nil {
			providedBy := map[string]any{"loadBalancer": e.ProvidedBy.LoadBalancer}
			if e.ProvidedBy.Address != "" {
				providedBy["address"] = e.ProvidedBy.Address
			}
			entry["providedBy"] = providedBy
		}
		out = append(out, entry)
	}
	return out
}

func machineComponentVars(state v1alpha1.State, ci v1alpha1.ClusterInfra, m v1alpha1.ClusterMachineComponent, clusterName string) map[string]any {
	driver := ProviderDriver(state, m)
	out := map[string]any{
		"kind":          v1alpha1.ComponentSlotMachines,
		"name":          m.Name,
		"providerName":  m.From.Provider,
		"substrateRole": driver.Dispatch.SubstrateRole,
		"bmcRole":       driver.Dispatch.BMCRole,
		"bootRole":      driver.Dispatch.BootRole,
		"networkConfig": clusterMachineNetworkConfigVars(m.NetworkConfig),
	}
	applyMachineRoleContract(out, driver.Roles)
	if ip := machinePrimaryIP(m); ip != "" {
		out["primaryIPAddress"] = ip
	}
	if interfaces := machineInterfaces(state, m, clusterName); len(interfaces) > 0 {
		out["interfaces"] = machineInterfaceVars(interfaces)
	}
	if hostRef := machineHostRef(state, m); hostRef != "" {
		out["hostRef"] = hostRef
		out["hostAddress"] = lookupHostAddress(state, hostRef)
	}
	if m.From.Profile != "" {
		out["fromProfile"] = m.From.Profile
		// Inline the profile spec so Ansible roles do not need to
		// resolve back across the provider list.
		if provider, ok := findProvider(state, m.From.Provider); ok {
			if profile, ok := findProfile(provider, m.From.Profile); ok {
				out["profile"] = map[string]any{
					"name":      profile.Name,
					"cpu":       profile.CPU,
					"memoryMiB": profile.MemoryMiB,
					"diskGiB":   profile.DiskGiB,
				}
				if l := profile.Libvirt; l != nil {
					out["libvirt"] = map[string]any{
						"hostRef": l.HostRef.Name,
						"uri":     l.URI,
					}
					if be := machineEmulatedBMCVars(state, profile); be != nil && driver.Dispatch.BMCRole == "emulated" {
						out["bmcEmulated"] = be
					}
				}
				if profile.VSphere != nil {
					out["vsphere"] = machineProfileProvisionerVars(profile)
				}
				if k := profile.KubeVirt; k != nil {
					out["kubevirt"] = map[string]any{
						"clusterRef": k.ClusterRef.Name,
						"namespace":  k.Namespace,
					}
					if k.StorageClassRef != nil {
						out["kubevirt"].(map[string]any)["storageClassRef"] = k.StorageClassRef.Name
					}
				}
			}
		}
	}
	if m.From.Name != "" {
		out["fromName"] = m.From.Name
		if provider, ok := findProvider(state, m.From.Provider); ok {
			if server, ok := findProviderMachine(provider, m.From.Name); ok {
				out["server"] = map[string]any{
					"name":       server.Name,
					"interfaces": providerInterfacesVars(server),
				}
				if server.BareMetal != nil {
					out["bmc"] = map[string]any{
						"address":                        server.BareMetal.BMC.Address,
						"protocol":                       server.BareMetal.BMC.Protocol,
						"credentialsRef":                 server.BareMetal.BMC.CredentialsRef.Name,
						"disableCertificateVerification": server.BareMetal.BMC.DisableCertificateVerification,
					}
				}
			}
		}
	}
	if hints := machineRootDeviceHints(state, m); hints != nil {
		out["rootDeviceHints"] = rootDeviceHintsConfig(hints)
	}
	if clusterName != "" {
		out["clusterName"] = clusterName
	}
	if boot := machineBootVars(state, ci, m, clusterName); boot != nil {
		out["boot"] = boot
	}
	return out
}

func applyMachineRoleContract(out map[string]any, roles support.RoleContract) {
	if len(roles.HostSetupRoles) > 0 {
		out["hostSetupRoles"] = stringSliceAny(roles.HostSetupRoles)
	}
	if roles.SubstrateApplyRole != "" {
		out["substrateApplyRole"] = roles.SubstrateApplyRole
	}
	if roles.SubstrateDestroyRole != "" {
		out["substrateDestroyRole"] = roles.SubstrateDestroyRole
	}
	if roles.BMCApplyRole != "" {
		out["bmcApplyRole"] = roles.BMCApplyRole
	}
	if roles.BMCDestroyRole != "" {
		out["bmcDestroyRole"] = roles.BMCDestroyRole
	}
	if roles.BootApplyRole != "" {
		out["bootApplyRole"] = roles.BootApplyRole
	}
	if roles.MediaPrepareRole != "" {
		out["mediaPrepareRole"] = roles.MediaPrepareRole
	}
	if roles.RequiresKVM {
		out["requiresKVM"] = true
	}
}

// machineHostRef returns the provider-host this machine is anchored to,
// when the substrate has one (libvirt). vsphere/kubevirt machines have
// no provider host. from.name baremetal machines are reached over BMC
// directly from the controller; the provider host is also empty there.
func machineHostRef(state v1alpha1.State, m v1alpha1.ClusterMachineComponent) string {
	if m.From.Profile == "" {
		return ""
	}
	provider, ok := findProvider(state, m.From.Provider)
	if !ok {
		return ""
	}
	profile, ok := findProfile(provider, m.From.Profile)
	if !ok {
		return ""
	}
	if profile.Libvirt != nil {
		return profile.Libvirt.HostRef.Name
	}
	return ""
}
