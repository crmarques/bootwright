package installation

import (
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type DiscoveredRootProof func(lifecycle.Execution, Request) error

func (c Capability) WithDiscoveredRootProof(proof DiscoveredRootProof) Capability {
	c.discoveredRootProof = proof
	return c
}

func (c Capability) proveDiscoveredRoot(e lifecycle.Execution, r Request) error {
	if r.DiscoveryProof == nil {
		return nil
	}
	if c.discoveredRootProof == nil {
		return refusal("lifecycle.state", "the frozen root has no matching completed inventory dependency", "restore the original discovery proof before continuing installation")
	}
	return c.discoveredRootProof(e, r)
}
