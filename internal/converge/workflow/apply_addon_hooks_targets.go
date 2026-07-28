package workflow

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/hooks"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

type hookTargetMachine struct {
	label           string
	machine         v1alpha1.Machine
	sshUser         string
	sshKeyRef       v1alpha1.SecretRef
	sshPasswordRef  v1alpha1.SecretRef
	sudoPasswordRef v1alpha1.SecretRef
}

func (e *addonHookExecutor) resolveHookTargetMachines(hook v1alpha1.ClusterAddonStep) ([]hookTargetMachine, error) {
	target := hook.Target
	var out []hookTargetMachine
	add := func(more []hookTargetMachine) { out = append(out, more...) }

	if target.BoundCluster != nil {
		machines, err := e.containerClusterMachines(e.plan.Cluster)
		if err != nil {
			return nil, err
		}
		add(machines)
	}
	if target.Static != nil {
		for _, name := range target.Static.Clusters {
			machines, err := e.clusterMachines(name)
			if err != nil {
				return nil, err
			}
			add(machines)
		}
		for _, name := range target.Static.Machines {
			machine, ok := stateview.Machine(e.state, name)
			if !ok {
				return nil, fmt.Errorf("hook %s target machine %q not found", hook.Name, name)
			}
			add([]hookTargetMachine{machineHookTarget("Machine/"+name, machine)})
		}
	}
	if target.FromInput != nil {
		machines, err := e.fromInputMachines(hook, *target.FromInput)
		if err != nil {
			return nil, err
		}
		add(machines)
	}
	return dedupeTargetMachines(out), nil
}

func (e *addonHookExecutor) fromInputMachines(hook v1alpha1.ClusterAddonStep, from v1alpha1.ClusterAddonStepInputTarget) ([]hookTargetMachine, error) {
	refKind, ok := e.inputRefKind(from.Input)
	if !ok {
		return nil, fmt.Errorf("hook %s fromInput %s is not a resourceRef input", hook.Name, from.Input)
	}
	name := e.inputValue(from.Input)
	if name == "" {
		return nil, fmt.Errorf("hook %s fromInput %s has no value", hook.Name, from.Input)
	}
	switch refKind {
	case hooks.RefKindStorageExport:
		export, ok := stateview.ExportByName(e.state, name)
		if !ok {
			return nil, fmt.Errorf("hook %s fromInput references unknown StorageExport %q", hook.Name, name)
		}
		return e.storageClusterMachines(export.Spec.StorageClusterRef.Name)
	case hooks.RefKindStorageCluster:
		return e.storageClusterMachines(name)
	case hooks.RefKindContainerCluster:
		return e.containerClusterMachines(name)
	case hooks.RefKindMachine:
		machine, ok := stateview.Machine(e.state, name)
		if !ok {
			return nil, fmt.Errorf("hook %s fromInput references unknown Machine %q", hook.Name, name)
		}
		return []hookTargetMachine{machineHookTarget("Machine/"+name, machine)}, nil
	}
	return nil, fmt.Errorf("hook %s fromInput refKind %q is not supported", hook.Name, refKind)
}

func (e *addonHookExecutor) clusterMachines(name string) ([]hookTargetMachine, error) {
	if _, ok := stateview.ClusterByName(e.state, name); ok {
		return e.storageClusterMachines(name)
	}
	for _, cluster := range e.state.ContainerClusters {
		if cluster.Metadata.Name == name {
			return e.containerClusterMachines(name)
		}
	}
	return nil, fmt.Errorf("hook target cluster %q not found", name)
}

func (e *addonHookExecutor) containerClusterMachines(name string) ([]hookTargetMachine, error) {
	var cluster v1alpha1.ContainerCluster
	found := false
	for _, c := range e.state.ContainerClusters {
		if c.Metadata.Name == name {
			cluster, found = c, true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("container cluster %q not found", name)
	}
	var out []hookTargetMachine
	for _, node := range cluster.Spec.Nodes {
		machine, ok := stateview.Machine(e.state, node.MachineRef.Name)
		if !ok {
			continue
		}
		out = append(out, machineHookTarget("ContainerCluster/"+name+" node/"+node.MachineRef.Name, machine))
	}
	return out, nil
}

func (e *addonHookExecutor) storageClusterMachines(name string) ([]hookTargetMachine, error) {
	cluster, ok := stateview.ClusterByName(e.state, name)
	if !ok || cluster.Spec.Ceph == nil {
		return nil, fmt.Errorf("storage cluster %q not found or not a Ceph cluster", name)
	}
	bootstrap := cluster.Spec.Ceph.Cephadm.Bootstrap.Node
	ordered := append([]v1alpha1.StorageCephNode(nil), cluster.Spec.Ceph.Topology.Nodes...)
	for i := range ordered {
		if ordered[i].Name == bootstrap {
			ordered[0], ordered[i] = ordered[i], ordered[0]
			break
		}
	}
	var out []hookTargetMachine
	for _, node := range ordered {
		machine, ok := topology.NodeMachine(e.state, cluster, node.Name)
		if !ok {
			continue
		}
		target := machineHookTarget("StorageCluster/"+name+" node/"+node.Name, machine)
		target.sshUser = v1alpha1.StorageClusterCephadmSSHUser(cluster)
		if ref := cluster.Spec.Ceph.Cephadm.ClusterSSH.KeyRef.Name; ref != "" {
			target.sshKeyRef = v1alpha1.SecretRef{Name: ref}
		}
		out = append(out, target)
	}
	return out, nil
}

func machineHookTarget(label string, machine v1alpha1.Machine) hookTargetMachine {
	target := hookTargetMachine{label: label, machine: machine}
	if machine.Spec.Access.SSH != nil {
		target.sshUser = machine.Spec.Access.SSH.User
		target.sshKeyRef = v1alpha1.MachineSSHKeyRef(machine)
		target.sshPasswordRef = v1alpha1.MachineSSHPasswordRef(machine)
		target.sudoPasswordRef = machine.Spec.Access.SSH.SudoPasswordRef
	}
	return target
}

func (e *addonHookExecutor) inputRefKind(input string) (string, bool) {
	for _, accepted := range e.plan.Addon.Spec.Accepts.Inputs {
		if accepted.Name != input {
			continue
		}
		if accepted.ResourceRef != nil && accepted.ResourceRef.Kind != "" {
			return accepted.ResourceRef.Kind, true
		}
	}
	return "", false
}

func hookConnectionSecretNames(machines []hookTargetMachine) []string {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	for _, m := range machines {
		if m.machine.Spec.Access.SSH == nil {
			continue
		}
		add(m.sshKeyRef.Name)
		add(m.sshPasswordRef.Name)
		add(m.sudoPasswordRef.Name)
		add(m.machine.Spec.Access.SSH.KnownHostsRef.Name)
	}
	return names
}

func dedupeTargetMachines(in []hookTargetMachine) []hookTargetMachine {
	seen := map[string]bool{}
	var out []hookTargetMachine
	for _, m := range in {
		if m.machine.Metadata.Name == "" || seen[m.machine.Metadata.Name] {
			continue
		}
		seen[m.machine.Metadata.Name] = true
		out = append(out, m)
	}
	return out
}
