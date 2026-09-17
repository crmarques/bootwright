package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/trust/enrollment"
)

func machineTrustCommand() commandSpec {
	spec := commandSpec{path: "machine trust", short: "Inspect and maintain exact host-key trust", flags: []flagSpec{stringFlag("machines", "Select comma-separated machine names (default: all)"), stringFlag("replace", "Select comma-separated replacements (default: none)"), dryRunFlag(), confirmationFlag(), outputFlag()}}
	spec.long = spec.short + ". It observes exactly the endpoints this context declares and records what" +
		" they present. A Machine that proves its host key another way is reported and left alone;" +
		" a changed key is superseded only by naming it in --replace."
	return available(spec)
}

type MachineTrustService interface {
	Enroll(context.Context, enrollment.EnrollRequest) (*enrollment.Report, error)
}

func (s Services) invokeMachineTrust(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.MachineTrust == nil {
		return commandResult{}, errMissingService
	}
	switch path {
	case "machine trust":
		result, err := invokeResult(ctx, values, enrollment.EnrollRequest{
			ContextName:      values.text("context"),
			Machines:         values.names("machines"),
			Replace:          values.names("replace"),
			DryRun:           values.boolean("dry-run"),
			SkipConfirmation: values.boolean("yes"),
		}, s.MachineTrust.Enroll)
		return commandResult{trust: result}, err
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
}
