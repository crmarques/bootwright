package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func lifecycleInspectionCommands() []commandSpec {
	return []commandSpec{
		{path: "plan", short: "Preview the next complete lifecycle operation"},
		{path: "status", short: "Show context readiness and lifecycle state", flags: []flagSpec{outputFlag(), boolFlag("watch", "Watch lifecycle state"), {name: "watch-interval", help: "Set the watch interval", kind: "string", defaultValue: "5s"}}},
	}
}

func lifecycleMutationCommands() []commandSpec {
	return []commandSpec{
		{path: "apply", short: "Apply the complete selected lifecycle unit", flags: []flagSpec{authorizationFlag(), confirmationFlag(), verboseFlag()}},
		{path: "destroy", short: "Destroy the complete selected lifecycle unit", flags: []flagSpec{authorizationFlag(), confirmationFlag(), verboseFlag()}},
	}
}

type LifecycleService interface {
	Plan(context.Context, lifecycle.PlanRequest) error
	Status(context.Context, lifecycle.StatusRequest) error
	Apply(context.Context, lifecycle.ApplyRequest) error
	Destroy(context.Context, lifecycle.DestroyRequest) error
}

func (s Services) invokeLifecycle(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.Lifecycle == nil {
		return errMissingService
	}
	switch path {
	case "plan":
		return invokeRequest(ctx, values, lifecycle.PlanRequest{
			ContextName: values.text("context"),
		}, s.Lifecycle.Plan)
	case "status":
		return invokeRequest(ctx, values, lifecycle.StatusRequest{
			ContextName:   values.text("context"),
			Watch:         values.boolean("watch"),
			WatchInterval: values.duration("watch-interval"),
		}, s.Lifecycle.Status)
	case "apply":
		return invokeRequest(ctx, values, lifecycle.ApplyRequest{
			ContextName:      values.text("context"),
			Authorizations:   values.authorizations(),
			SkipConfirmation: values.boolean("yes"),
			Verbose:          values.boolean("verbose"),
			SSH:              values.ssh(),
		}, s.Lifecycle.Apply)
	case "destroy":
		return invokeRequest(ctx, values, lifecycle.DestroyRequest{
			ContextName:      values.text("context"),
			Authorizations:   values.authorizations(),
			SkipConfirmation: values.boolean("yes"),
			Verbose:          values.boolean("verbose"),
			SSH:              values.ssh(),
		}, s.Lifecycle.Destroy)
	default:
		return errors.New("command has no application dispatch")
	}
}
