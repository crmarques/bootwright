package agentinstall

import (
	"context"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Runner performs one authorized operation through the fixed Ansible
// entrypoint. It chooses no target, implementation or workflow, and returns
// bounded structured evidence the capability validates strictly.
type Runner interface {
	Run(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error)
}

// Identities reads the identity a physical node's own Machine block proved
// earlier in this operation, from the evidence the attempt was handed. That
// evidence is another capability's, so its owner decodes it and this
// capability asks only for the answer. A Machine with no pin reports false,
// never an error.
type Identities interface {
	PinnedIdentity(name string, proved []lifecycle.BlockEvidence) (machine.HardwareIdentity, bool, error)
}
