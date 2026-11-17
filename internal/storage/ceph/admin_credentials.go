package ceph

import (
	"context"
	"time"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const AdminPasswordOutput = "admin-password"

const adminCredentialDeadline = 3 * time.Minute

func (c Capability) AdminCredentialSeed(reconciliation.Block) string { return DashboardOutput }

func (c Capability) AdminCredential(block reconciliation.Block) (string, string, bool, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return "", "", false, err
	}
	return AdminPasswordOutput, "admin", request.Action == "cluster", nil
}

func (c Capability) PrepareAdminCredential(ctx context.Context, e lifecycle.Execution) (secrets.Material, error) {
	return lifecycle.PrepareAdminCredential(ctx, c.adminCredentialCall(), e)
}

func (c Capability) UpdateAdminCredential(ctx context.Context, e lifecycle.Execution, update secrets.Material) error {
	return lifecycle.UpdateAdminCredential(ctx, c.adminCredentialCall(), e, update)
}

func (c Capability) adminCredentialCall() lifecycle.AdminCredentialCall {
	return lifecycle.AdminCredentialCall{Runner: c.runner, Adapter: "Ceph", Bind: c.bindAdminCredential, Guard: dashboardGuard,
		Missing: failure("access.unavailable", "Ceph returned no administrator credential identity", "check the original dashboard administrator account")}
}

func (c Capability) bindAdminCredential(_ context.Context, e lifecycle.Execution) (lifecycle.Execution, lifecycle.Invocation, func(), error) {
	request, err := DecodeRequest(e.Block.Request)
	if err != nil {
		return e, lifecycle.Invocation{}, nil, err
	}
	proof, err := decodeEvidence(e.PreviousEvidence)
	if err != nil || !validIncarnation(proof.Incarnation) {
		return e, lifecycle.Invocation{}, nil, evidenceFailure()
	}
	execution, files, cleanup, err := boundMaterial(e, request, "observe", c.proofs)
	if err != nil {
		return e, lifecycle.Invocation{}, nil, err
	}
	return execution, lifecycle.Invocation{Implementation: Implementation, Variable: "bootwright_ceph", Canonical: e.Block.Request,
		Placement: machine.Placement{Connection: machine.ConnectionLocal, Machine: request.Controller}, Materials: files, Values: map[string]string{"incarnation": proof.Incarnation},
		Refusals: adapterRefusals(), Deadline: adminCredentialDeadline}, cleanup, nil
}

func dashboardGuard(guard secrets.Material) (secrets.Material, error) {
	value, _ := guard.Part(secrets.ValuePart)
	defer clear(value)
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: value}), nil
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
