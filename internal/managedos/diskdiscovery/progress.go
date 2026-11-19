package diskdiscovery

import (
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func discoveryGroups(name string, removing bool) []reconciliation.Group {
	steps := []struct{ id, description string }{
		{"inspect-target", "verify machine identity, power and discovery custody"},
		{"read-bmc-inventory", "read the drive inventory the BMC reports, when automatic inventory may use it"},
		{"prepare-ssh", "prepare operation-scoped SSH keys, host pinning and crypto policy"},
		{"build-media", "verify the source ISO and build installer-free private RAM inventory media"},
		{"publish-media", "publish the private discovery ISO"},
		{"verify-delivery", "verify HTTPS delivery of the discovery ISO"},
		{"insert-media", "configure declared media trust and attach the discovery ISO"},
		{"select-boot", "select the discovery ISO for the next boot"},
		{"power-on", "power on the machine and confirm its power state"},
		{"await-answer", "connect through pinned SSH to the RAM inventory; retry while its service starts"},
		{"receive-inventory", "read and verify the authenticated disk inventory"},
		{"power-off", "power off through the BMC and verify it"},
		{"eject-media", "eject the discovery ISO"},
		{"seal-cleanup", "record and seal discovery cleanup evidence"},
	}
	var groups []reconciliation.Group
	for _, step := range steps {
		groups = append(groups, reconciliation.Group{ID: step.id, Description: step.description, Machines: []string{name}})
	}
	if removing {
		return discoveryRemovalGroups(groups)
	}
	return groups
}

// Preserve frozen presentation.
func discoveryRemovalGroups(frozen []reconciliation.Group) []reconciliation.Group {
	var groups []reconciliation.Group
	for _, group := range frozen {
		if slices.Contains([]string{"inspect-target", "power-off", "eject-media", "seal-cleanup"}, group.ID) {
			group.Machines = slices.Clone(group.Machines)
			groups = append(groups, group)
		}
	}
	return groups
}
