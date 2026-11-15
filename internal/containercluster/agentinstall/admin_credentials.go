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

func (c InstallCapability) PrepareAdminCredential(ctx context.Context, e lifecycle.Execution) (secrets.Material, error) {
	result, err := c.adminCredentialRun(ctx, e, "prepare_credentials", secrets.Material{})
	defer lifecycle.ClearProduced(result.Produced)
	if err != nil {
		lifecycle.ClearProduced(result.Produced)
		return secrets.Material{}, err
	}
	if !lifecycle.AdminCredentialProved(result.Evidence, e.Block.RequestDigest) {
		return secrets.Material{}, refusal("access.unavailable", "the administrator credential preparation was not proved", "check the cluster's original administrator account")
	}
	for _, output := range result.Produced {
		if output.Name == "guard" {
			defer output.Material.Clear()
			guard, _ := output.Material.Part(secrets.ValuePart)
			defer clear(guard)
			var prepared struct {
				Guard    json.RawMessage `json:"guard"`
				Password string          `json:"password"`
			}
			if canonicaljson.DecodeClosed(guard, &prepared) != nil || len(prepared.Password) < 23 || len(prepared.Password) > 64 {
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
	}
	return secrets.Material{}, refusal("access.unavailable", "the cluster returned no console credential identity", "check that its original kubeadmin account still exists")
}

func (c InstallCapability) UpdateAdminCredential(ctx context.Context, e lifecycle.Execution, update secrets.Material) error {
	result, err := c.adminCredentialRun(ctx, e, "update_credentials", update)
	lifecycle.ClearProduced(result.Produced)
	if err != nil {
		return err
	}
	if !lifecycle.AdminCredentialProved(result.Evidence, e.Block.RequestDigest) {
		return refusal("access.unavailable", "the administrator credential update was not proved", "repeat bootwright cluster update-admin-credentials to finish the retained update")
	}

	return nil
}

func (c InstallCapability) adminCredentialRun(ctx context.Context, e lifecycle.Execution, operation string, update secrets.Material) (lifecycle.RunResult, error) {
	request, err := DecodeInstallRequest(e.Block.Request)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	access, err := e.LendProduced(ctx, lifecycle.ProducedInput{Kind: Kind, Object: e.Block.Object, Output: KubeconfigOutput})
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	defer access.Clear()
	client, err := ToolPath(ctx, e, Tool{Compatibility: request.Release.Distribution, Kind: clientTool, Version: request.Release.Version}, "oc")
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	e.Material = maps.Clone(e.Material)
	if e.Material == nil {
		e.Material = map[string]secrets.Material{}
	}
	e.Material["admin-access"], e.Material["admin-update"] = access, update
	call := lifecycle.Invocation{Implementation: InstallImplementation, Operation: operation, Variable: "bootwright_cluster_admin", Canonical: e.Block.Request,
		Placement: request.Placement, Deadline: 2 * time.Minute,
		Materials: []lifecycle.MaterialFile{{Name: "admin-access", Secret: "admin-access", Part: secrets.ValuePart, Variable: "kubeconfig"}}, Values: map[string]string{"client": client},
	}
	if operation == "prepare_credentials" {
		call.Outputs = []lifecycle.OutputFile{{Name: "guard", Variable: "guard"}}
	} else {
		call.Materials = append(call.Materials, lifecycle.MaterialFile{Name: "admin-update", Secret: "admin-update", Part: secrets.ValuePart, Variable: "update"})
	}
	run := lifecycle.RunFor(e, call)
	run.ReadOnly = operation == "prepare_credentials"
	run.Unrecorded = false
	return c.runner.Run(ctx, run)
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
