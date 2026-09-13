package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func lifecycleInspectionCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "plan", short: "Preview the next complete lifecycle operation", flags: []flagSpec{stageFlag()}, long: "Preview the next legal full-context operation, or the exact continuation point of an incomplete one. It reads durable state and changes nothing. With --stage it also shows which blocks that selection would start."}),
		available(commandSpec{path: "status", short: "Show context readiness and lifecycle state", flags: []flagSpec{outputFlag(), boolFlag("watch", "Watch lifecycle state"), {name: "watch-interval", help: "Set the watch interval", kind: "string", defaultValue: "5s"}}, long: "Report context readiness, desired state and lifecycle progress from durable records. It performs no probe."}),
	}
}

func lifecycleMutationCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "apply", short: "Apply the complete selected lifecycle unit", flags: []flagSpec{authorizationFlag(), stageFlag(), confirmationFlag(), verboseFlag()}, long: "Realize the complete selected Environment, or continue the exact operation an interruption left behind. The plan is always complete; --stage only gates which blocks this invocation starts, and the operation pauses at that boundary. Review the plan before confirming; --yes skips ordinary confirmation."}),
		available(commandSpec{path: "destroy", short: "Destroy the complete selected lifecycle unit", flags: []flagSpec{authorizationFlag(), confirmationFlag(), verboseFlag()}, long: "Remove everything a completed apply recorded as owned, proving each removal. Review the plan before confirming; --yes skips ordinary confirmation."}),
	}
}

type LifecycleService interface {
	Plan(context.Context, lifecycle.PlanRequest) (*lifecycle.PlanResult, error)
	Status(context.Context, lifecycle.StatusRequest) (*lifecycle.StatusResult, error)
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
			ContextName:   values.text("context"),
			Watch:         values.boolean("watch"),
			WatchInterval: values.duration("watch-interval"),
		}, s.Lifecycle.Status)
	case "apply":
		result.lifecycleOperation, err = invokeResult(ctx, values, lifecycle.ApplyRequest{
			ContextName:      values.text("context"),
			Stages:           values.names("stage"),
			Authorizations:   values.authorizations(),
			SkipConfirmation: values.boolean("yes"),
			Verbose:          values.boolean("verbose"),
			SSH:              values.ssh(),
		}, s.Lifecycle.Apply)
	case "destroy":
		result.lifecycleOperation, err = invokeResult(ctx, values, lifecycle.DestroyRequest{
			ContextName:      values.text("context"),
			Authorizations:   values.authorizations(),
			SkipConfirmation: values.boolean("yes"),
			Verbose:          values.boolean("verbose"),
			SSH:              values.ssh(),
		}, s.Lifecycle.Destroy)
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
	return result, err
}
