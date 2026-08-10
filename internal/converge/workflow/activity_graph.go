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
	providedBy map[string][]string
	available  map[string]bool
}

func NewActivityGraph() *ActivityGraph {
	return &ActivityGraph{
		activities: map[string]Activity{},
		providedBy: map[string][]string{},
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
	if activity.Task.ExecutionClass != "" && activity.Task.ExecutionClass != ApplyTaskExecutionLiveProof {
		return fmt.Errorf("activity %s has unknown execution class %q", activity.ID, activity.Task.ExecutionClass)
	}
	if activity.Task.ExecutionClass == ApplyTaskExecutionLiveProof && activity.Task.Entry.Kind != ApplyTaskKindControllerNameResolution {
		return fmt.Errorf("activity %s uses the evidence-free live-proof execution class for unsupported task kind %q", activity.ID, activity.Task.Entry.Kind)
	}
	if activity.Task.Entry.Kind == ApplyTaskKindControllerNameResolution {
		expected := "bootwright_controller_name_resolution_mutation_selected=true"
		if activity.Task.ExecutionClass == ApplyTaskExecutionLiveProof {
			expected = "bootwright_controller_name_resolution_mutation_selected=false"
		}
		decisions := 0
		matched := false
		for _, pair := range activity.Task.ExtraVarPairs {
			if strings.HasPrefix(pair, "bootwright_controller_name_resolution_mutation_selected=") {
				decisions++
				matched = pair == expected
			}
		}
		if decisions != 1 || !matched {
			return fmt.Errorf("activity %s execution class %q requires exactly %q, got %v", activity.ID, activity.Task.ExecutionClass, expected, activity.Task.ExtraVarPairs)
		}
	}
	if _, ok := g.activities[activity.ID]; ok {
		return fmt.Errorf("duplicate activity %s", activity.ID)
	}
	g.activities[activity.ID] = activity
	g.order = append(g.order, activity.ID)
	for _, capability := range activity.Provides {
		key := capability.key()
		g.providedBy[key] = appendUniqueString(g.providedBy[key], activity.ID)
	}
	return nil
}

func (g *ActivityGraph) AddDependency(id, dependsOn string) error {
	activity, ok := g.activities[id]
	if !ok {
		return fmt.Errorf("cannot add dependency to unknown activity %s", id)
	}
	activity.ExplicitDependencies = appendUniqueString(activity.ExplicitDependencies, dependsOn)
	g.activities[id] = activity
	return nil
}

func (g *ActivityGraph) AddOrderingDependency(id, dependsOn string) error {
	activity, ok := g.activities[id]
	if !ok {
		return fmt.Errorf("cannot add ordering dependency to unknown activity %s", id)
	}
	activity.Task.Entry.OrderingDependencies = appendUniqueString(activity.Task.Entry.OrderingDependencies, dependsOn)
	g.activities[id] = activity
	return nil
}

func (g *ActivityGraph) ActivitySnapshot() []Activity {
	out := make([]Activity, 0, len(g.order))
	for _, id := range g.order {
		out = append(out, g.activities[id])
	}
	return out
}

func (g *ActivityGraph) Lower() ([]ApplyTask, error) {
	depsByID := map[string][]string{}
	cycleDepsByID := map[string][]string{}
	for _, id := range g.order {
		activity := g.activities[id]
		var deps []string
		for _, required := range activity.Requires {
			key := required.key()
			if g.available[key] {
				continue
			}
			providers := g.providedBy[key]
			if len(providers) == 0 {
				return nil, fmt.Errorf("%s requires unavailable capability %s", activity.ID, key)
			}
			deps = appendUniqueStrings(deps, providers...)
		}
		deps = appendUniqueStrings(deps, activity.Task.Entry.Dependencies...)
		deps = appendUniqueStrings(deps, activity.ExplicitDependencies...)
		depsByID[id] = deps
		entry := activity.Task.Entry
		entry.Dependencies = deps
		cycleDepsByID[id] = taskDependencyIDs(entry)
	}
	if err := detectActivityCycles(cycleDepsByID, g.activities); err != nil {
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

func virtctlProvisionedCapability(hostCluster string) CapabilityRef {
	return CapabilityRef{Kind: "kubevirt.virtctl-provisioned", Name: hostCluster}
}
