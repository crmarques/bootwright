package workflow

import (
	"fmt"

	"github.com/crmarques/bootwright/internal/ownership"
)

func storageDestroyOwnerContractProblems(record ownership.ResourceRecord, contextName, expectedSeedHost string) []string {
	var problems []string
	if record.Owner != ownership.Owner {
		problems = append(problems, fmt.Sprintf("owner is %q, want %q", record.Owner, ownership.Owner))
	}
	if record.EffectiveRole() != ownership.RoleOwner {
		problems = append(problems, fmt.Sprintf("role is %q, want %q", record.EffectiveRole(), ownership.RoleOwner))
	}
	if record.APIVersion != "bootwright.io/ownership/v1alpha1" {
		problems = append(problems, fmt.Sprintf("apiVersion is %q, want %q", record.APIVersion, "bootwright.io/ownership/v1alpha1"))
	}
	if record.Context != contextName {
		problems = append(problems, fmt.Sprintf("context is %q, want %q", record.Context, contextName))
	}
	if record.Cluster != record.Name {
		problems = append(problems, fmt.Sprintf("cluster is %q, want record name %q", record.Cluster, record.Name))
	}
	if expectedSeedHost == "" {
		problems = append(problems, "selected desired state has no expected seed host")
	} else {
		if record.Host != expectedSeedHost {
			problems = append(problems, fmt.Sprintf("host is %q, want selected seed %q", record.Host, expectedSeedHost))
		}
		if record.Attributes["seedHost"] != expectedSeedHost {
			problems = append(problems, fmt.Sprintf("attributes.seedHost is %q, want selected seed %q", record.Attributes["seedHost"], expectedSeedHost))
		}
	}
	if !storageDestroyFSIDPattern.MatchString(record.Attributes["fsid"]) {
		problems = append(problems, fmt.Sprintf("attributes.fsid %q is not a canonical UUID", record.Attributes["fsid"]))
	}
	return problems
}
