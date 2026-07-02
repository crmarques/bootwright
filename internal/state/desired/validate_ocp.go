package desiredstate

import (
	"fmt"
	"net"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

func validateContainerClusters(state v1alpha1.State) []string {
	var errs []string
	machines := indexMachines(state.Machines)
	providers := indexProviders(state.InfraProviders)
	networkConfigs := indexNetworkConfigs(state.NetworkConfigs)
	components := indexInfraComponents(state.InfraComponents)
	env := primaryEnvironment(&state)
	for _, ocp := range state.ContainerClusters {
		if e := validateName(v1alpha1.KindContainerCluster, ocp.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		switch v1alpha1.InstallMode(ocp) {
		case v1alpha1.InstallModeConnected, v1alpha1.InstallModeDisconnected:
		default:
			errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.install.mode %q must be one of {%s, %s}",
				ocp.Metadata.Name, ocp.Spec.Install.Mode, v1alpha1.InstallModeConnected, v1alpha1.InstallModeDisconnected))
		}
		errs = append(errs, validateDistribution(ocp)...)
		errs = append(errs, validateClusterNetworking(ocp)...)
		if ocp.Spec.Install.Method != "" && ocp.Spec.Install.Method != v1alpha1.OCPInstallMethodAgent {
			errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.install.method %q must be %q",
				ocp.Metadata.Name, ocp.Spec.Install.Method, v1alpha1.OCPInstallMethodAgent))
		}
		errs = append(errs, validateClusterPlatformWithDerivation(state, ocp)...)
		ci, ok := stateview.ClusterInstallForContainerCluster(state, ocp)
		if ok {
			errs = append(errs, validateClusterEndpoints(fmt.Sprintf("ContainerCluster/%s spec.install", ocp.Metadata.Name), ci, components, networkConfigs, true)...)
			errs = append(errs, validateClusterArtifactAccess(fmt.Sprintf("ContainerCluster/%s spec.install", ocp.Metadata.Name), ocp.Spec.Install.ArtifactAccess, ocp.DefaultedRefs, env, components)...)
			errs = append(errs, validateMachineNetworkBindings(ci, providers, networkConfigs)...)
			errs = append(errs, validateContainerEndpointRefs(ocp, ci)...)
			errs = append(errs, validateSNOOpenShiftEndpoints(ocp, ci)...)
		}
		errs = append(errs, validateNodes(ocp, machines)...)
		errs = append(errs, validateInstallRefs(state, ocp)...)
	}
	return errs
}

// validateClusterPlatformWithDerivation wraps validateClusterPlatform with
// the spec.install.platform derivation contract: normalize materializes the
// platform from the single provider type behind the cluster's nodes, so an
// omitted platform that survives to validation means derivation could not
// pick one. When the nodes bind machines across multiple provider types,
// emit the specific conflict instead of the generic "type is required".
func validateClusterPlatformWithDerivation(state v1alpha1.State, ocp v1alpha1.ContainerCluster) []string {
	owner := fmt.Sprintf("ContainerCluster/%s spec.install.platform", ocp.Metadata.Name)
	if installPlatformOmitted(ocp.Spec.Install.Platform) && len(ocp.Spec.Hosts) > 0 {
		// An unresolved machineRef is the root cause of a non-derivable platform.
		// That machineRef error is already reported at the host level, so suppress
		// the misleading "platform.type is required" that would otherwise steer the
		// operator to author a platform they do not need.
		if clusterHasUnresolvedMachineRef(state, ocp) {
			return nil
		}
		if binding := clusterNodeProviderBinding(state, ocp); len(binding.types) > 1 {
			return []string{fmt.Sprintf("%s cannot be derived: spec.hosts bind machines across multiple provider types (%s); set spec.install.platform.type explicitly",
				owner, strings.Join(binding.providers, ", "))}
		}
	}
	return validateClusterPlatform(owner, ocp.Spec.Install.Platform, len(ocp.Spec.Hosts) > 0)
}

// clusterHasUnresolvedMachineRef reports whether any host names a Machine that is
// not in the loaded state; that dangling reference, not a missing platform, is the
// real error to surface.
func clusterHasUnresolvedMachineRef(state v1alpha1.State, ocp v1alpha1.ContainerCluster) bool {
	present := make(map[string]bool, len(state.Machines))
	for _, m := range state.Machines {
		present[m.Metadata.Name] = true
	}
	for _, host := range ocp.Spec.Hosts {
		if host.MachineRef.Name != "" && !present[host.MachineRef.Name] {
			return true
		}
	}
	return false
}

func validateClusterNetworking(ocp v1alpha1.ContainerCluster) []string {
	var errs []string
	networking := ocp.Spec.Networking
	prefix := fmt.Sprintf("ContainerCluster/%s spec.networking", ocp.Metadata.Name)
	if networking == nil {
		return []string{prefix + " is required"}
	}
	if networking.NetworkType != "" && strings.TrimSpace(networking.NetworkType) != networking.NetworkType {
		errs = append(errs, fmt.Sprintf("%s.networkType %q must not contain leading or trailing whitespace", prefix, networking.NetworkType))
	}
	if len(networking.ClusterNetwork) == 0 {
		errs = append(errs, prefix+".clusterNetwork is required")
	}
	for i, entry := range networking.ClusterNetwork {
		field := fmt.Sprintf("%s.clusterNetwork[%d]", prefix, i)
		if entry.CIDR == "" {
			errs = append(errs, field+".cidr is required")
			continue
		}
		_, ipNet, err := net.ParseCIDR(entry.CIDR)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s.cidr %q is not a valid CIDR", field, entry.CIDR))
			continue
		}
		if entry.HostPrefix == 0 {
			errs = append(errs, field+".hostPrefix is required")
			continue
		}
		ones, bits := ipNet.Mask.Size()
		if entry.HostPrefix <= ones || entry.HostPrefix > bits {
			errs = append(errs, fmt.Sprintf("%s.hostPrefix %d must be greater than CIDR prefix length %d and no larger than %d", field, entry.HostPrefix, ones, bits))
		}
	}
	if len(networking.ServiceNetwork) == 0 {
		errs = append(errs, prefix+".serviceNetwork is required")
	}
	for i, cidr := range networking.ServiceNetwork {
		field := fmt.Sprintf("%s.serviceNetwork[%d]", prefix, i)
		if cidr == "" {
			errs = append(errs, field+" is required")
			continue
		}
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			errs = append(errs, fmt.Sprintf("%s %q is not a valid CIDR", field, cidr))
		}
	}
	return errs
}

func validateDistribution(ocp v1alpha1.ContainerCluster) []string {
	var errs []string
	if image := ocp.Spec.Distribution.Release.Image; image != "" {
		if err := validatePinnedImageReference(image); err != "" {
			errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.distribution.release.image %q %s", ocp.Metadata.Name, image, err))
		}
	}
	switch v1alpha1.DistributionType(ocp) {
	case v1alpha1.DistributionOpenShift:
		if ocp.Spec.Distribution.Release.Version == "" && ocp.Spec.Distribution.Release.Image == "" {
			errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.distribution.release.version is required for openshift unless release.image is set", ocp.Metadata.Name))
		}
	case v1alpha1.DistributionOKD:
		if ocp.Spec.Distribution.Release.Version == "" && ocp.Spec.Distribution.Release.Image == "" {
			errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.distribution.release.image or version is required for okd", ocp.Metadata.Name))
		}
		if ocp.Spec.Distribution.Release.Channel != "" {
			errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.distribution.release.channel is only supported for openshift", ocp.Metadata.Name))
		}
	default:
		errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.distribution.type %q must be one of {%s, %s}",
			ocp.Metadata.Name, ocp.Spec.Distribution.Type, v1alpha1.DistributionOpenShift, v1alpha1.DistributionOKD))
	}
	return errs
}

func validateNodes(ocp v1alpha1.ContainerCluster, machines map[string]v1alpha1.Machine) []string {
	var errs []string
	if len(ocp.Spec.Hosts) == 0 {
		return []string{fmt.Sprintf("ContainerCluster/%s spec.hosts is required", ocp.Metadata.Name)}
	}
	master := 0
	worker := 0
	infra := 0
	seenHostnames := map[string]bool{}
	for i, node := range ocp.Spec.Hosts {
		prefix := fmt.Sprintf("ContainerCluster/%s spec.hosts[%d]", ocp.Metadata.Name, i)
		if node.Hostname == "" {
			errs = append(errs, fmt.Sprintf("%s.hostname is required", prefix))
		} else if seenHostnames[node.Hostname] {
			errs = append(errs, fmt.Sprintf("%s.hostname %q is duplicated", prefix, node.Hostname))
		}
		seenHostnames[node.Hostname] = true
		switch node.Role {
		case v1alpha1.NodeRoleMaster:
			master++
		case v1alpha1.NodeRoleWorker:
			worker++
		case v1alpha1.NodeRoleInfra:
			infra++
		default:
			errs = append(errs, fmt.Sprintf("%s.role %q must be master, worker, or infra", prefix, node.Role))
		}
		errs = append(errs, validateNodePlacement(prefix, node)...)
		if node.MachineRef.Name == "" {
			errs = append(errs, fmt.Sprintf("%s.machineRef is required", prefix))
			continue
		}
		machine, ok := machines[node.MachineRef.Name]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s.machineRef %q does not match any Machine", prefix, node.MachineRef.Name))
		} else if !machineHasCapability(machine, v1alpha1.MachineCapabilityOpenShiftNode) {
			errs = append(errs, fmt.Sprintf("%s.machineRef %q lacks capability %q", prefix, node.MachineRef.Name, v1alpha1.MachineCapabilityOpenShiftNode))
		}
	}
	if master == 0 {
		errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.hosts requires at least one master", ocp.Metadata.Name))
	}
	if ocp.Spec.ControlPlane != nil && ocp.Spec.ControlPlane.Replicas != 0 && ocp.Spec.ControlPlane.Replicas != master {
		errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.controlPlane.replicas %d does not match master node count %d",
			ocp.Metadata.Name, ocp.Spec.ControlPlane.Replicas, master))
	}
	workerReplicas := 0
	for _, pool := range ocp.Spec.Compute {
		workerReplicas += pool.Replicas
	}
	// Infra hosts install in the worker pool, so the compute replica count
	// covers workers and infra together.
	if computeNodes := worker + infra; len(ocp.Spec.Compute) > 0 && workerReplicas != computeNodes {
		errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.compute worker replicas %d does not match worker+infra node count %d",
			ocp.Metadata.Name, workerReplicas, computeNodes))
	}
	return errs
}

// validateNodePlacement checks the optional day-2 node labels and taints: label
// keys must be non-empty and taint effects must be a valid Kubernetes effect.
func validateNodePlacement(prefix string, node v1alpha1.OCPHostSpec) []string {
	var errs []string
	for key := range node.Labels {
		if strings.TrimSpace(key) == "" {
			errs = append(errs, prefix+".labels contains an empty key")
		}
	}
	for j, taint := range node.Taints {
		owner := fmt.Sprintf("%s.taints[%d]", prefix, j)
		if strings.TrimSpace(taint.Key) == "" {
			errs = append(errs, owner+".key is required")
		}
		switch taint.Effect {
		case v1alpha1.TaintEffectNoSchedule, v1alpha1.TaintEffectPreferNoSchedule, v1alpha1.TaintEffectNoExecute:
		default:
			errs = append(errs, fmt.Sprintf("%s.effect %q must be one of {%s, %s, %s}", owner, taint.Effect,
				v1alpha1.TaintEffectNoSchedule, v1alpha1.TaintEffectPreferNoSchedule, v1alpha1.TaintEffectNoExecute))
		}
	}
	return errs
}

func validateSNOOpenShiftEndpoints(ocp v1alpha1.ContainerCluster, ci v1alpha1.ClusterInstall) []string {
	if !stateview.IsSingleNodeCluster(ocp) {
		return nil
	}
	var errs []string
	for _, role := range []string{v1alpha1.EndpointAPI, v1alpha1.EndpointAPIInt, v1alpha1.EndpointIngress} {
		refName := containerEndpointRefName(role)
		endpoint, ok := ci.Endpoints[refName]
		if ok && endpoint.Source.Type == v1alpha1.EndpointSourceOpenShift {
			errs = append(errs, fmt.Sprintf("ContainerCluster/%s single-node clusters forbid spec.install.endpoints.%s source.type=openshift",
				ocp.Metadata.Name, refName))
		}
	}
	return errs
}

func validateContainerEndpointRefs(ocp v1alpha1.ContainerCluster, ci v1alpha1.ClusterInstall) []string {
	var errs []string
	for _, role := range []string{v1alpha1.EndpointAPI, v1alpha1.EndpointAPIInt, v1alpha1.EndpointIngress} {
		refName := containerEndpointRefName(role)
		prefix := fmt.Sprintf("ContainerCluster/%s spec.install.endpoints.%s", ocp.Metadata.Name, role)
		if refName == "" {
			errs = append(errs, prefix+" is required")
			continue
		}
		endpoint, ok := ci.Endpoints[refName]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s is required", prefix))
			continue
		}
		source := endpoint.Source.Type
		switch source {
		case v1alpha1.EndpointSourceOpenShift, v1alpha1.EndpointSourceExternal:
			if endpoint.Address == "" {
				errs = append(errs, fmt.Sprintf("%s.address is required for %s endpoint", prefix, role))
			}
		case v1alpha1.EndpointSourceInfraComponent:
		default:
			errs = append(errs, fmt.Sprintf("%s.name %q references endpoint source.type %q; container endpoints require one of {%s, %s, %s}",
				prefix, refName, source,
				v1alpha1.EndpointSourceOpenShift, v1alpha1.EndpointSourceExternal, v1alpha1.EndpointSourceInfraComponent))
		}
	}
	return errs
}

func containerEndpointRefName(role string) string {
	switch role {
	case v1alpha1.EndpointAPI:
		return role
	case v1alpha1.EndpointAPIInt:
		return role
	case v1alpha1.EndpointIngress:
		return role
	default:
		return ""
	}
}

func validateInstallRefs(state v1alpha1.State, ocp v1alpha1.ContainerCluster) []string {
	var errs []string
	// Normalize fills pullSecretRef for openshift clusters whenever an
	// Environment is loaded (Environment default, else the
	// openshift-pull-secret convention), so an empty ref here means the state
	// has no Environment — already its own validation failure. Keep the guard
	// for direct Validate callers, without an inheritance hint that can never
	// apply on this path.
	if v1alpha1.DistributionType(ocp) == v1alpha1.DistributionOpenShift && ocp.Spec.Install.PullSecretRef.Name == "" {
		errs = append(errs, fmt.Sprintf("ContainerCluster/%s install.pullSecretRef is required for openshift", ocp.Metadata.Name))
	}
	errs = append(errs, validateNodeSSHSpec(
		fmt.Sprintf("ContainerCluster/%s spec.install.nodeSSH", ocp.Metadata.Name),
		ocp.Spec.Install.NodeSSH,
		true,
	)...)
	errs = append(errs, validateAdditionalTrustBundleRefs(ocp)...)
	errs = append(errs, validateServingCertificateRefs(state, ocp)...)
	return errs
}

func validateAdditionalTrustBundleRefs(ocp v1alpha1.ContainerCluster) []string {
	var errs []string
	seen := map[string]bool{}
	owner := fmt.Sprintf("ContainerCluster/%s spec.install.additionalTrustBundleRefs", ocp.Metadata.Name)
	for i, ref := range ocp.Spec.Install.AdditionalTrustBundleRefs {
		if ref.Name == "" {
			errs = append(errs, fmt.Sprintf("%s[%d] is required", owner, i))
			continue
		}
		if seen[ref.Name] {
			errs = append(errs, fmt.Sprintf("%s[%d] %q is duplicated", owner, i, ref.Name))
			continue
		}
		seen[ref.Name] = true
	}
	return errs
}

func validateServingCertificateRefs(state v1alpha1.State, ocp v1alpha1.ContainerCluster) []string {
	serving := ocp.Spec.Install.ServingCertificates
	if serving == nil {
		return nil
	}
	var errs []string
	baseDomain := ""
	if env := primaryEnvironment(&state); env != nil {
		baseDomain = env.Spec.BaseDomain
	}
	apiIntName := "api-int." + ocp.Metadata.Name + "." + baseDomain
	if api := serving.APIServer; api != nil {
		if len(api.NamedCertificates) == 0 {
			errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.install.servingCertificates.apiServer.namedCertificates requires at least one entry", ocp.Metadata.Name))
		}
		for i, cert := range api.NamedCertificates {
			owner := fmt.Sprintf("ContainerCluster/%s spec.install.servingCertificates.apiServer.namedCertificates[%d]", ocp.Metadata.Name, i)
			if cert.SecretRef.Name == "" {
				errs = append(errs, owner+".secretRef is required")
			}
			if len(cert.Names) == 0 {
				errs = append(errs, owner+".names requires at least one DNS name")
			}
			seenNames := map[string]bool{}
			for j, name := range cert.Names {
				field := fmt.Sprintf("%s.names[%d]", owner, j)
				if strings.TrimSpace(name) != name || name == "" {
					errs = append(errs, fmt.Sprintf("%s must not be empty or contain leading/trailing whitespace", field))
					continue
				}
				if baseDomain != "" && strings.EqualFold(name, apiIntName) {
					errs = append(errs, fmt.Sprintf("%s %q must not target the internal API endpoint", field, name))
				}
				if seenNames[name] {
					errs = append(errs, fmt.Sprintf("%s %q is duplicated", field, name))
					continue
				}
				seenNames[name] = true
			}
		}
	}
	if ingress := serving.Ingress; ingress != nil && ingress.DefaultCertificateRef.Name == "" {
		errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.install.servingCertificates.ingress.defaultCertificateRef is required", ocp.Metadata.Name))
	}
	return errs
}
