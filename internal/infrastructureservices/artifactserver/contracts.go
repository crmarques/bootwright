package artifactserver

import (
	"context"

	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
)

// Runner performs one authorized artifact-server operation through the fixed
// Ansible entrypoint. It chooses no target, implementation or workflow, and
// returns bounded structured evidence the capability validates strictly.
type Runner interface {
	Run(context.Context, managedservice.RunRequest) (managedservice.RunResult, error)
}
