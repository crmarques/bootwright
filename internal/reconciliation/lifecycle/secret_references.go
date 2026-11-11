package lifecycle

import (
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func SecretNames(references ...string) []string {
	names := slices.DeleteFunc(slices.Clone(references), func(name string) bool { return name == "" })
	slices.Sort(names)
	return slices.Compact(names)
}

const unnamedSecretRemedy = "this build is defective: the capability must name every Secret it binds in the SecretReferences of a block it plans; report it with this message"

func refuseUnnamedSecrets(implementation string, capability Capability, contribution CapabilityPlan) error {
	if len(SecretNames(contribution.Secrets...)) == 0 {
		return nil
	}
	referencer, names := capability.(SecretReferencer)
	if !names {
		return failure("lifecycle.state", "the "+implementation+" capability binds Secret "+SecretNames(contribution.Secrets...)[0]+" but cannot name the Secrets its blocks reference", unnamedSecretRemedy)
	}
	named := map[string]bool{}
	for _, definition := range contribution.Definitions {
		references, err := referencer.SecretReferences(reconciliation.Block{BlockDefinition: definition})
		if err != nil {
			return failure("lifecycle.state", "the "+implementation+" capability cannot name the Secrets block "+definition.ID+" references", unnamedSecretRemedy)
		}
		for _, reference := range references {
			named[reference] = true
		}
	}
	for _, reference := range SecretNames(contribution.Secrets...) {
		if !named[reference] {
			return failure("lifecycle.state", "the "+implementation+" capability binds Secret "+reference+", which none of its "+blockCount(contribution.Definitions)+" names", unnamedSecretRemedy)
		}
	}
	return nil
}

func blockCount(definitions []reconciliation.BlockDefinition) string {
	switch len(definitions) {
	case 0:
		return "blocks (it planned none)"
	case 1:
		return "blocks (" + definitions[0].ID + ")"
	}
	return "blocks (" + definitions[0].ID + " and others)"
}
