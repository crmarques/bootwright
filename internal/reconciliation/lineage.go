package reconciliation

import (
	"slices"
)

func Lineage(plans ...Plan) (Plan, error) {
	members := make([]Member, 0, len(plans))
	for _, plan := range plans {
		if plan.Verb != Apply {
			if damage := Fold(members...).damage; damage != nil {
				return Plan{}, damage
			}
			return Plan{}, planError("only apply plans make a lineage")
		}
		members = append(members, Member{Plan: plan})
	}
	return Fold(members...).Plan()
}

func Additive(fresh Plan, owned ...Plan) (Plan, error) {
	lineage, err := Lineage(owned...)
	if err != nil {
		return Plan{}, err
	}
	return additive(fresh, lineage)
}

func AdditiveOver(fresh Plan, members ...Member) (Plan, error) {
	lineage, err := LineageOf(members...)
	if err != nil {
		return Plan{}, err
	}
	return additive(fresh, lineage)
}

func additive(fresh, lineage Plan) (Plan, error) {
	if fresh.Verb != Apply {
		return Plan{}, planError("only an apply plan can be added")
	}
	if first, differs := Compare(lineage, fresh).First(); differs && first.Change != Added {
		return Plan{}, recordsLineageReshaped.Fail("lifecycle block "+first.Block+" would be frozen differently from the plan it was frozen in", "")
	}
	held := map[string]bool{}
	for _, block := range lineage.Blocks {
		held[block.ID] = true
	}
	var definitions []BlockDefinition
	for _, block := range fresh.Blocks {
		if held[block.ID] {
			continue
		}
		if len(block.Prerequisites) != 0 {
			return Plan{}, planError("a plan to add already carries prerequisites")
		}
		definition := clone(block.BlockDefinition)
		definition.Dependencies = slices.DeleteFunc(definition.Dependencies, func(id string) bool {
			if !held[id] {
				return false
			}
			definition.Prerequisites = append(definition.Prerequisites, id)
			return true
		})
		slices.Sort(definition.Prerequisites)
		definitions = append(definitions, definition)
	}
	if len(definitions) == 0 {
		return Plan{}, planError("the plan adds no block to the lineage")
	}
	result, err := NewPlan(Apply, definitions)
	if err != nil {
		return Plan{}, err
	}
	return result.WithPreparation(fresh.Preparation)
}
