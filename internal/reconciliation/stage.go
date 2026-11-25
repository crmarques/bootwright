package reconciliation

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// Stage groups blocks by the kind of platform work they perform. A block's
// stage is frozen with the plan; a selection gates which blocks an invocation
// may start, never which blocks the plan contains.
type Stage string

const (
	StageController             Stage = "controller"
	StageInfrastructureServices Stage = "infrastructure-services"
	StageSubstrates             Stage = "substrates"
	StageMachines               Stage = "machines"
	StageClusters               Stage = "clusters"
	StageAddOns                 Stage = "add-ons"
)

func Stages() []Stage {
	return []Stage{StageController, StageInfrastructureServices, StageSubstrates, StageMachines, StageClusters, StageAddOns}
}

func ValidStage(value Stage) bool { return slices.Contains(Stages(), value) }

func Floors() []Stage { return Stages()[2:] }

func ValidFloor(value Stage) bool { return slices.Contains(Floors(), value) }

func (s Stage) AtOrAbove(floor Stage) bool {
	return slices.Index(Stages(), s) >= slices.Index(Stages(), floor)
}

// StageSelection is the set of stages an invocation may start. An empty
// selection admits every stage, which is what an omitted flag means.
type StageSelection []Stage

// ParseStages accepts the public stage names in any order and returns them in
// canonical order. An unknown name refuses rather than selecting nothing.
func ParseStages(values []string) (StageSelection, error) {
	var selection StageSelection
	for _, value := range values {
		stage := Stage(value)
		if !ValidStage(stage) {
			return nil, stateError("lifecycle stage is not recognized")
		}
		if !slices.Contains(selection, stage) {
			selection = append(selection, stage)
		}
	}
	slices.SortFunc(selection, func(x, y Stage) int {
		return slices.Index(Stages(), x) - slices.Index(Stages(), y)
	})
	return selection, nil
}

func (s StageSelection) Selects(stage Stage) bool {
	return len(s) == 0 || slices.Contains(s, stage)
}

// Selection is a stage selection with an optional add-on instance to stop
// after. Bound to a plan, a selection of the controller stage also admits the
// blocks a controller block waits for, which run before that stage finishes.
type Selection struct {
	Stages     StageSelection
	Until      string
	Clusters   []string
	Machines   []string
	targets    map[string]bool
	joint      map[string]bool
	within     map[string]bool
	precursors map[string]bool
}

func ParseSelection(stages []string, until string) (Selection, error) {
	parsed, err := ParseStages(stages)
	if err != nil {
		return Selection{}, err
	}
	return Selection{Stages: parsed, Until: until}, nil
}

func (s Selection) WithTargets(clusters, machines []string) (Selection, error) {
	if len(clusters) != 0 && len(machines) != 0 {
		return Selection{}, stateError("--clusters and --machines are mutually exclusive")
	}
	for _, names := range [][]string{clusters, machines} {
		for _, name := range names {
			if !api.ValidLexical(api.LexicalName, name) {
				return Selection{}, stateError("apply target names must be lowercase DNS labels")
			}
		}
	}
	s.Clusters = slices.Compact(slices.Sorted(slices.Values(clusters)))
	s.Machines = slices.Compact(slices.Sorted(slices.Values(machines)))
	return s, nil
}

func (s Selection) Targeted() bool { return len(s.Clusters) != 0 || len(s.Machines) != 0 }

func (s Selection) Empty() bool { return len(s.Stages) == 0 && s.Until == "" && !s.Targeted() }

func clusterOwners(plan Plan, block Block) []string {
	if plan.Preparation == nil && block.Kind == string(api.Machine) {
		return nil
	}
	if api.IsClusterKind(api.Kind(block.Kind)) && len(block.ClusterOwners) == 0 {
		return []string{block.Object}
	}
	return block.ClusterOwners
}

func (s Selection) MissingTargets(plan Plan) []string {
	found := map[string]bool{}
	for _, block := range plan.Blocks {
		if len(s.Clusters) != 0 {
			for _, name := range clusterOwners(plan, block) {
				found[name] = true
			}
		}
		if len(s.Machines) != 0 && block.Kind == string(api.Machine) {
			found[block.Object] = true
		}
	}
	var missing []string
	for _, name := range append(slices.Clone(s.Clusters), s.Machines...) {
		if !found[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

func (s Selection) Bind(plan Plan) (Selection, bool) {
	s.within, s.precursors, s.targets, s.joint = nil, nil, nil, nil
	var roots []string
	if s.Targeted() {
		s.joint = map[string]bool{}
		for _, block := range plan.Blocks {
			owners := clusterOwners(plan, block)
			if len(owners) > 1 && slices.ContainsFunc(owners, func(name string) bool { return !slices.Contains(s.Clusters, name) }) {
				s.joint[block.ID] = true
			}
			if block.Kind == string(api.Machine) && slices.Contains(s.Machines, block.Object) ||
				len(owners) != 0 && !slices.ContainsFunc(owners, func(name string) bool { return !slices.Contains(s.Clusters, name) }) {
				roots = append(roots, block.ID)
			}
		}
		s.targets = dependencyClosure(plan, roots)
	}
	if s.Until != "" {
		var target string
		for _, block := range plan.Blocks {
			if untilTarget(block) && block.Object == s.Until {
				if target != "" || s.Targeted() && !s.targets[block.ID] {
					return s, false
				}
				target = block.ID
			}
		}
		if target == "" {
			return s, false
		}
		s.within = dependencyClosure(plan, []string{target})
	}
	if len(s.Stages) != 0 && s.Stages.Selects(StageController) {
		var controller []string
		for _, block := range plan.Blocks {
			if block.Stage == StageController && s.inScope(block) {
				controller = append(controller, block.Dependencies...)
			}
		}
		s.precursors = dependencyClosure(plan, controller)
	}
	return s, true
}

func dependencyClosure(plan Plan, queue []string) map[string]bool {
	blocks := make(map[string][]string, len(plan.Blocks))
	for _, block := range plan.Blocks {
		blocks[block.ID] = block.Dependencies
	}
	within := map[string]bool{}
	for i := 0; i < len(queue); i++ {
		id := queue[i]
		if !within[id] {
			within[id] = true
			queue = append(queue, blocks[id]...)
		}
	}
	return within
}

func (s Selection) inScope(block Block) bool {
	return !s.joint[block.ID] && (!s.Targeted() || s.targets[block.ID]) && (s.Until == "" || s.within[block.ID])
}

func (s Selection) Admits(block Block) bool {
	return s.inScope(block) && (s.Stages.Selects(block.Stage) || s.precursors[block.ID])
}

func untilTarget(block Block) bool {
	cluster, instance, found := strings.Cut(block.Object, ".")
	return block.Stage == StageAddOns && found && ValidSegment(cluster) && ValidSegment(instance)
}

func UntilCandidates(plan Plan) []string {
	objects := []string{}
	for _, block := range plan.Blocks {
		if untilTarget(block) && !slices.Contains(objects, block.Object) {
			objects = append(objects, block.Object)
		}
	}
	slices.Sort(objects)
	return objects
}

func (s StageSelection) Names() []string {
	names := make([]string, 0, len(s))
	for _, stage := range s {
		names = append(names, string(stage))
	}
	return names
}

// Ready lists the blocks an operation may work on next: pending blocks whose
// every dependency is durably done, in frozen plan order.
func Ready(plan Plan, states map[string]BlockState) []Block {
	ready := make([]Block, 0, len(plan.Blocks))
	for _, block := range plan.Blocks {
		if blockState(states, block.ID) != BlockPending || waitingOn(plan, states, block) != "" {
			continue
		}
		ready = append(ready, cloneBlock(block))
	}
	return ready
}

// Startable narrows Ready to the blocks this invocation admits. Everything
// else stays pending; a selection never fails a block.
func Startable(plan Plan, states map[string]BlockState, selection Selection) []Block {
	startable := make([]Block, 0, len(plan.Blocks))
	for _, block := range Ready(plan, states) {
		if selection.Admits(block) {
			startable = append(startable, block)
		}
	}
	return startable
}

type DeferralReason string

const (
	DeferredNotSelected DeferralReason = "not-selected"
	DeferredWaiting     DeferralReason = "waits-on"
)

// Deferral explains why a pending block cannot start in this invocation.
type Deferral struct {
	Reason DeferralReason
	Block  string
	Stage  Stage
}

// Deferrals explains every pending block a selection cannot start, so a
// preview can show the operator what a wider selection would unblock.
func Deferrals(plan Plan, states map[string]BlockState, selection Selection) map[string]Deferral {
	deferrals := map[string]Deferral{}
	for _, block := range plan.Blocks {
		if blockState(states, block.ID) != BlockPending {
			continue
		}
		if !selection.Admits(block) {
			deferrals[block.ID] = Deferral{Reason: DeferredNotSelected, Stage: block.Stage}
			continue
		}
		blocking := waitingOn(plan, states, block)
		if blocking == "" {
			continue
		}
		stage := Stage("")
		if dependency, ok := plan.Block(blocking); ok {
			stage = dependency.Stage
		}
		deferrals[block.ID] = Deferral{Reason: DeferredWaiting, Block: blocking, Stage: stage}
	}
	return deferrals
}

// OwnedSubset is the part of an apply's plan the context still holds: every
// block it started. A block that never started has nothing to remove, while a
// block permitted to change its target may have changed anything up to the
// point it stopped, whatever stopped it, and only its own inverse proves that
// absent. Ownership therefore does not move as an unproved block is resolved,
// so a removal planned from this set stays exactly as wide once every effect
// it covers is proved.
func OwnedSubset(plan Plan, states map[string]BlockState) Plan {
	return subset(plan, states, func(state BlockState) bool { return state != BlockPending })
}

// RemainingSubset is the part of a removal plan that is not yet proved gone, so
// a fresh removal covers exactly what an incomplete one left behind.
func RemainingSubset(plan Plan, states map[string]BlockState) Plan {
	return subset(plan, states, func(state BlockState) bool { return state != BlockDone })
}

func subset(plan Plan, states map[string]BlockState, keep func(BlockState) bool) Plan {
	blocks := make([]Block, 0, len(plan.Blocks))
	for _, block := range plan.Blocks {
		if keep(blockState(states, block.ID)) {
			blocks = append(blocks, cloneBlock(block))
		}
	}
	selected := Plan{Verb: plan.Verb, Blocks: blocks}
	if plan.Preparation != nil {
		scope := *plan.Preparation
		scope.Targets = slices.Clone(scope.Targets)
		selected.Preparation = &scope
	}
	return selected
}

// waitingOn names the first dependency, in frozen order, that is not yet done.
func waitingOn(plan Plan, states map[string]BlockState, block Block) string {
	for _, candidate := range plan.Blocks {
		if !slices.Contains(block.Dependencies, candidate.ID) {
			continue
		}
		if blockState(states, candidate.ID) != BlockDone {
			return candidate.ID
		}
	}
	return ""
}

func blockState(states map[string]BlockState, id string) BlockState {
	if state, ok := states[id]; ok && state != "" {
		return state
	}
	return BlockPending
}
