package cli

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

type sshIdentity struct {
	User   string
	KeyRef v1alpha1.SecretRef
	Origin string
}

func machineDeclaredIdentity(machine v1alpha1.Machine) sshIdentity {
	return sshIdentity{
		User:   v1alpha1.MachineSSHUser(machine),
		KeyRef: v1alpha1.MachineSSHKeyRef(machine),
		Origin: "Machine/" + machine.Metadata.Name + " spec.access.ssh",
	}
}

func machineAccessIdentity(state v1alpha1.State, machine v1alpha1.Machine) sshIdentity {
	declared := machineDeclaredIdentity(machine)
	if !machineLoginRevoked(machine) {
		return declared
	}
	for _, cluster := range machineStorageClusters(state, machine.Metadata.Name) {
		if replacement, ok := storageClusterIdentity(cluster); ok {
			return replacement
		}
	}
	return declared
}

func machineLoginRevoked(machine v1alpha1.Machine) bool {
	return v1alpha1.MachineRevokesRootLogin(machine) && v1alpha1.MachineSSHUser(machine) == v1alpha1.RootSSHUser
}

func storageClusterIdentity(cluster v1alpha1.StorageCluster) (sshIdentity, bool) {
	if !v1alpha1.StorageClusterManagesNodeAccount(cluster) {
		return sshIdentity{}, false
	}
	return sshIdentity{
		User:   v1alpha1.StorageClusterCephadmSSHUser(cluster),
		KeyRef: v1alpha1.SecretRef{Name: cluster.Spec.Ceph.Cephadm.ClusterSSH.KeyRef.Name},
		Origin: "StorageCluster/" + cluster.Metadata.Name + " spec.ceph.cephadm.clusterSSH",
	}, true
}

func machineStorageClusters(state v1alpha1.State, machineName string) []v1alpha1.StorageCluster {
	var out []v1alpha1.StorageCluster
	for _, cluster := range state.StorageClusters {
		if !v1alpha1.StorageClusterManaged(cluster) || cluster.Spec.Ceph == nil {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			if node.MachineRef.Name == machineName {
				out = append(out, cluster)
				break
			}
		}
	}
	return out
}

func machineNamedIdentities(state v1alpha1.State, machineName, user string) []sshIdentity {
	var out []sshIdentity
	if machine, ok := stateview.Machine(state, machineName); ok {
		if declared := machineDeclaredIdentity(machine); declared.User == user {
			out = append(out, declared)
		}
	}
	for _, cluster := range machineStorageClusters(state, machineName) {
		if identity, ok := storageClusterIdentity(cluster); ok && identity.User == user {
			out = append(out, identity)
		}
	}
	return distinctCredentials(out)
}

func distinctCredentials(identities []sshIdentity) []sshIdentity {
	var out []sshIdentity
	for _, identity := range identities {
		seen := false
		for _, kept := range out {
			if kept.KeyRef.Name == identity.KeyRef.Name {
				seen = true
				break
			}
		}
		if !seen {
			out = append(out, identity)
		}
	}
	return out
}

func applySSHUser(state v1alpha1.State, machineName string, target sshTarget, user string) (sshTarget, error) {
	if user == "" || user == target.User {
		return target, nil
	}
	matches := machineNamedIdentities(state, machineName, user)
	if len(matches) > 1 {
		return sshTarget{}, ambiguousSSHUserError(machineName, user, matches)
	}
	if len(matches) == 1 {
		target.User = matches[0].User
		target.KeyRef = matches[0].KeyRef
		return target, nil
	}
	target.User = user
	target.KeyRef = v1alpha1.SecretRef{}
	return target, nil
}

func ambiguousSSHUserError(machineName, user string, matches []sshIdentity) error {
	origins := make([]string, 0, len(matches))
	for _, match := range matches {
		origins = append(origins, fmt.Sprintf("%s (Secret %q)", match.Origin, match.KeyRef.Name))
	}
	return fmt.Errorf("--ssh-user %q names an account Machine %s carries for more than one owner, each with its own credential: %s. Bootwright will not guess which credential opens it. Reach the account through the object that owns it with 'bootwright cluster rsh --name <cluster> --node %s', or pass your own key with --ssh-preferred-id-key", user, machineName, strings.Join(origins, "; "), machineName)
}
