package desiredstate

import (
	"fmt"
	"net"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateContainerClusters(state v1alpha1.State) []string {
	var errs []string
	infraIndex := indexClusterInfras(state.ClusterInfras)
	seen := map[string]bool{}
	for _, ocp := range state.ContainerClusters {
		if e := validateName(v1alpha1.KindContainerCluster, ocp.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[ocp.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate ContainerCluster %q", ocp.Metadata.Name))
		}
		seen[ocp.Metadata.Name] = true
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
		ci, ok, nodeErrs := resolveContainerClusterInfra(ocp, infraIndex)
		errs = append(errs, nodeErrs...)
		if ok {
			errs = append(errs, validateNodes(ocp, ci)...)
			errs = append(errs, validateContainerEndpointRefs(ocp, ci)...)
			errs = append(errs, validateSNOOpenShiftEndpoints(ocp, ci)...)
		}
		errs = append(errs, validateInstallRefs(state, ocp)...)
	}
	return errs
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

func resolveContainerClusterInfra(ocp v1alpha1.ContainerCluster, infraIndex map[string]v1alpha1.ClusterInfra) (v1alpha1.ClusterInfra, bool, []string) {
	var errs []string
	if len(ocp.Spec.Nodes) == 0 {
		return v1alpha1.ClusterInfra{}, false, []string{fmt.Sprintf("ContainerCluster/%s spec.nodes is required", ocp.Metadata.Name)}
	}
	infraName := ""
	for i, node := range ocp.Spec.Nodes {
		prefix := fmt.Sprintf("ContainerCluster/%s spec.nodes[%d]", ocp.Metadata.Name, i)
		if node.InfraNodeRef.ClusterInfra == "" {
			errs = append(errs, fmt.Sprintf("%s.infraNodeRef.clusterInfra is required", prefix))
			continue
		}
		if infraName == "" {
			infraName = node.InfraNodeRef.ClusterInfra
		} else if node.InfraNodeRef.ClusterInfra != infraName {
			errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.nodes must reference exactly one ClusterInfra in v1 (got %q and %q)",
				ocp.Metadata.Name, infraName, node.InfraNodeRef.ClusterInfra))
		}
	}
	if infraName == "" {
		return v1alpha1.ClusterInfra{}, false, errs
	}
	ci, ok := infraIndex[infraName]
	if !ok {
		errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.nodes[].infraNodeRef.clusterInfra %q does not match any ClusterInfra",
			ocp.Metadata.Name, infraName))
		return v1alpha1.ClusterInfra{}, false, errs
	}
	return ci, true, errs
}

func validateNodes(ocp v1alpha1.ContainerCluster, ci v1alpha1.ClusterInfra) []string {
	var errs []string
	nodes := map[string]v1alpha1.ClusterNodeComponent{}
	for _, m := range ci.Spec.Components.Nodes {
		nodes[m.Name] = m
	}
	master := 0
	worker := 0
	seenHostnames := map[string]bool{}
	for i, node := range ocp.Spec.Nodes {
		prefix := fmt.Sprintf("ContainerCluster/%s spec.nodes[%d]", ocp.Metadata.Name, i)
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
		default:
			errs = append(errs, fmt.Sprintf("%s.role %q must be master or worker", prefix, node.Role))
		}
		if node.InfraNodeRef.Name == "" {
			errs = append(errs, fmt.Sprintf("%s.infraNodeRef.name is required", prefix))
			continue
		}
		infraNode, ok := nodes[node.InfraNodeRef.Name]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s.infraNodeRef.name %q is not defined on ClusterInfra/%s spec.components.nodes",
				prefix, node.InfraNodeRef.Name, ci.Metadata.Name))
		} else if infraNode.Source.ProviderRef.Name == "" {
			errs = append(errs, fmt.Sprintf("%s.infraNodeRef.name %q must reference a provider-sourced ClusterInfra node",
				prefix, node.InfraNodeRef.Name))
		}
	}
	if master == 0 {
		errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.nodes requires at least one master", ocp.Metadata.Name))
	}
	if ocp.Spec.ControlPlane != nil && ocp.Spec.ControlPlane.Replicas != 0 && ocp.Spec.ControlPlane.Replicas != master {
		errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.controlPlane.replicas %d does not match master node count %d",
			ocp.Metadata.Name, ocp.Spec.ControlPlane.Replicas, master))
	}
	workerReplicas := 0
	for _, pool := range ocp.Spec.Compute {
		if pool.Name == "" || pool.Name == "worker" {
			workerReplicas += pool.Replicas
		}
	}
	if len(ocp.Spec.Compute) > 0 && workerReplicas != worker {
		errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.compute worker replicas %d does not match worker node count %d",
			ocp.Metadata.Name, workerReplicas, worker))
	}
	return errs
}

func validateSNOOpenShiftEndpoints(ocp v1alpha1.ContainerCluster, ci v1alpha1.ClusterInfra) []string {
	if !isSingleNodeCluster(ocp) {
		return nil
	}
	var errs []string
	for _, role := range []string{v1alpha1.EndpointAPI, v1alpha1.EndpointAPIInt, v1alpha1.EndpointIngress} {
		refName := containerEndpointRefName(ocp, role)
		endpoint, ok := ci.Spec.Endpoints[refName]
		if ok && effectiveEndpointSource(endpoint, v1alpha1.EndpointSourceOpenShift) == v1alpha1.EndpointSourceOpenShift {
			errs = append(errs, fmt.Sprintf("ContainerCluster/%s single-node clusters forbid ClusterInfra/%s spec.endpoints.%s source.type=openshift",
				ocp.Metadata.Name, ci.Metadata.Name, refName))
		}
	}
	return errs
}

func validateContainerEndpointRefs(ocp v1alpha1.ContainerCluster, ci v1alpha1.ClusterInfra) []string {
	var errs []string
	for _, role := range []string{v1alpha1.EndpointAPI, v1alpha1.EndpointAPIInt, v1alpha1.EndpointIngress} {
		refName := containerEndpointRefName(ocp, role)
		prefix := fmt.Sprintf("ContainerCluster/%s spec.install.endpointRefs.%s", ocp.Metadata.Name, role)
		if refName == "" {
			errs = append(errs, prefix+".name is required")
			continue
		}
		endpoint, ok := ci.Spec.Endpoints[refName]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s.name %q does not match any ClusterInfra/%s spec.endpoints entry", prefix, refName, ci.Metadata.Name))
			continue
		}
		source := effectiveEndpointSource(endpoint, v1alpha1.EndpointSourceOpenShift)
		switch source {
		case v1alpha1.EndpointSourceOpenShift, v1alpha1.EndpointSourceExternal:
			if endpoint.Address == "" {
				errs = append(errs, fmt.Sprintf("ClusterInfra/%s spec.endpoints.%s.address is required for ContainerCluster/%s %s endpoint",
					ci.Metadata.Name, refName, ocp.Metadata.Name, role))
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

func containerEndpointRefName(ocp v1alpha1.ContainerCluster, role string) string {
	switch role {
	case v1alpha1.EndpointAPI:
		return ocp.Spec.Install.EndpointRefs.API.Name
	case v1alpha1.EndpointAPIInt:
		return ocp.Spec.Install.EndpointRefs.APIInt.Name
	case v1alpha1.EndpointIngress:
		return ocp.Spec.Install.EndpointRefs.Ingress.Name
	default:
		return ""
	}
}

func effectiveEndpointSource(endpoint v1alpha1.Endpoint, defaultSource string) string {
	if endpoint.Source.Type != "" {
		return endpoint.Source.Type
	}
	return defaultSource
}

func isSingleNodeCluster(ocp v1alpha1.ContainerCluster) bool {
	if len(ocp.Spec.Nodes) != 1 {
		return false
	}
	return ocp.Spec.Nodes[0].Role == v1alpha1.NodeRoleMaster
}

func validateInstallRefs(state v1alpha1.State, ocp v1alpha1.ContainerCluster) []string {
	var errs []string
	if v1alpha1.DistributionType(ocp) == v1alpha1.DistributionOpenShift && ocp.Spec.Install.PullSecretRef.Name == "" {
		errs = append(errs, fmt.Sprintf("ContainerCluster/%s install.pullSecretRef.name is required for openshift (inheritable from Environment)", ocp.Metadata.Name))
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
			errs = append(errs, fmt.Sprintf("%s[%d].name is required", owner, i))
			continue
		}
		if seen[ref.Name] {
			errs = append(errs, fmt.Sprintf("%s[%d].name %q is duplicated", owner, i, ref.Name))
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
				errs = append(errs, owner+".secretRef.name is required")
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
		errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.install.servingCertificates.ingress.defaultCertificateRef.name is required", ocp.Metadata.Name))
	}
	return errs
}
