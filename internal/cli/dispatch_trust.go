package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/trust"
)

func (s Services) invokeMachineTrust(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.MachineTrust == nil {
		return errMissingService
	}
	switch path {
	case "machine trust":
		return invokeRequest(ctx, values, trust.EnrollRequest{
			ContextName:      values.text("context"),
			Machines:         values.names("machines"),
			Replace:          values.names("replace"),
			DryRun:           values.boolean("dry-run"),
			SkipConfirmation: values.boolean("yes"),
		}, s.MachineTrust.Enroll)
	default:
		return errors.New("command has no application dispatch")
	}
}
