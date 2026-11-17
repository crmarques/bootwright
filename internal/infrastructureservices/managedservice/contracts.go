package managedservice

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Runner performs one authorized managed-service operation through the fixed
// Ansible entrypoint. It chooses no target, implementation or workflow, and
// returns bounded structured evidence the capability validates strictly.
type Runner interface {
	Run(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error)
}

const Family = "managed-service"

// Frozen is one kind's frozen request, and Frame what the shared lifecycle
// reads of it: a kind keeps its own wire shape only where it truly differs.
type Frozen interface {
	Canonical() ([]byte, error)
	Frame() Frame
}

// Reported is one kind's evidence: the part every kind reports alike, and the
// kind's own proof that its listeners answered.
type Reported[R any] interface {
	Observed() Observed
	Ready(request R, fingerprint string) error
}
