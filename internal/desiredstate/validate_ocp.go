package desiredstate

import (
	"fmt"

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
		if ocp.Spec.Install.Method != "" && ocp.Spec.Install.Method != v1alpha1.OCPInstallMethodAgent {
			errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.install.method %q must be %q",
				ocp.Metadata.Name, ocp.Spec.Install.Method, v1alpha1.OCPInstallMethodAgent))
		}
		ci, ok, nodeErrs := resolveContainerClusterInfra(ocp, infraIndex)
		errs = append(errs, nodeErrs...)
		if ok {
			errs = append(errs, validateNodes(ocp, ci)...)
			errs = append(errs, validateSNOOpenShiftEndpoints(ocp, ci)...)
		}
		errs = append(errs, validateInstallRefs(ocp)...)
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
		if node.MachineRef.ClusterInfra == "" {
			errs = append(errs, fmt.Sprintf("%s.machineRef.clusterInfra is required", prefix))
			continue
		}
		if infraName == "" {
			infraName = node.MachineRef.ClusterInfra
		} else if node.MachineRef.ClusterInfra != infraName {
			errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.nodes must reference exactly one ClusterInfra in v1 (got %q and %q)",
				ocp.Metadata.Name, infraName, node.MachineRef.ClusterInfra))
		}
	}
	if infraName == "" {
		return v1alpha1.ClusterInfra{}, false, errs
	}
	ci, ok := infraIndex[infraName]
	if !ok {
		errs = append(errs, fmt.Sprintf("ContainerCluster/%s spec.nodes[].machineRef.clusterInfra %q does not match any ClusterInfra",
			ocp.Metadata.Name, infraName))
		return v1alpha1.ClusterInfra{}, false, errs
	}
	return ci, true, errs
}

func validateNodes(ocp v1alpha1.ContainerCluster, ci v1alpha1.ClusterInfra) []string {
	var errs []string
	machines := map[string]bool{}
	for _, m := range ci.Spec.Components.Machines {
		machines[m.Name] = true
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
		if node.MachineRef.Name == "" {
			errs = append(errs, fmt.Sprintf("%s.machineRef.name is required", prefix))
			continue
		}
		if !machines[node.MachineRef.Name] {
			errs = append(errs, fmt.Sprintf("%s.machineRef.name %q is not defined on ClusterInfra/%s spec.components.machines",
				prefix, node.MachineRef.Name, ci.Metadata.Name))
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
	for name, endpoint := range ci.Spec.Endpoints {
		if endpoint.VIP != "" {
			errs = append(errs, fmt.Sprintf("ContainerCluster/%s single-node clusters forbid ClusterInfra/%s spec.endpoints.%s.vip",
				ocp.Metadata.Name, ci.Metadata.Name, name))
			return errs
		}
	}
	return errs
}

func isSingleNodeCluster(ocp v1alpha1.ContainerCluster) bool {
	if len(ocp.Spec.Nodes) != 1 {
		return false
	}
	return ocp.Spec.Nodes[0].Role == v1alpha1.NodeRoleMaster
}

func validateInstallRefs(ocp v1alpha1.ContainerCluster) []string {
	var errs []string
	if v1alpha1.DistributionType(ocp) == v1alpha1.DistributionOpenShift && ocp.Spec.Install.PullSecretRef.Name == "" {
		errs = append(errs, fmt.Sprintf("ContainerCluster/%s install.pullSecretRef.name is required for openshift (inheritable from Environment)", ocp.Metadata.Name))
	}
	if ocp.Spec.Install.SSHKeyRef.Name == "" {
		errs = append(errs, fmt.Sprintf("ContainerCluster/%s install.sshKeyRef.name is required (inheritable from Environment)", ocp.Metadata.Name))
	}
	return errs
}
