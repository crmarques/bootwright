package installation

import (
	"context"

	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Runner performs one authorized installation operation through the fixed
// Ansible entrypoint. It chooses no target, implementation or workflow, and
// returns bounded structured evidence the capability validates strictly.
type Runner interface {
	Run(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error)
}

// Identities reads the identity a physical Machine's own block proved earlier
// in this operation, from the evidence the attempt was handed. That evidence
// is another capability's, so its owner decodes it and this capability asks
// only for the answer. A Machine with no pin reports false, never an error.
type Identities interface {
	PinnedIdentity(name string, proved []lifecycle.BlockEvidence) (machineref.HardwareIdentity, bool, error)
}

// MediaRecord is one image the host media store publishes: the size and
// SHA-256 its record names, and Observed, the size of the bytes the store
// holds now. Failure is the store's cause for an image it lists but cannot
// read; when it is set the other fields carry nothing a plan may freeze.
type MediaRecord struct {
	SHA256         string
	Size, Observed int64
	Failure        string
}

// MediaRecords reads every image the host media store publishes, keyed by its
// store name, so a plan freezes each image an installation uses at its record.
type MediaRecords interface {
	MediaRecords(context.Context) (map[string]MediaRecord, error)
}
