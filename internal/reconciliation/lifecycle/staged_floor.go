package lifecycle

import (
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type stagedFloor struct {
	stage    reconciliation.Stage
	clusters []string
}

func floorOf(stage *operationstore.Stage) stagedFloor {
	if stage == nil {
		return stagedFloor{}
	}
	return stagedFloor{stage: stage.Floor, clusters: slices.Clone(stage.Clusters)}
}

func (f stagedFloor) staged() bool { return f.stage != "" }

func (f stagedFloor) same(other stagedFloor) bool {
	return f.stage == other.stage && slices.Equal(f.clusters, other.clusters)
}

func (f stagedFloor) covers(other stagedFloor) bool {
	return other.stage.AtOrAbove(f.stage) && (len(f.clusters) == 0 ||
		len(other.clusters) != 0 && !slices.ContainsFunc(other.clusters, func(name string) bool { return !slices.Contains(f.clusters, name) }))
}

func (f stagedFloor) at(stage reconciliation.Stage) stagedFloor {
	if !reconciliation.NarrowsToClusters(stage) {
		return stagedFloor{stage: stage}
	}
	return stagedFloor{stage: stage, clusters: slices.Clone(f.clusters)}
}

func (f stagedFloor) joined(other stagedFloor) stagedFloor {
	lower := f.stage
	if lower.AtOrAbove(other.stage) {
		lower = other.stage
	}
	if len(f.clusters) == 0 || len(other.clusters) == 0 {
		return stagedFloor{stage: lower}
	}
	return stagedFloor{stage: lower, clusters: slices.Compact(slices.Sorted(slices.Values(slices.Concat(f.clusters, other.clusters))))}.at(lower)
}

func (f stagedFloor) flags() []string {
	if !f.staged() {
		return nil
	}
	flags := []string{"--stage", string(f.stage)}
	if len(f.clusters) != 0 {
		flags = append(flags, "--clusters", strings.Join(f.clusters, ","))
	}
	return flags
}

func (f stagedFloor) named() string {
	named := "the " + string(f.stage) + " stage"
	switch len(f.clusters) {
	case 0:
		return named
	case 1:
		return named + " of cluster " + f.clusters[0]
	}
	return named + " of clusters " + strings.Join(f.clusters, ", ")
}

func (f stagedFloor) seeds(plan reconciliation.Plan, block reconciliation.Block) bool {
	return block.Stage.AtOrAbove(f.stage) && (len(f.clusters) == 0 || f.names(plan, block))
}

func (f stagedFloor) names(plan reconciliation.Plan, block reconciliation.Block) bool {
	return slices.ContainsFunc(reconciliation.ClusterOwners(plan, block), func(name string) bool { return slices.Contains(f.clusters, name) })
}

func (f stagedFloor) record(taken []string, power []operationstore.KeptPower) *operationstore.Stage {
	return &operationstore.Stage{Floor: f.stage, Clusters: slices.Clone(f.clusters), TakenBack: taken, KeptPower: power}
}

func (s Service) foreignClusters(live reconciliation.Plan, floor stagedFloor, scope []string) ([]string, stagedFloor) {
	var foreign []string
	wider := floor
	for first := true; len(floor.clusters) != 0; first = false {
		var added []string
		for _, block := range live.Blocks {
			owners := reconciliation.ClusterOwners(live, block)
			if !slices.Contains(scope, block.ID) || len(owners) == 0 || wider.names(live, block) {
				continue
			}
			if first {
				foreign = append(foreign, block.ID+" (cluster "+strings.Join(owners, ", cluster ")+")")
			}
			added = append(added, owners...)
		}
		if len(added) == 0 {
			break
		}
		wider = wider.joined(stagedFloor{stage: wider.stage, clusters: added})
		scope = s.closure(live, wider)
	}
	return foreign, wider
}

func refuseClusters(name string, floor stagedFloor, held liveState) error {
	if len(floor.clusters) == 0 {
		return nil
	}
	for _, block := range held.whole.Blocks {
		if len(block.ClusterOwners) == 0 && clusterChild(block.BlockDefinition) {
			return engineInputInvalid.Fail("the frozen plan lacks cluster ownership for "+block.ID+", so --clusters cannot narrow a staged destroy of it",
				"take back the whole stage with "+contextCommand(name, "destroy --dry-run", "--stage", string(floor.stage))+", or the whole context with "+contextCommand(name, string(reconciliation.Destroy)))
		}
	}
	var planned, atFloor []string
	for _, holding := range held.chain.ownership().Holdings() {
		owners := reconciliation.ClusterOwners(reconciliation.Plan{}, holding.Block)
		planned = append(planned, owners...)
		if holding.Block.Stage.AtOrAbove(floor.stage) {
			atFloor = append(atFloor, owners...)
		}
	}
	if unknown := absent(floor.clusters, planned); len(unknown) != 0 {
		return engineInputInvalid.Fail("--clusters names "+strings.Join(unknown, ", ")+", which "+agreeing(unknown, "owns", "own")+" no step of this context",
			"name the clusters whose steps to take back, or take back the whole stage with "+contextCommand(name, "destroy --dry-run", "--stage", string(floor.stage)))
	}
	if idle := absent(floor.clusters, atFloor); len(idle) != 0 {
		remedy := "name only clusters with steps at the " + string(floor.stage) + " stage"
		if lower := floor.at(reconciliation.StageClusters); floor.stage != reconciliation.StageClusters {
			remedy += ", or take back from the clusters stage with " + contextCommand(name, "destroy --dry-run", lower.flags()...)
		}
		return engineInputInvalid.Fail("--clusters names "+strings.Join(idle, ", ")+", which "+agreeing(idle, "holds", "hold")+" nothing at or above the "+string(floor.stage)+" stage",
			remedy)
	}
	return nil
}

func absent(names, held []string) []string {
	return slices.DeleteFunc(slices.Clone(names), func(name string) bool { return slices.Contains(held, name) })
}

func agreeing(names []string, one, many string) string {
	if len(names) == 1 {
		return one
	}
	return many
}
