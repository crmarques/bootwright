package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func lifecycleInspectionCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "plan", short: "Preview the next complete lifecycle operation", flags: []flagSpec{stageFlag()}, long: "Preview the next legal full-context operation, or the exact continuation point of an incomplete one. It reads durable state and changes nothing. With --stage it also shows which blocks that selection would start."}),
		available(commandSpec{path: "status", short: "Show context readiness and lifecycle state", long: "Report context readiness, desired state and lifecycle progress from durable records. It performs no probe."}),
		available(commandSpec{path: "diff", short: "Compare desired state with what this context realized", flags: []flagSpec{boolFlag("observe", "Read each realized block's target through its read-only observation")}, long: "Compare the context's desired state with what its apply realized, object by object. Without --observe it reads durable records only and contacts no host; with --observe it reads each realized block's target through that block's read-only observation. It changes nothing."}),
	}
}

func lifecycleMutationCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "apply", short: "Apply the complete selected lifecycle unit", flags: []flagSpec{authorizationFlag(), stageFlag(), confirmationFlag()}, long: "Realize the complete selected Environment, or continue the exact operation an interruption left behind. The plan is always complete; --stage only gates which blocks this invocation starts, and the operation pauses at that boundary. Review the plan before confirming; --yes skips ordinary confirmation."}),
		available(commandSpec{path: "destroy", short: "Destroy the complete selected lifecycle unit", flags: []flagSpec{authorizationFlag(), confirmationFlag()}, long: "Remove what this context's apply owns, whether that apply completed or not, proving each removal before it registers; a failed destroy is replaced by a fresh removal. Follow the plan's Stop first and Stop after online cleanup lists (bootwright machine stop): online cleanup, such as storage cleanup or a subscription unregistration, needs its Machines running, and destroy pauses for their stop after it. A plan that deletes disks needs --authorize data-loss. Review the plan before confirming; --yes skips ordinary confirmation."}),
	}
}

type LifecycleService interface {
	Plan(context.Context, lifecycle.PlanRequest) (*lifecycle.PlanResult, error)
	Status(context.Context, lifecycle.StatusRequest) (*lifecycle.StatusResult, error)
	Diff(context.Context, lifecycle.DiffRequest) (*lifecycle.DiffResult, error)
	Apply(context.Context, lifecycle.ApplyRequest) (*lifecycle.OperationResult, error)
	Destroy(context.Context, lifecycle.DestroyRequest) (*lifecycle.OperationResult, error)
}

func (s Services) invokeLifecycle(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.Lifecycle == nil {
		return commandResult{}, errMissingService
	}
	var result commandResult
	var err error
	switch path {
	case "plan":
		result.lifecyclePlan, err = invokeResult(ctx, values, lifecycle.PlanRequest{
			ContextName: values.text("context"),
			Stages:      values.names("stage"),
		}, s.Lifecycle.Plan)
	case "status":
		result.lifecycleStatus, err = invokeResult(ctx, values, lifecycle.StatusRequest{
			ContextName: values.text("context"),
		}, s.Lifecycle.Status)
	case "diff":
		result.lifecycleDiff, err = invokeResult(ctx, values, lifecycle.DiffRequest{
			ContextName: values.text("context"),
			Observe:     values.boolean("observe"),
		}, s.Lifecycle.Diff)
	case "apply":
		result.lifecycleOperation, err = invokeResult(ctx, values, lifecycle.ApplyRequest{
			ContextName:      values.text("context"),
			Stages:           values.names("stage"),
			Authorizations:   values.authorizations(),
			SkipConfirmation: values.boolean("yes"),
			SSH:              values.ssh(),
		}, s.Lifecycle.Apply)
	case "destroy":
		result.lifecycleOperation, err = invokeResult(ctx, values, lifecycle.DestroyRequest{
			ContextName:      values.text("context"),
			Authorizations:   values.authorizations(),
			SkipConfirmation: values.boolean("yes"),
			SSH:              values.ssh(),
		}, s.Lifecycle.Destroy)
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
	return result, err
}
