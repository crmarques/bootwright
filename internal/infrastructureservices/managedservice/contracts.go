package managedservice

import "context"

// Runner performs one authorized managed-service operation through the fixed
// Ansible entrypoint. It chooses no target, implementation or workflow, and
// returns bounded structured evidence the capability validates strictly.
type Runner interface {
	Run(context.Context, RunRequest) (RunResult, error)
}
