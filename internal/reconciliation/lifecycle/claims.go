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
	for _, definition := range definitions {
		c.hold(bound, definition.Kind, definition.Object)
	}
}

func (c *claims) hold(bound CapabilityBinding, kind, name string) {
	if c.holders == nil {
		c.holders = map[string]string{}
	}
	key := bound.family() + "\x00" + kind + "\x00" + name
	holder, held := c.holders[key]
	if !held {
		c.holders[key] = bound.Implementation
		return
	}
	if holder == bound.Implementation {
		return
	}
	identity := kind + "/" + name
	c.refused = append(c.refused, Refusal{
		Kind: kind, Name: name,
		Reason: "two implementations of this executable claim " + identity + ": " + holder + " and " + bound.Implementation,
		Remediation: "remove " + identity + " from the selected Environment, or use an example within the supported shape such as " + supportedExample +
			"; each object is realized by exactly one implementation",
	})
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
