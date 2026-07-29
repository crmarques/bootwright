package workflow

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/steps"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

type stepTargetMachine struct {
	label           string
	machine         v1alpha1.Machine
	sshUser         string
	sshKeyRef       v1alpha1.SecretRef
	sshPasswordRef  v1alpha1.SecretRef
	sudoPasswordRef v1alpha1.SecretRef
}

func (e *addonStepExecutor) resolveStepTargetMachines(step v1alpha1.ClusterAddonStep) ([]stepTargetMachine, error) {
	target := step.Target
	var out []stepTargetMachine
	add := func(more []stepTargetMachine) { out = append(out, more...) }

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
				return nil, fmt.Errorf("step %s target machine %q not found", step.Name, name)
			}
			add([]stepTargetMachine{machineStepTarget("Machine/"+name, machine)})
		}
	}
	if target.FromInput != nil {
		machines, err := e.fromInputMachines(step, *target.FromInput)
		if err != nil {
			return nil, err
		}
		add(machines)
	}
	return dedupeTargetMachines(out), nil
}

func (e *addonStepExecutor) fromInputMachines(step v1alpha1.ClusterAddonStep, from v1alpha1.ClusterAddonStepInputTarget) ([]stepTargetMachine, error) {
	refKind, ok := e.inputRefKind(from.Input)
	if !ok {
		return nil, fmt.Errorf("step %s fromInput %s is not a resourceRef input", step.Name, from.Input)
	}
	name := e.inputValue(from.Input)
	if name == "" {
		return nil, fmt.Errorf("step %s fromInput %s has no value", step.Name, from.Input)
	}
	switch refKind {
	case steps.RefKindStorageExport:
		export, ok := stateview.ExportByName(e.state, name)
		if !ok {
			return nil, fmt.Errorf("step %s fromInput references unknown StorageExport %q", step.Name, name)
		}
		return e.storageClusterMachines(export.Spec.StorageClusterRef.Name)
	case steps.RefKindStorageCluster:
		return e.storageClusterMachines(name)
	case steps.RefKindContainerCluster:
		return e.containerClusterMachines(name)
	case steps.RefKindMachine:
		machine, ok := stateview.Machine(e.state, name)
		if !ok {
			return nil, fmt.Errorf("step %s fromInput references unknown Machine %q", step.Name, name)
		}
		return []stepTargetMachine{machineStepTarget("Machine/"+name, machine)}, nil
	}
	return nil, fmt.Errorf("step %s fromInput refKind %q is not supported", step.Name, refKind)
}

func (e *addonStepExecutor) clusterMachines(name string) ([]stepTargetMachine, error) {
	if _, ok := stateview.ClusterByName(e.state, name); ok {
		return e.storageClusterMachines(name)
	}
	for _, cluster := range e.state.ContainerClusters {
		if cluster.Metadata.Name == name {
			return e.containerClusterMachines(name)
		}
	}
	return nil, fmt.Errorf("step target cluster %q not found", name)
}

func (e *addonStepExecutor) containerClusterMachines(name string) ([]stepTargetMachine, error) {
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
	var out []stepTargetMachine
	for _, node := range cluster.Spec.Nodes {
		machine, ok := stateview.Machine(e.state, node.MachineRef.Name)
		if !ok {
			continue
		}
		out = append(out, machineStepTarget("ContainerCluster/"+name+" node/"+node.MachineRef.Name, machine))
	}
	return out, nil
}

func (e *addonStepExecutor) storageClusterMachines(name string) ([]stepTargetMachine, error) {
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
	var out []stepTargetMachine
	for _, node := range ordered {
		machine, ok := topology.NodeMachine(e.state, cluster, node.Name)
		if !ok {
			continue
		}
		target := machineStepTarget("StorageCluster/"+name+" node/"+node.Name, machine)
		target.sshUser = v1alpha1.StorageClusterCephadmSSHUser(cluster)
		if ref := cluster.Spec.Ceph.Cephadm.ClusterSSH.KeyRef.Name; ref != "" {
			target.sshKeyRef = v1alpha1.SecretRef{Name: ref}
		}
		out = append(out, target)
	}
	return out, nil
}

func machineStepTarget(label string, machine v1alpha1.Machine) stepTargetMachine {
	target := stepTargetMachine{label: label, machine: machine}
	if machine.Spec.Access.SSH != nil {
		target.sshUser = machine.Spec.Access.SSH.User
		target.sshKeyRef = v1alpha1.MachineSSHKeyRef(machine)
		target.sshPasswordRef = v1alpha1.MachineSSHPasswordRef(machine)
		target.sudoPasswordRef = machine.Spec.Access.SSH.SudoPasswordRef
	}
	return target
}

func (e *addonStepExecutor) inputRefKind(input string) (string, bool) {
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

func stepConnectionSecretNames(machines []stepTargetMachine) []string {
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

func dedupeTargetMachines(in []stepTargetMachine) []stepTargetMachine {
	seen := map[string]bool{}
	var out []stepTargetMachine
	for _, m := range in {
		if m.machine.Metadata.Name == "" || seen[m.machine.Metadata.Name] {
			continue
		}
		seen[m.machine.Metadata.Name] = true
		out = append(out, m)
	}
	return out
}
