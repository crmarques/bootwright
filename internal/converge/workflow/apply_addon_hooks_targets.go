package workflow

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/hooks"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

// hookTargetMachine is one resolved target: a Machine plus a human label used in
// the ad-hoc inventory and failure messages.
type hookTargetMachine struct {
	label   string
	machine v1alpha1.Machine
}

// resolveHookTargetMachines resolves a hook's target spec to the machines its
// playbook runs against. It mirrors resolveProvisioningTarget but at the machine
// level (hooks build an ad-hoc SSH inventory, never a rendered inventory group),
// and adds the fromInput ref-chain the storage-attachment use case needs.
func (e *addonHookExecutor) resolveHookTargetMachines(hook v1alpha1.ClusterAddonHook) ([]hookTargetMachine, error) {
	target := hook.Target
	var out []hookTargetMachine
	add := func(more []hookTargetMachine) { out = append(out, more...) }

	if target.BoundCluster {
		machines, err := e.containerClusterMachines(e.plan.Cluster)
		if err != nil {
			return nil, err
		}
		add(machines)
	}
	for _, name := range target.Clusters {
		machines, err := e.clusterMachines(name)
		if err != nil {
			return nil, err
		}
		add(machines)
	}
	for _, name := range target.Machines {
		machine, ok := stateview.Machine(e.state, name)
		if !ok {
			return nil, fmt.Errorf("hook %s target machine %q not found", hook.Name, name)
		}
		add([]hookTargetMachine{{label: "Machine/" + name, machine: machine}})
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

func (e *addonHookExecutor) fromInputMachines(hook v1alpha1.ClusterAddonHook, from v1alpha1.ClusterAddonHookInputTarget) ([]hookTargetMachine, error) {
	refKind, ok := e.inputRefKind(from.Input, from.Property)
	if !ok {
		return nil, fmt.Errorf("hook %s fromInput %s.%s is not a refKind property", hook.Name, from.Input, from.Property)
	}
	name := e.inputValue(from.Input, from.Property)
	if name == "" {
		return nil, fmt.Errorf("hook %s fromInput %s.%s has no value", hook.Name, from.Input, from.Property)
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
		return []hookTargetMachine{{label: "Machine/" + name, machine: machine}}, nil
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
	for _, node := range cluster.Spec.Hosts {
		machine, ok := stateview.Machine(e.state, node.MachineRef.Name)
		if !ok {
			continue
		}
		out = append(out, hookTargetMachine{label: "ContainerCluster/" + name + " node/" + node.MachineRef.Name, machine: machine})
	}
	return out, nil
}

// storageClusterMachines resolves a Ceph cluster's admin-capable machines,
// ordering the cephadm bootstrap host first (it holds the admin keyring the
// exporter and ceph-auth commands need), then the remaining topology hosts.
func (e *addonHookExecutor) storageClusterMachines(name string) ([]hookTargetMachine, error) {
	cluster, ok := stateview.ClusterByName(e.state, name)
	if !ok || cluster.Spec.Ceph == nil {
		return nil, fmt.Errorf("storage cluster %q not found or not a Ceph cluster", name)
	}
	bootstrap := cluster.Spec.Ceph.Cephadm.Bootstrap.Host
	ordered := append([]v1alpha1.StorageCephHost(nil), cluster.Spec.Ceph.Topology.Hosts...)
	for i := range ordered {
		if ordered[i].Hostname == bootstrap {
			ordered[0], ordered[i] = ordered[i], ordered[0]
			break
		}
	}
	var out []hookTargetMachine
	for _, node := range ordered {
		machine, ok := topology.NodeMachine(e.state, cluster, node.Hostname)
		if !ok {
			continue
		}
		out = append(out, hookTargetMachine{label: "StorageCluster/" + name + " node/" + node.Hostname, machine: machine})
	}
	return out, nil
}

func (e *addonHookExecutor) inputRefKind(input, property string) (string, bool) {
	for _, accepted := range e.plan.Addon.Spec.Accepts.Inputs {
		if accepted.Name != input {
			continue
		}
		if schema, ok := accepted.Schema.Properties[property]; ok && schema.RefKind != "" {
			return schema.RefKind, true
		}
	}
	return "", false
}

// machineSecretNames collects the context secret names the resolved machines'
// SSH access references (private key + explicit known-hosts), for scoped
// materialization into the hook connection dir.
func machineSecretNames(machines []hookTargetMachine) []string {
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
		add(m.machine.Spec.Access.SSH.KeyRef.Name)
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
