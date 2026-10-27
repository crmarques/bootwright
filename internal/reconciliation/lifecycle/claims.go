package lifecycle

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// claims refuses an object two implementations of one family both select.
type claims struct {
	holders map[string]string
	refused []Refusal
}

func (c *claims) record(bound CapabilityBinding, definitions []reconciliation.BlockDefinition) {
	if c.holders == nil {
		c.holders = map[string]string{}
	}
	family := bound.family()
	for _, definition := range definitions {
		key := family + "\x00" + definition.Kind + "\x00" + definition.Object
		holder, held := c.holders[key]
		if !held {
			c.holders[key] = bound.Implementation
			continue
		}
		if holder == bound.Implementation {
			continue
		}
		identity := definition.Kind + "/" + definition.Object
		c.refused = append(c.refused, Refusal{
			Kind: definition.Kind, Name: definition.Object,
			Reason: "two implementations of this executable claim " + identity + ": " + holder + " and " + bound.Implementation,
			Remediation: "remove " + identity + " from the selected Environment, or use an example within the supported shape such as " + supportedExample +
				"; each object is realized by exactly one implementation",
		})
	}
}

func (c *claims) failure() error {
	if len(c.refused) == 0 {
		return nil
	}
	return unsupportedFailure(SortRefusals(c.refused))
}

func (b CapabilityBinding) family() string {
	if b.Family == "" {
		return b.Kind
	}
	return b.Family
}
