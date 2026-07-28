package desiredstate

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateStorageCephadmSSHPosture(prefix string, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine, state v1alpha1.State) []string {
	var errs []string
	user := v1alpha1.StorageClusterCephadmSSHUser(cluster)
	revoking := v1alpha1.StorageClusterRevokesRootLogin(cluster, state)
	if user != v1alpha1.RootSSHUser && !v1alpha1.ValidPOSIXUserName(user) {
		errs = append(errs, fmt.Sprintf("%s.clusterSSH.user %q is not a valid POSIX user name (lowercase letter or underscore, then lowercase letters, digits, underscore or dash, at most 32 chars)", prefix, user))
	}
	if revoking && user == v1alpha1.RootSSHUser {
		errs = append(errs, fmt.Sprintf("%s.clusterSSH.user resolves to %q while a storage node Machine sets spec.access.rootLogin %q; cephadm would orchestrate over an account whose login is being revoked. Set %s.clusterSSH.user (for example %q), or set the Machine back to %q", prefix, v1alpha1.RootSSHUser, v1alpha1.MachineRootLoginRevoke, prefix, v1alpha1.StorageCephadmDefaultSSHUser, v1alpha1.MachineRootLoginKeep))
	}
	if !v1alpha1.StorageClusterManagesNodeAccount(cluster) {
		for i, node := range cluster.Spec.Ceph.Topology.Nodes {
			machine, ok := machines[node.MachineRef.Name]
			if !ok || v1alpha1.MachineUsesOperatorIdentity(machine) {
				continue
			}
			if v1alpha1.MachineInstallsOS(machine) {
				errs = append(errs, fmt.Sprintf("%s.clusterSSH.user resolves to %q while %s.topology.nodes[%d].machineRef %q resolves to Machine/%s, whose OS Bootwright installs; such a node never accepts a root login, so cephadm could not orchestrate it. Set %s.clusterSSH.user (for example %q)", prefix, v1alpha1.RootSSHUser, strings.TrimSuffix(prefix, ".cephadm"), i, node.MachineRef.Name, machine.Metadata.Name, prefix, v1alpha1.StorageCephadmDefaultSSHUser))
				break
			}
			if nodeUser := v1alpha1.MachineSSHUser(machine); nodeUser != v1alpha1.RootSSHUser {
				errs = append(errs, fmt.Sprintf("%s.clusterSSH.user resolves to %q while %s.topology.nodes[%d].machineRef %q resolves to Machine/%s whose spec.access.ssh.user is %q; cephadm would orchestrate every host over an account that machine does not carry. Set %s.clusterSSH.user to %q so the orchestration identity is the account the node actually carries", prefix, v1alpha1.RootSSHUser, strings.TrimSuffix(prefix, ".cephadm"), i, node.MachineRef.Name, machine.Metadata.Name, nodeUser, prefix, nodeUser))
				break
			}
		}
		return errs
	}
	clusterKey := cluster.Spec.Ceph.Cephadm.ClusterSSH.KeyRef.Name
	if clusterKey == "" {
		errs = append(errs, fmt.Sprintf("%s.clusterSSH.keyRef is required when %s.clusterSSH.user is %q rather than %q; it is the key cephadm orchestrates every host with, and the key the cluster authorizes for that account. Declare an sshKeyPair Secret (spec.source.generated) and name it here", prefix, prefix, user, v1alpha1.RootSSHUser))
		return errs
	}
	if fleetKey := machineAccessKeyRef(state).Name; fleetKey != "" && fleetKey == clusterKey {
		errs = append(errs, fmt.Sprintf("%s.clusterSSH.keyRef %q is also Environment spec.machineAccess.keyRef; that key opens the %q service account on every Machine Bootwright installs, and cephadm bootstrap moves the cluster identity into the Ceph mon config-key store where this cluster's manager can read it. Declare a second sshKeyPair Secret (spec.source.generated) for the cluster identity", prefix, clusterKey, v1alpha1.BootwrightSSHUser))
	}
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		machine, ok := machines[node.MachineRef.Name]
		if !ok || machine.DefaultedRefs.Access {
			continue
		}
		if v1alpha1.MachineSSHKeyRef(machine).Name == clusterKey && !v1alpha1.MachineProvisionsLogin(machine) {
			errs = append(errs, fmt.Sprintf("%s.clusterSSH.keyRef %q is also Machine/%s %s; that key already opens an account Bootwright drives the machine with outside this cluster, and cephadm bootstrap moves the cluster identity into the Ceph mon config-key store. Declare a second sshKeyPair Secret (spec.source.generated) for the cluster identity", prefix, clusterKey, machine.Metadata.Name, machineSSHKeyField(machine)))
			break
		}
	}
	return errs
}
