package cli

import (
	"fmt"
	"strings"
)

func applyUnreadableOwnershipRefusal(ownershipDir string, skipped []error) error {
	details := make([]string, 0, len(skipped))
	for _, warning := range skipped {
		details = append(details, warning.Error())
	}
	return fmt.Errorf("%d ownership record(s) under %s could not be read, so this run cannot tell what this context already owns: %s; apply reads ownership records strictly, because a record it cannot read is a resource it would leave standing — a renamed cluster's predecessor keeps running on the same nodes while its replacement is bootstrapped over them. Repair or remove the reported record file(s) and re-run; no authorization skips this on apply, and --authorize %s covers the same records only for `bootwright destroy`",
		len(skipped), ownershipDir, strings.Join(details, "; "), authorizeUnreadableRecords)
}
