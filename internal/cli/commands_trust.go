package cli

import (
	"context"
	"strings"

	"github.com/crmarques/bootwright/internal/trust/enrollment"
)

func machineTrustCommand() commandSpec {
	spec := commandSpec{path: "machine trust", results: []jsonResult{trustResult{}}, render: renderMachineTrust(), validate: validateMachineTrustCommand, example: "--context lab --dry-run", invoke: Services.invokeMachineTrust, short: "Inspect and maintain exact host-key trust", flags: []flagSpec{listFlag("name", "Select comma-separated machine names (default: all)"), listFlag("replace", "Select comma-separated replacements, each optionally <machine>=SHA256:<fingerprint> (default: none)"), stringFlag("expect", "Name comma-separated verified fingerprints as <machine>=SHA256:<fingerprint>"), dryRunFlag(), confirmationFlag()}}
	spec.long = spec.short + ". It observes exactly the endpoints this context declares and records what" +
		" they present. A Machine that proves its host key another way is reported and left alone;" +
		" a changed key is superseded only by naming it in --replace. A key whose fingerprint --expect or --replace names" +
		" must match it, and --yes records only keys whose fingerprints are named, after you verify them out of band."
	return available(spec)
}

func trustExpectations(replaced, expected []string) ([]string, map[string]string) {
	var replace []string
	var expect map[string]string
	name := func(member string) string {
		machine, fingerprint, named := strings.Cut(member, "=")
		if named {
			if expect == nil {
				expect = map[string]string{}
			}
			expect[machine] = fingerprint
		}
		return machine
	}
	if replaced != nil {
		replace = make([]string, 0, len(replaced))
	}
	for _, member := range replaced {
		replace = append(replace, name(member))
	}
	for _, member := range expected {
		name(member)
	}
	return replace, expect
}

type MachineTrustService interface {
	Enroll(context.Context, enrollment.EnrollRequest) (*enrollment.Report, error)
}

func (s Services) invokeMachineTrust(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.MachineTrust == nil {
		return commandResult{}, errMissingService
	}
	replace, expect := trustExpectations(values.names("replace"), values.names("expect"))
	result, err := invokeResult(ctx, values, enrollment.EnrollRequest{
		ContextName:      values.text("context"),
		Machines:         values.names("name"),
		Replace:          replace,
		Expect:           expect,
		DryRun:           values.boolean("dry-run"),
		SkipConfirmation: values.boolean("yes"),
	}, s.MachineTrust.Enroll)
	return commandResult{trust: result}, err
}
