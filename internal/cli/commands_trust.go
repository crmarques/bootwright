package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/trust/enrollment"
)

func machineTrustCommand() commandSpec {
	return commandSpec{path: "machine trust", short: "Inspect and maintain exact host-key trust", flags: []flagSpec{stringFlag("machines", "Select comma-separated machine names (default: all)"), stringFlag("replace", "Select comma-separated replacements (default: none)"), dryRunFlag(), confirmationFlag(), outputFlag()}}
}

type MachineTrustService interface {
	Enroll(context.Context, enrollment.EnrollRequest) error
}

func (s Services) invokeMachineTrust(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.MachineTrust == nil {
		return errMissingService
	}
	switch path {
	case "machine trust":
		return invokeRequest(ctx, values, enrollment.EnrollRequest{
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
