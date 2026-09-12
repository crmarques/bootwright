package artifactserver

import "context"

// Runner performs one authorized artifact-server operation through the fixed
// Ansible entrypoint. It chooses no target, implementation or workflow, and
// returns bounded structured evidence the capability validates strictly.
type Runner interface {
	Run(context.Context, RunRequest) (RunResult, error)
}
