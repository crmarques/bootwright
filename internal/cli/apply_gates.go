package cli

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/workspace"
)

func applyOwnershipRecords(ctx workspace.Context, dryRun bool, invocation *resolvedInvocation) ([]ownership.ResourceRecord, []error, error) {
	records, skipped, err := converge.LoadContextOwnershipRecordsWithWarnings(ctx.OwnershipDir, ctx.Name)
	if err != nil {
		return nil, nil, err
	}
	if len(skipped) > 0 && !dryRun {
		return nil, nil, applyUnreadableOwnershipRefusal(ctx.OwnershipDir, skipped, invocation)
	}
	return records, skipped, nil
}

func applyUnreadableOwnershipRefusal(ownershipDir string, skipped []error, invocation *resolvedInvocation) error {
	details := make([]string, 0, len(skipped))
	for _, warning := range skipped {
		details = append(details, warning.Error())
	}
	retry := "Repair or remove the reported record file(s), then repeat the same invocation"
	if invocation != nil {
		command, err := invocation.retry(retryIntent{})
		if err != nil {
			return err
		}
		retry = fmt.Sprintf("Repair or remove the reported record file(s), then re-run `%s`", command.String())
	}
	return fmt.Errorf("%d ownership record(s) under %s could not be read, so this run cannot tell what this context already owns: %s; apply reads ownership records strictly, because a record it cannot read is a resource it would leave standing — a renamed cluster's predecessor keeps running on the same nodes while its replacement is bootstrapped over them. %s; no authorization skips this on apply, and --authorize %s covers the same records only for the destroy command",
		len(skipped), ownershipDir, strings.Join(details, "; "), retry, authorizeUnreadableRecords)
}
