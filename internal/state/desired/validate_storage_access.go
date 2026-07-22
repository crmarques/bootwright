package desiredstate

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

var posixUsername = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

func validateStorageCephadmSSHPosture(prefix string, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine, state v1alpha1.State) []string {
	var errs []string
	user := v1alpha1.StorageClusterCephadmSSHUser(cluster)
	revoking := v1alpha1.StorageClusterRevokesRootLogin(cluster, state)
	if user != v1alpha1.RootSSHUser && !posixUsername.MatchString(user) {
		errs = append(errs, fmt.Sprintf("%s.clusterSSH.user %q is not a valid POSIX user name (lowercase letter or underscore, then lowercase letters, digits, underscore or dash, at most 32 chars)", prefix, user))
	}
	if revoking && user == v1alpha1.RootSSHUser {
		errs = append(errs, fmt.Sprintf("%s.clusterSSH.user resolves to %q while a storage node Machine sets spec.access.rootLogin %q; cephadm would orchestrate over an account whose login is being revoked. Set %s.clusterSSH.user (for example %q), or set the Machine back to %q", prefix, v1alpha1.RootSSHUser, v1alpha1.MachineRootLoginRevoke, prefix, v1alpha1.StorageCephadmDefaultSSHUser, v1alpha1.MachineRootLoginKeep))
	}
	if revoking && cluster.Spec.Ceph.Cephadm.ClusterSSH.KeyRef.Name == "" {
		errs = append(errs, fmt.Sprintf("%s.clusterSSH.keyRef is required when a storage node Machine sets spec.access.rootLogin %q; without it cephadm's cluster identity is the node Machine access key, so the key Bootwright drives the node with would also open the passwordless-sudo %q account. Declare an sshKeyPair Secret (spec.source.generated) and name it here", prefix, v1alpha1.MachineRootLoginRevoke, user))
	}
	clusterKey := cluster.Spec.Ceph.Cephadm.ClusterSSH.KeyRef.Name
	for i, node := range cluster.Spec.Ceph.Topology.Nodes {
		machine, ok := machines[node.MachineRef.Name]
		if !ok || !v1alpha1.MachineRevokesRootLogin(machine) {
			continue
		}
		if machine.Spec.Access.SSH != nil && clusterKey != "" && machine.Spec.Access.SSH.KeyRef.Name == clusterKey {
			errs = append(errs, fmt.Sprintf("%s.clusterSSH.keyRef %q is also Machine/%s spec.access.ssh.keyRef; the cephadm cluster identity would be the same key Bootwright drives the node with, so revoking root login moves that key into the Ceph mon config-key store and it opens the passwordless-sudo %q account. Declare a second sshKeyPair Secret (spec.source.generated) for the cluster identity", prefix, clusterKey, machine.Metadata.Name, user))
			break
		}
		if v1alpha1.MachineSSHUser(machine) == user {
			errs = append(errs, fmt.Sprintf("%s.topology.nodes[%d].machineRef %q resolves to Machine/%s whose spec.access.ssh.user is already %q while spec.access.rootLogin is %q; the install-window identity and the post-install account must differ, otherwise the account Bootwright provisions is the one it already connects with. Leave spec.access.ssh.user as the pre-existing identity", strings.TrimSuffix(prefix, ".cephadm"), i, node.MachineRef.Name, machine.Metadata.Name, user, v1alpha1.MachineRootLoginRevoke))
		}
	}
	return errs
}
