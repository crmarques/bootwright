package agentinstall

import (
	"context"
	"encoding/json"
	"maps"
	"time"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func (c InstallCapability) AdminCredentialSeed(reconciliation.Block) string {
	return ConsolePasswordOutput
}

func (c InstallCapability) AdminCredential(block reconciliation.Block) (string, string, bool, error) {
	_, err := DecodeInstallRequest(block.Request)
	return AdminPasswordOutput, "kubeadmin", err == nil, err
}

const adminCredentialDeadline = 2 * time.Minute

func (c InstallCapability) PrepareAdminCredential(ctx context.Context, e lifecycle.Execution) (secrets.Material, error) {
	return lifecycle.PrepareAdminCredential(ctx, c.adminCredentialCall(), e)
}

func (c InstallCapability) UpdateAdminCredential(ctx context.Context, e lifecycle.Execution, update secrets.Material) error {
	return lifecycle.UpdateAdminCredential(ctx, c.adminCredentialCall(), e, update)
}

func (c InstallCapability) adminCredentialCall() lifecycle.AdminCredentialCall {
	return lifecycle.AdminCredentialCall{Runner: c.runner, Adapter: "installation", Bind: bindAdminCredential, Guard: c.consoleGuard,
		Missing: refusal("access.unavailable", "the cluster returned no console credential identity", "check that its original kubeadmin account still exists")}
}

func bindAdminCredential(ctx context.Context, e lifecycle.Execution) (lifecycle.Execution, lifecycle.Invocation, func(), error) {
	request, err := DecodeInstallRequest(e.Block.Request)
	if err != nil {
		return e, lifecycle.Invocation{}, nil, err
	}
	access, err := e.LendProduced(ctx, lifecycle.ProducedInput{Kind: Kind, Object: e.Block.Object, Output: KubeconfigOutput})
	if err != nil {
		return e, lifecycle.Invocation{}, nil, err
	}
	client, err := ToolPath(ctx, e, Tool{Compatibility: request.Release.Distribution, Kind: clientTool, Version: request.Release.Version}, "oc")
	if err != nil {
		access.Clear()
		return e, lifecycle.Invocation{}, nil, err
	}
	e.Material = maps.Clone(e.Material)
	if e.Material == nil {
		e.Material = map[string]secrets.Material{}
	}
	e.Material["admin-access"] = access
	return e, lifecycle.Invocation{Implementation: InstallImplementation, Variable: "bootwright_cluster_admin", Canonical: e.Block.Request,
		Placement: request.Placement, Deadline: adminCredentialDeadline, Refusals: runtimeRefusals(e.Context, e.Continuation, request),
		Materials: []lifecycle.MaterialFile{{Name: "admin-access", Secret: "admin-access", Part: secrets.ValuePart, Variable: "kubeconfig"}}, Values: map[string]string{"client": client},
	}, access.Clear, nil
}

func (c InstallCapability) consoleGuard(guard secrets.Material) (secrets.Material, error) {
	value, _ := guard.Part(secrets.ValuePart)
	defer clear(value)
	var prepared struct {
		Guard    json.RawMessage `json:"guard"`
		Password string          `json:"password"`
	}
	if canonicaljson.DecodeClosed(value, &prepared) != nil || len(prepared.Password) < 23 || len(prepared.Password) > 64 {
		return secrets.Material{}, refusal("access.unavailable", "console credential preparation was invalid", "repeat the credential update")
	}
	password := prepared.Password
	if c.hashPassword == nil {
		return secrets.Material{}, refusal("access.unavailable", "password hashing is not configured", "configure the administrator credential implementation")
	}
	hash, err := c.hashPassword([]byte(password))
	if err != nil {
		return secrets.Material{}, refusal("secret.input", "password generation failed", "repeat the credential update")
	}
	encoded, err := json.Marshal(struct {
		Guard    json.RawMessage `json:"guard"`
		Password string          `json:"password"`
		Hash     string          `json:"hash"`
	}{prepared.Guard, password, string(hash)})
	if err != nil {
		return secrets.Material{}, err
	}
	defer clear(encoded)
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: encoded}), nil
}

func (c InstallCapability) AdminCredentialIdentity(ctx context.Context, e lifecycle.Execution) (string, error) {
	held, err := e.LendProduced(ctx, lifecycle.ProducedInput{Kind: Kind, Object: e.Block.Object, Output: KubeconfigOutput})
	if err != nil {
		return "", err
	}
	defer held.Clear()
	value, _ := held.Part(secrets.ValuePart)
	defer clear(value)
	return kubeconfigIdentity(value)
}

func (c InstallCapability) AdminCredentialMayBeMissing() bool { return true }

func (c InstallCapability) WithPasswordHash(hash func([]byte) ([]byte, error)) InstallCapability {
	c.hashPassword = hash
	return c
}
