package reconciliation

import (
	"maps"
	"slices"
)

func Lineage(plans ...Plan) (Plan, error) {
	if len(plans) == 0 {
		return Plan{}, planError("a lineage needs at least one plan")
	}
	held := map[string]bool{}
	var definitions []BlockDefinition
	for _, plan := range plans {
		if plan.Verb != Apply {
			return Plan{}, planError("only apply plans make a lineage")
		}
		earlier := maps.Clone(held)
		for _, block := range plan.Blocks {
			if held[block.ID] {
				return Plan{}, planError("lifecycle lineage repeats the block " + block.ID)
			}
			definition := clone(block.BlockDefinition)
			for _, prerequisite := range definition.Prerequisites {
				if !earlier[prerequisite] {
					return Plan{}, planError("lifecycle block " + block.ID + " has the prerequisite " + prerequisite + ", which no earlier plan holds")
				}
			}
			definition.Dependencies = append(definition.Dependencies, definition.Prerequisites...)
			slices.Sort(definition.Dependencies)
			definition.Dependencies = slices.Compact(definition.Dependencies)
			definition.Prerequisites = nil
			definitions = append(definitions, definition)
			held[block.ID] = true
		}
	}
	return NewPlan(Apply, definitions)
}

func Additive(fresh Plan, owned ...Plan) (Plan, error) {
	lineage, err := Lineage(owned...)
	if err != nil {
		return Plan{}, err
	}
	if fresh.Verb != Apply {
		return Plan{}, planError("only an apply plan can be added")
	}
	if first, differs := Compare(lineage, fresh).First(); differs && first.Change != Added {
		return Plan{}, planError("lifecycle block " + first.Block + " would be frozen differently from the plan it was frozen in")
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
	return NewPlan(Apply, definitions)
}
