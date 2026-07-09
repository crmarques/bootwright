package secret

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func ConsumedAsTLS(name string, state v1alpha1.State) bool {
	for _, cluster := range state.ContainerClusters {
		serving := cluster.Spec.Install.ServingCertificates
		if serving == nil {
			continue
		}
		if api := serving.APIServer; api != nil {
			for _, cert := range api.NamedCertificates {
				if cert.SecretRef.Name == name {
					return true
				}
			}
		}
		if ingress := serving.Ingress; ingress != nil && ingress.DefaultCertificateRef.Name == name {
			return true
		}
	}
	return false
}

func ConsumedAsClusterSSH(name string, state v1alpha1.State) bool {
	return ConsumedAsClusterSSHPublic(name, state) || ConsumedAsClusterSSHPrivate(name, state)
}

func ConsumedAsClusterSSHPublic(name string, state v1alpha1.State) bool {
	for _, cluster := range state.ContainerClusters {
		ssh := cluster.Spec.Install.NodeSSH
		if ssh.KeyPairRef.Name == name || ssh.PublicKeyRef.Name == name {
			return true
		}
	}
	return false
}

func ConsumedAsClusterSSHPrivate(name string, state v1alpha1.State) bool {
	for _, cluster := range state.ContainerClusters {
		ssh := cluster.Spec.Install.NodeSSH
		if ssh.KeyPairRef.Name == name || ssh.PrivateKeyRef.Name == name {
			return true
		}
	}
	return false
}

func ConsumedAsStorageSSH(name string, state v1alpha1.State) bool {
	return ConsumedAsStorageSSHPublic(name, state) || ConsumedAsStorageSSHPrivate(name, state)
}

func ConsumedAsStorageSSHPublic(name string, state v1alpha1.State) bool {
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		if cluster.Spec.Ceph.Cephadm.ClusterSSHKeyRef.Name == name {
			return true
		}
		for _, node := range cluster.Spec.Ceph.Topology.Hosts {
			machine, ok := topology.NodeMachine(state, cluster, node.Hostname)
			if ok && machine.Spec.Access.SSH != nil && machine.Spec.Access.SSH.KeyRef.Name == name {
				return true
			}
		}
	}
	return false
}

func ConsumedAsStorageSSHPrivate(name string, state v1alpha1.State) bool {
	return ConsumedAsStorageSSHPublic(name, state)
}

func ConsumedAsHostSSH(name string, state v1alpha1.State) bool {
	for _, machine := range state.Machines {
		if machine.Spec.Access.SSH != nil && machine.Spec.Access.SSH.KeyRef.Name == name {
			return true
		}
	}
	return false
}
