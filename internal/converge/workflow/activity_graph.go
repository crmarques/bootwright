package workflow

import (
	"fmt"
	"sort"
	"strings"
)

type CapabilityRef struct {
	Kind string
	Name string
}

func (c CapabilityRef) key() string {
	return c.Kind + ":" + c.Name
}

type Activity struct {
	ID                   string
	Task                 ApplyTask
	Requires             []CapabilityRef
	Provides             []CapabilityRef
	ExplicitDependencies []string
}

type ActivityGraph struct {
	activities map[string]Activity
	order      []string
	providedBy map[string]string
	available  map[string]bool
}

func NewActivityGraph() *ActivityGraph {
	return &ActivityGraph{
		activities: map[string]Activity{},
		providedBy: map[string]string{},
		available:  map[string]bool{},
	}
}

func (g *ActivityGraph) AddAvailable(capability CapabilityRef) {
	g.available[capability.key()] = true
}

func (g *ActivityGraph) Add(activity Activity) error {
	if activity.ID == "" {
		activity.ID = activity.Task.Entry.ID
	}
	if activity.Task.Entry.ID == "" {
		activity.Task.Entry.ID = activity.ID
	}
	if activity.ID == "" {
		return fmt.Errorf("activity ID is required")
	}
	if _, ok := g.activities[activity.ID]; ok {
		return fmt.Errorf("duplicate activity %s", activity.ID)
	}
	g.activities[activity.ID] = activity
	g.order = append(g.order, activity.ID)
	for _, capability := range activity.Provides {
		key := capability.key()
		if existing := g.providedBy[key]; existing != "" {
			return fmt.Errorf("capability %s is provided by both %s and %s", key, existing, activity.ID)
		}
		g.providedBy[key] = activity.ID
	}
	return nil
}

// AddDependency appends dependsOn to an already-added activity's hard explicit
// dependencies. The ProvisioningPlaybook planner uses it to gate a core phase
// task on a before-timing playbook after every core activity is in the graph.
func (g *ActivityGraph) AddDependency(id, dependsOn string) error {
	activity, ok := g.activities[id]
	if !ok {
		return fmt.Errorf("cannot add dependency to unknown activity %s", id)
	}
	activity.ExplicitDependencies = appendUniqueString(activity.ExplicitDependencies, dependsOn)
	g.activities[id] = activity
	return nil
}

// AddOrderingDependency appends dependsOn to an already-added activity's soft
// ordering dependencies (wait-until-terminal, run regardless). Used to gate a
// core phase task on a before-timing playbook whose failureMode is continue, so
// the phase proceeds even if the playbook fails.
func (g *ActivityGraph) AddOrderingDependency(id, dependsOn string) error {
	activity, ok := g.activities[id]
	if !ok {
		return fmt.Errorf("cannot add ordering dependency to unknown activity %s", id)
	}
	activity.Task.Entry.OrderingDependencies = appendUniqueString(activity.Task.Entry.OrderingDependencies, dependsOn)
	g.activities[id] = activity
	return nil
}

// ActivitySnapshot returns the activities added so far, in insertion order. The
// ProvisioningPlaybook planner reads it to bucket core tasks by phase without
// threading a phase index through every planner call site.
func (g *ActivityGraph) ActivitySnapshot() []Activity {
	out := make([]Activity, 0, len(g.order))
	for _, id := range g.order {
		out = append(out, g.activities[id])
	}
	return out
}

func (g *ActivityGraph) Lower() ([]ApplyTask, error) {
	depsByID := map[string][]string{}
	for _, id := range g.order {
		activity := g.activities[id]
		var deps []string
		for _, required := range activity.Requires {
			key := required.key()
			if g.available[key] {
				continue
			}
			provider := g.providedBy[key]
			if provider == "" {
				return nil, fmt.Errorf("%s requires unavailable capability %s", activity.ID, key)
			}
			deps = appendUniqueString(deps, provider)
		}
		deps = appendUniqueStrings(deps, activity.Task.Entry.Dependencies...)
		deps = appendUniqueStrings(deps, activity.ExplicitDependencies...)
		depsByID[id] = deps
	}
	if err := detectActivityCycles(depsByID, g.activities); err != nil {
		return nil, err
	}
	tasks := make([]ApplyTask, 0, len(g.order))
	for _, id := range g.order {
		activity := g.activities[id]
		task := activity.Task
		task.Entry.Dependencies = depsByID[id]
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func appendUniqueStrings(items []string, values ...string) []string {
	for _, value := range values {
		items = appendUniqueString(items, value)
	}
	return items
}

func detectActivityCycles(depsByID map[string][]string, activities map[string]Activity) error {
	const (
		visiting = 1
		visited  = 2
	)
	state := map[string]int{}
	var stack []string
	var visit func(string) error
	visit = func(id string) error {
		switch state[id] {
		case visiting:
			cycle := append([]string(nil), stack...)
			cycle = append(cycle, id)
			return fmt.Errorf("activity graph dependency cycle: %s", strings.Join(cycle, " -> "))
		case visited:
			return nil
		}
		state[id] = visiting
		stack = append(stack, id)
		for _, dep := range depsByID[id] {
			if _, ok := activities[dep]; !ok {
				continue
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = visited
		return nil
	}
	ids := make([]string, 0, len(activities))
	for id := range activities {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func machineOSReadyCapability(machine string) CapabilityRef {
	return CapabilityRef{Kind: "machine.os-ready", Name: machine}
}

func machineInstantiatedCapability(machine string) CapabilityRef {
	return CapabilityRef{Kind: "machine.instantiated", Name: machine}
}

func providerHostReadyCapability(host string) CapabilityRef {
	return CapabilityRef{Kind: "provider.host-ready", Name: host}
}

func providerServiceReadyCapability(host string) CapabilityRef {
	return CapabilityRef{Kind: "provider.service-ready", Name: host}
}

func serviceEndpointReadyCapability(host string) CapabilityRef {
	return CapabilityRef{Kind: "service.endpoint-ready", Name: host}
}

func clusterInstalledCapability(cluster string) CapabilityRef {
	return CapabilityRef{Kind: "cluster.installed", Name: cluster}
}

func addonProvidesCapability(cluster, capability string) CapabilityRef {
	return CapabilityRef{Kind: "addon.provides:" + capability, Name: cluster}
}

// virtctlProvisionedCapability marks that a KubeVirt host cluster's
// version-matched virtctl has been installed on the controller. The boot task of
// every child cluster running on that host requires it, so booting waits for the
// deps-stage provision activity.
func virtctlProvisionedCapability(hostCluster string) CapabilityRef {
	return CapabilityRef{Kind: "kubevirt.virtctl-provisioned", Name: hostCluster}
}
