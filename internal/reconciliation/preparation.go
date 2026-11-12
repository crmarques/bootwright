package reconciliation

import (
	"fmt"
	"regexp"
	"slices"
)

// PreparationScope binds the exact purpose and selected resources.
type PreparationScope struct {
	Command              string   `json:"command"`
	Kind                 string   `json:"kind"`
	Targets              []string `json:"targets"`
	PreparePrerequisites bool     `json:"preparePrerequisites"`
}

var preparationCommandName = regexp.MustCompile(`^[a-z][a-z-]*(?: [a-z][a-z-]*){0,2}$`)

var preparationName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func (p PreparationScope) Validate() error {
	if !preparationCommandName.MatchString(p.Command) || !preparationName.MatchString(p.Kind) || len(p.Targets) == 0 || len(p.Targets) > MaxMachines {
		return fmt.Errorf("preparation purpose or targets are invalid")
	}
	if !slices.IsSorted(p.Targets) || len(slices.Compact(slices.Clone(p.Targets))) != len(p.Targets) {
		return fmt.Errorf("preparation targets must be unique and canonically ordered")
	}
	for _, target := range p.Targets {
		if !preparationName.MatchString(target) {
			return fmt.Errorf("preparation target is invalid")
		}
	}
	return nil
}

func (p Plan) WithPreparation(scope *PreparationScope) (Plan, error) {
	if scope == nil {
		p.Preparation = nil
		return p, nil
	}
	if p.Verb != Apply {
		return Plan{}, fmt.Errorf("a removal cannot prepare resources")
	}
	if err := scope.Validate(); err != nil {
		return Plan{}, err
	}
	copy := *scope
	copy.Targets = slices.Clone(scope.Targets)
	p.Preparation = &copy
	return p, nil
}
