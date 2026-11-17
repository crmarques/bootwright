package lifecycle

import (
	"context"
	"maps"
	"slices"

	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	PrepareCredentialsOperation = "prepare_credentials"
	UpdateCredentialsOperation  = "update_credentials"
)

type AdminCredentialCall struct {
	Runner  AdminCredentialRunner
	Adapter string
	Bind    func(context.Context, Execution) (Execution, Invocation, func(), error)
	Guard   func(secrets.Material) (secrets.Material, error)
	Missing error
}

func PrepareAdminCredential(ctx context.Context, a AdminCredentialCall, e Execution) (secrets.Material, error) {
	result, err := runAdminCredential(ctx, a, e, PrepareCredentialsOperation, secrets.Material{})
	defer ClearProduced(result.Produced)
	if err != nil {
		return secrets.Material{}, err
	}
	if !AdminCredentialProved(result.Evidence, e.Block.RequestDigest) {
		return secrets.Material{}, failure("access.unavailable", "the administrator credential preparation was not proved", "check the cluster's original administrator account")
	}
	for _, output := range result.Produced {
		if output.Name == "guard" {
			return a.Guard(output.Material)
		}
	}
	return secrets.Material{}, a.Missing
}

func UpdateAdminCredential(ctx context.Context, a AdminCredentialCall, e Execution, update secrets.Material) error {
	result, err := runAdminCredential(ctx, a, e, UpdateCredentialsOperation, update)
	ClearProduced(result.Produced)
	if err != nil {
		return err
	}
	if !AdminCredentialProved(result.Evidence, e.Block.RequestDigest) {
		return failure("access.unavailable", "the administrator credential update was not proved", "repeat bootwright cluster update-admin-credentials to finish the retained update")
	}
	return nil
}

func runAdminCredential(ctx context.Context, a AdminCredentialCall, e Execution, operation string, update secrets.Material) (RunResult, error) {
	if a.Runner == nil {
		return RunResult{}, failure("lifecycle.state", "the "+a.Adapter+" adapter is not configured", "")
	}
	execution, call, cleanup, err := a.Bind(ctx, e)
	if err != nil {
		return RunResult{}, err
	}
	defer cleanup()
	if call.Placement.Machine == "" || call.Placement != (machineref.Placement{Connection: machineref.ConnectionLocal, Machine: call.Placement.Machine}) {
		return RunResult{}, failure("lifecycle.state", "the frozen administrator credential request is placed off the controller", "")
	}
	execution.Material = maps.Clone(execution.Material)
	if execution.Material == nil {
		execution.Material = map[string]secrets.Material{}
	}
	execution.Material["admin-update"] = update
	call.Operation = operation
	if operation == PrepareCredentialsOperation {
		call.Outputs = []OutputFile{{Name: "guard", Variable: "guard"}}
	} else {
		call.Materials = append(slices.Clone(call.Materials), MaterialFile{Name: "admin-update", Secret: "admin-update", Part: secrets.ValuePart, Variable: "update"})
	}
	run := RunFor(execution, call)
	run.ReadOnly = operation == PrepareCredentialsOperation
	run.Unrecorded = false
	return a.Runner.Run(ctx, run)
}
