package reconciliation

import (
	"slices"
)

type Life string

const (
	LifeAbsent       Life = "absent"
	LifeRealizing    Life = "realizing"
	LifeRealized     Life = "realized"
	LifeUnproved     Life = "unproved"
	LifeRemoving     Life = "removing"
	LifeLeaving      Life = "leaving"
	LifeRetained     Life = "retained"
	LifeAbandoned    Life = "abandoned"
	LifePartial      Life = "partial"
	LifeInconclusive Life = "inconclusive"
	LifeNoEffect     Life = "no-effect"
)

func LifeAfter(prior Life, verb Verb, state BlockState, effect EffectState, inverse *Inverse) Life {
	switch {
	case state == "" || state == BlockPending:
		return prior
	case state == BlockRunning && verb == Apply:
		return LifeRealizing
	case state == BlockRunning:
		return LifeRemoving
	case state == BlockDone && verb == Apply:
		return LifeRealized
	case state == BlockFailed && verb == Destroy && effect == EffectPartial:
		return LifePartial
	case state == BlockFailed && verb == Destroy && effect == EffectInconclusive:
		return LifeInconclusive
	case state == BlockFailed && verb == Destroy && effect == EffectNoEffect:
		return LifeNoEffect
	case state != BlockDone:
		return LifeUnproved
	case effect == EffectNoEffect:
		return LifeAbandoned
	case inverse != nil && len(inverse.Retains) != 0:
		return LifeRetained
	case inverse != nil && len(inverse.LeavesWith) != 0:
		return LifeLeaving
	}
	return LifeAbsent
}

type Member struct {
	Operation string
	Plan      Plan
	States    map[string]BlockState
	Effects   map[string]EffectState
	Completed bool
	TakenBack []string
	Whole     bool
}

type Holding struct {
	Block       Block
	Created     Block
	Holder      int
	Incarnation int
	State       BlockState
	Life        Life
}

type Ownership struct {
	members  []Member
	holdings []Holding
	at       map[string]int
	damage   error
}

func Fold(members ...Member) Ownership {
	o := Ownership{members: members, at: map[string]int{}}
	for index, member := range members {
		if member.Plan.Verb == Apply {
			o.apply(index)
			continue
		}
		o.remove(index)
	}
	return o
}

func LineageOf(members ...Member) (Plan, error) { return Fold(members...).Plan() }

func (o *Ownership) apply(index int) {
	member := o.members[index]
	earlier := map[string]bool{}
	for id, at := range o.at {
		earlier[id] = !o.takenBack(o.holdings[at])
	}
	for _, block := range member.Plan.Blocks {
		at, held := o.at[block.ID]
		if held && !o.takenBack(o.holdings[at]) {
			o.fail(planError("lifecycle lineage repeats the block " + block.ID))
			continue
		}
		if held {
			o.release(block.ID)
		}
		for _, prerequisite := range block.Prerequisites {
			if !earlier[prerequisite] {
				o.fail(planError("lifecycle block " + block.ID + " has the prerequisite " + prerequisite + ", which no earlier plan holds"))
			}
		}
		state := member.States[block.ID]
		o.hold(Holding{Block: block, Created: block, Holder: index, Incarnation: index, State: state,
			Life: LifeAfter(LifeAbsent, Apply, state, member.Effects[block.ID], nil)})
	}
}

func (o *Ownership) remove(index int) {
	member := o.members[index]
	for _, id := range member.TakenBack {
		holding, lingers := Holding{Block: Block{BlockDefinition: BlockDefinition{ID: id}}, Incarnation: -1}, false
		if at, held := o.at[id]; held {
			holding = o.holdings[at]
			lingers = o.members[holding.Holder].Plan.Verb == Destroy && slices.Contains([]Life{LifeRetained, LifeLeaving, LifeAbandoned}, holding.Life)
		}
		if !lingers {
			holding.State, holding.Life = BlockDone, LifeAbsent
		}
		holding.Holder = index
		o.hold(holding)
	}
	for _, block := range member.Plan.Blocks {
		holding := Holding{Incarnation: -1, Life: LifeAbsent}
		if at, held := o.at[block.ID]; held {
			holding = o.holdings[at]
		}
		state := member.States[block.ID]
		holding.Block, holding.Holder, holding.State = block, index, state
		holding.Life = LifeAfter(holding.Life, Destroy, state, member.Effects[block.ID], block.Inverse)
		o.hold(holding)
	}
	for o.settleLeaving() {
	}
	if member.Whole {
		for _, holding := range slices.Clone(o.holdings) {
			if holding.Holder != index {
				o.release(holding.Block.ID)
			}
		}
	}
}

func (o *Ownership) hold(holding Holding) {
	if at, held := o.at[holding.Block.ID]; held {
		o.holdings[at] = holding
		return
	}
	o.at[holding.Block.ID] = len(o.holdings)
	o.holdings = append(o.holdings, holding)
}

func (o *Ownership) release(id string) {
	at := o.at[id]
	o.holdings = slices.Delete(o.holdings, at, at+1)
	delete(o.at, id)
	for index := at; index < len(o.holdings); index++ {
		o.at[o.holdings[index].Block.ID] = index
	}
}

func (o *Ownership) settleLeaving() bool {
	settled := false
	for index, holding := range o.holdings {
		if holding.Life != LifeLeaving || holding.Block.Inverse == nil {
			continue
		}
		if !slices.ContainsFunc(holding.Block.Inverse.LeavesWith, func(id string) bool {
			at, held := o.at[id]
			return !held || !o.takenBack(o.holdings[at])
		}) {
			o.holdings[index].Life, settled = LifeAbsent, true
		}
	}
	return settled
}

func (o *Ownership) fail(err error) {
	if o.damage == nil {
		o.damage = err
	}
}

func (o Ownership) takenBack(h Holding) bool {
	return o.members[h.Holder].Plan.Verb == Destroy && h.State == BlockDone && h.Life == LifeAbsent
}

func (o Ownership) TakenBack(holding Holding) bool { return o.takenBack(holding) }

func (o Ownership) Holding(id string) (Holding, bool) {
	at, held := o.at[id]
	if !held {
		return Holding{}, false
	}
	return o.holdings[at], true
}

func (o Ownership) Holdings() []Holding { return slices.Clone(o.holdings) }

func (o Ownership) Unfinished() []Holding {
	var unfinished []Holding
	for _, holding := range o.holdings {
		if o.members[holding.Holder].Plan.Verb == Apply && holding.State != BlockDone {
			unfinished = append(unfinished, holding)
		}
	}
	return unfinished
}

func (o Ownership) Covered() []Holding {
	var covered []Holding
	for _, holding := range o.holdings {
		holder := o.members[holding.Holder]
		switch {
		case holder.Plan.Verb != Apply && holding.State != BlockDone,
			holder.Plan.Verb == Apply && (holder.Completed || holding.State != "" && holding.State != BlockPending):
			covered = append(covered, holding)
		}
	}
	return covered
}

func (o Ownership) Plan() (Plan, error) {
	if o.damage != nil {
		return Plan{}, o.damage
	}
	last := len(o.members) - 1
	for last >= 0 && o.members[last].Plan.Verb != Apply {
		last--
	}
	if last < 0 {
		return Plan{}, planError("a lineage needs at least one plan")
	}
	var definitions []BlockDefinition
	for _, holding := range o.holdings {
		if holding.Incarnation < 0 || o.takenBack(holding) {
			continue
		}
		definition := clone(holding.Created.BlockDefinition)
		definition.Dependencies = append(definition.Dependencies, definition.Prerequisites...)
		slices.Sort(definition.Dependencies)
		definition.Dependencies = slices.Compact(definition.Dependencies)
		definition.Prerequisites = nil
		definitions = append(definitions, definition)
	}
	result, err := NewPlan(Apply, definitions)
	if err != nil {
		return Plan{}, err
	}
	return result.WithPreparation(o.members[last].Plan.Preparation)
}
