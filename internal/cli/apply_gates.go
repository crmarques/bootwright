package cli

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/workspace"
)

func applyOwnershipRecords(ctx workspace.Context, dryRun bool, invocation *resolvedInvocation) ([]ownership.ResourceRecord, []error, error) {
	allRecords, skipped, err := ownership.LoadResourcesWithWarnings(ctx.OwnershipDir)
	if err != nil {
		skipped = append(skipped, fmt.Errorf("scan ownership storage %s: %w", ctx.OwnershipDir, err))
		if dryRun {
			return nil, skipped, nil
		}
		return nil, nil, applyUnreadableOwnershipRefusal(ctx.OwnershipDir, skipped, invocation)
	}
	trustedRecords := make([]ownership.ResourceRecord, 0, len(allRecords))
	for _, record := range allRecords {
		var conflicts []string
		if record.APIVersion != "bootwright.io/ownership/v1alpha1" {
			conflicts = append(conflicts, fmt.Sprintf("apiVersion=%q", record.APIVersion))
		}
		if record.Owner != ownership.Owner {
			conflicts = append(conflicts, fmt.Sprintf("owner=%q", record.Owner))
		}
		if record.Context != ctx.Name {
			conflicts = append(conflicts, fmt.Sprintf("context=%q", record.Context))
		}
		if len(conflicts) > 0 {
			skipped = append(skipped, fmt.Errorf("ownership resource %s/%s has noncanonical identity inside context %q ownership storage: %s", record.Kind, record.Name, ctx.Name, strings.Join(conflicts, ", ")))
			continue
		}
		trustedRecords = append(trustedRecords, record)
	}
	records := ownership.FilterByContext(trustedRecords, ctx.Name)
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
	retry := "Repair the reported evidence, or remove it only after proving it stale, then repeat the same invocation"
	if invocation != nil {
		command, err := invocation.retry(retryIntent{})
		if err != nil {
			return err
		}
		retry = fmt.Sprintf("Repair the reported evidence, or remove it only after proving it stale, then re-run `%s`", command.String())
	}
	return fmt.Errorf("%d ownership evidence file(s) under %s could not be read or do not belong to this context, so this run cannot tell what this context already owns: %s; apply reads ownership evidence strictly, because evidence it cannot trust may name a resource it would leave standing or overwrite — a renamed cluster's predecessor can keep running while its replacement is bootstrapped over it. %s; no authorization skips this on apply, and --authorize %s covers unreadable records only for the destroy command",
		len(skipped), ownershipDir, strings.Join(details, "; "), retry, authorizeUnreadableRecords)
}
