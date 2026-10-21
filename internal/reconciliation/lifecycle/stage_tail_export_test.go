package lifecycle

import "github.com/crmarques/bootwright/internal/reconciliation"

func StageTailDependenciesForTest(bound []BoundCapability, definitions []reconciliation.BlockDefinition) ([]reconciliation.BlockDefinition, error) {
	service := Service{capabilities: sharedResolver{bound: bound}}
	return service.dependOnStageTails(definitions)
}
