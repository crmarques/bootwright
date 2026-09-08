package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func (s Services) invokeLifecycle(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.Lifecycle == nil {
		return errMissingService
	}
	switch path {
	case "plan":
		return invokeRequest(ctx, values, reconciliation.PlanRequest{
			ContextName: values.text("context"),
		}, s.Lifecycle.Plan)
	case "status":
		return invokeRequest(ctx, values, reconciliation.StatusRequest{
			ContextName:   values.text("context"),
			Watch:         values.boolean("watch"),
			WatchInterval: values.duration("watch-interval"),
		}, s.Lifecycle.Status)
	case "apply":
		return invokeRequest(ctx, values, reconciliation.ApplyRequest{
			ContextName:      values.text("context"),
			Authorizations:   values.authorizations(),
			SkipConfirmation: values.boolean("yes"),
			Verbose:          values.boolean("verbose"),
			SSH:              values.ssh(),
		}, s.Lifecycle.Apply)
	case "destroy":
		return invokeRequest(ctx, values, reconciliation.DestroyRequest{
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
