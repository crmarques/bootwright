package lifecycle

import (
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func (s Service) dependOnStageTails(definitions []reconciliation.BlockDefinition) ([]reconciliation.BlockDefinition, error) {
	result := slices.Clone(definitions)
	for i, definition := range definitions {
		capability, ok := s.capabilities.Resolve(definition.Kind, definition.Implementation)
		if !ok {
			continue
		}
		tail, ok := capability.(StageTailConsumer)
		if !ok {
			continue
		}
		reference, last, err := tail.StageTail(definition)
		if err != nil {
			return nil, err
		}
		if !last {
			continue
		}
		if reference.Kind == "" || reference.Object == "" || !slices.Contains(definition.Requires, reference) {
			return nil, failure("lifecycle.state", "block "+definition.ID+" has an invalid stage tail reference", "make the tail depend on the object whose consumers it follows")
		}
		for _, other := range definitions {
			if other.ID != definition.ID && other.Stage == definition.Stage && slices.Contains(other.Requires, reference) {
				result[i].Dependencies = append(slices.Clone(result[i].Dependencies), other.ID)
			}
		}
		slices.Sort(result[i].Dependencies)
		result[i].Dependencies = slices.Compact(result[i].Dependencies)
	}
	return result, nil
}
