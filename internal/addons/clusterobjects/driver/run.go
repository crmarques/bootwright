package driver

import (
	"context"
	"maps"
	"time"

	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type Config struct {
	Placements                                []co.SecretPlacement
	Implementation, Variable, EvidenceVersion string
	Placement                                 machine.Placement
	Tool                                      controller.InstalledTool
	BudgetSeconds                             int64
	ProducedInput                             lifecycle.ProducedInput
}

func Run(ctx context.Context, runner Runner, execution lifecycle.Execution, operation string, config Config) (lifecycle.RunResult, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.RunResult{}, err
	}
	if runner == nil || execution.LendProduced == nil || execution.LocateTool == nil {
		return lifecycle.RunResult{}, failure("cluster object runtime or produced-input custody is unavailable", "apply the controller and cluster stages first")
	}
	if execution.ApplyOperation == "" || execution.ApplyRegistered == "" {
		return lifecycle.RunResult{}, failure("cluster object ownership identity is unavailable", "")
	}
	if operation != "apply" && operation != "observe" {
		return lifecycle.RunResult{}, failure("unsupported cluster object operation", "")
	}
	tool, err := execution.LocateTool(ctx, config.Tool)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	material, err := execution.LendProduced(ctx, config.ProducedInput)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	defer material.Clear()
	execution.Material = maps.Clone(execution.Material)
	if execution.Material == nil {
		execution.Material = map[string]secrets.Material{}
	}
	execution.Material["produced-kubeconfig"] = material
	previous := co.Evidence{}
	if len(execution.PreviousEvidence) > 0 {
		evidence, err := DecodeEvidence(execution.PreviousEvidence, execution.Block.RequestDigest, config.EvidenceVersion)
		if err != nil {
			return lifecycle.RunResult{}, err
		}
		previous.Receipts = evidence.Receipts
	}
	receiptData, err := co.Canonical(previous)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	invocation := lifecycle.Invocation{Implementation: config.Implementation, Operation: operation, Variable: config.Variable, Canonical: execution.Block.Request, Placement: config.Placement, Materials: []lifecycle.MaterialFile{{Name: "kubeconfig", Part: secrets.ValuePart, Secret: "produced-kubeconfig", Variable: "kubeconfig"}}, Values: map[string]string{"oc": tool, "operation": execution.ApplyOperation, "block": execution.Block.ID, "registered": execution.ApplyRegistered, "evidence": string(receiptData)}, Deadline: time.Duration(config.BudgetSeconds)*time.Second + 10*time.Minute}
	if operation == "apply" {
		files, sources, err := placementMaterials(config.Placements, execution.Material)
		if err != nil {
			return lifecycle.RunResult{}, err
		}
		invocation.Materials = append(invocation.Materials, files...)
		invocation.Values["secretSources"] = string(sources)
	}
	return runner.Run(ctx, lifecycle.RunFor(execution, invocation))
}
func failure(reason, remedy string) error {
	return diagnostics.NewFailureWithRemediation("lifecycle.state", reason, "", remedy)
}
