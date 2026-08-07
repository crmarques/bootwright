package workflow

import "sort"

type FlightPlanStep struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Label     string   `json:"label"`
	Node      string   `json:"node,omitempty"`
	DependsOn []string `json:"dependsOn,omitempty"`
}

type FlightPlanLane struct {
	Cluster     string           `json:"cluster,omitempty"`
	ClusterKind string           `json:"clusterKind,omitempty"`
	Steps       []FlightPlanStep `json:"steps"`
}

type FlightPlanStage struct {
	Index int              `json:"index"`
	Lanes []FlightPlanLane `json:"lanes"`
}

type FlightPlan struct {
	Stages []FlightPlanStage `json:"stages"`
}

const flightPlanInfraLane = ""

func BuildFlightPlan(tasks []ApplyTask, clusterOrder []string) FlightPlan {
	depths := flightPlanDepths(tasks)
	byStage := map[int][]ApplyTask{}
	maxDepth := -1
	for _, task := range tasks {
		depth := depths[task.Entry.ID]
		byStage[depth] = append(byStage[depth], task)
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	rank := flightPlanLaneRank(clusterOrder)
	plan := FlightPlan{}
	for depth := 0; depth <= maxDepth; depth++ {
		stageTasks := byStage[depth]
		if len(stageTasks) == 0 {
			continue
		}
		plan.Stages = append(plan.Stages, FlightPlanStage{
			Index: len(plan.Stages),
			Lanes: flightPlanLanes(stageTasks, rank),
		})
	}
	return plan
}

func flightPlanLanes(tasks []ApplyTask, rank map[string]int) []FlightPlanLane {
	byCluster := map[string][]ApplyTask{}
	kinds := map[string]string{}
	for _, task := range tasks {
		cluster := task.Entry.Cluster
		byCluster[cluster] = append(byCluster[cluster], task)
		if task.Entry.ClusterKind != "" {
			kinds[cluster] = task.Entry.ClusterKind
		}
	}
	names := make([]string, 0, len(byCluster))
	for cluster := range byCluster {
		names = append(names, cluster)
	}
	sort.Slice(names, func(i, j int) bool {
		return flightPlanLaneLess(names[i], names[j], rank)
	})
	lanes := make([]FlightPlanLane, 0, len(names))
	for _, cluster := range names {
		laneTasks := byCluster[cluster]
		sort.Slice(laneTasks, func(i, j int) bool {
			return laneTasks[i].Entry.ID < laneTasks[j].Entry.ID
		})
		steps := make([]FlightPlanStep, 0, len(laneTasks))
		for _, task := range laneTasks {
			steps = append(steps, FlightPlanStep{
				ID:        task.Entry.ID,
				Kind:      task.Entry.Kind,
				Label:     task.Entry.Label,
				Node:      task.Entry.Node,
				DependsOn: flightPlanDependsOn(task),
			})
		}
		lanes = append(lanes, FlightPlanLane{
			Cluster:     cluster,
			ClusterKind: kinds[cluster],
			Steps:       steps,
		})
	}
	return lanes
}

func flightPlanLaneLess(a, b string, rank map[string]int) bool {
	if a == flightPlanInfraLane {
		return b != flightPlanInfraLane
	}
	if b == flightPlanInfraLane {
		return false
	}
	rankA, okA := rank[a]
	rankB, okB := rank[b]
	switch {
	case okA && okB && rankA != rankB:
		return rankA < rankB
	case okA != okB:
		return okA
	}
	return a < b
}

func flightPlanLaneRank(order []string) map[string]int {
	rank := make(map[string]int, len(order))
	for i, name := range order {
		if _, seen := rank[name]; seen {
			continue
		}
		rank[name] = i
	}
	return rank
}

func flightPlanDependsOn(task ApplyTask) []string {
	var out []string
	out = appendUniqueStrings(out, task.Entry.Dependencies...)
	out = appendUniqueStrings(out, task.Entry.OrderingDependencies...)
	sort.Strings(out)
	return out
}

func flightPlanDepths(tasks []ApplyTask) map[string]int {
	edges := make(map[string][]string, len(tasks))
	known := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		known[task.Entry.ID] = true
	}
	for _, task := range tasks {
		var deps []string
		for _, dep := range flightPlanDependsOn(task) {
			if known[dep] && dep != task.Entry.ID {
				deps = append(deps, dep)
			}
		}
		edges[task.Entry.ID] = deps
	}
	depths := make(map[string]int, len(tasks))
	resolving := map[string]bool{}
	var depth func(string) int
	depth = func(id string) int {
		if value, ok := depths[id]; ok {
			return value
		}
		if resolving[id] {
			return 0
		}
		resolving[id] = true
		best := 0
		for _, dep := range edges[id] {
			if value := depth(dep) + 1; value > best {
				best = value
			}
		}
		delete(resolving, id)
		depths[id] = best
		return best
	}
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.Entry.ID)
	}
	sort.Strings(ids)
	for _, id := range ids {
		depth(id)
	}
	return depths
}

func (p FlightPlan) StageOf(taskID string) (int, bool) {
	for _, stage := range p.Stages {
		for _, lane := range stage.Lanes {
			for _, step := range lane.Steps {
				if step.ID == taskID {
					return stage.Index, true
				}
			}
		}
	}
	return 0, false
}

func (p FlightPlan) StepCount() int {
	total := 0
	for _, stage := range p.Stages {
		for _, lane := range stage.Lanes {
			total += len(lane.Steps)
		}
	}
	return total
}
