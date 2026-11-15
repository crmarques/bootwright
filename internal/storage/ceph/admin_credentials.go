package ceph

import (
	"context"
	"maps"
	"time"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const AdminPasswordOutput = "admin-password"

func (c Capability) AdminCredentialSeed(reconciliation.Block) string { return DashboardOutput }

func (c Capability) AdminCredential(block reconciliation.Block) (string, string, bool, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return "", "", false, err
	}
	return AdminPasswordOutput, "admin", request.Action == "cluster", nil
}

func (c Capability) PrepareAdminCredential(ctx context.Context, e lifecycle.Execution) (secrets.Material, error) {
	result, err := c.adminCredentialRun(ctx, e, "prepare_credentials", secrets.Material{})
	defer lifecycle.ClearProduced(result.Produced)
	if err != nil {
		lifecycle.ClearProduced(result.Produced)
		return secrets.Material{}, err
	}
	if !lifecycle.AdminCredentialProved(result.Evidence, e.Block.RequestDigest) {
		return secrets.Material{}, failure("access.unavailable", "the administrator credential preparation was not proved", "check the cluster's original administrator account")
	}
	for _, output := range result.Produced {
		if output.Name == "guard" {
			value, _ := output.Material.Part(secrets.ValuePart)
			defer clear(value)
			return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: value}), nil
		}
	}
	return secrets.Material{}, failure("access.unavailable", "Ceph returned no administrator credential identity", "check the original dashboard administrator account")
}

func (c Capability) UpdateAdminCredential(ctx context.Context, e lifecycle.Execution, update secrets.Material) error {
	result, err := c.adminCredentialRun(ctx, e, "update_credentials", update)
	lifecycle.ClearProduced(result.Produced)
	if err != nil {
		return err
	}
	if !lifecycle.AdminCredentialProved(result.Evidence, e.Block.RequestDigest) {
		return failure("access.unavailable", "the administrator credential update was not proved", "repeat bootwright cluster update-admin-credentials to finish the retained update")
	}

	return nil
}

func (c Capability) adminCredentialRun(ctx context.Context, e lifecycle.Execution, operation string, update secrets.Material) (lifecycle.RunResult, error) {
	request, err := DecodeRequest(e.Block.Request)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	proof, err := decodeEvidence(e.PreviousEvidence)
	if err != nil || !validIncarnation(proof.Incarnation) {
		return lifecycle.RunResult{}, evidenceFailure()
	}
	execution, files, cleanup, err := boundMaterial(e, request, "observe", c.proofs)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	defer cleanup()
	execution.Material = maps.Clone(execution.Material)
	if execution.Material == nil {
		execution.Material = map[string]secrets.Material{}
	}
	execution.Material["admin-update"] = update
	call := lifecycle.Invocation{Implementation: Implementation, Operation: operation, Variable: "bootwright_ceph", Canonical: e.Block.Request,
		Placement: machine.Placement{Connection: machine.ConnectionLocal, Machine: request.Controller}, Materials: files, Values: map[string]string{"incarnation": proof.Incarnation}, Deadline: 3 * time.Minute}
	if operation == "prepare_credentials" {
		call.Outputs = []lifecycle.OutputFile{{Name: "guard", Variable: "guard"}}
	} else {
		call.Materials = append(call.Materials, lifecycle.MaterialFile{Name: "admin-update", Secret: "admin-update", Part: secrets.ValuePart, Variable: "update"})
	}
	run := lifecycle.RunFor(execution, call)
	run.ReadOnly = operation == "prepare_credentials"
	run.Unrecorded = false
	return c.runner.Run(ctx, run)
}

func (c Capability) AdminCredentialIdentity(_ context.Context, e lifecycle.Execution) (string, error) {
	request, err := DecodeRequest(e.Block.Request)
	if err != nil {
		return "", err
	}
	proof, err := decodeEvidence(e.PreviousEvidence)
	if err != nil || !validIncarnation(proof.Incarnation) {
		return "", evidenceFailure()
	}
	return "cluster fsid " + request.FSID + " created " + proof.Incarnation + " as dashboard user " + DashboardUser, nil
}

func (c Capability) AdminCredentialMayBeMissing() bool { return false }
