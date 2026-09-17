package inventory

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
)

// How far through this context's lifecycle a Machine has been carried. Each
// value names the verb that last acted on it and whether that verb completed,
// because an operator asking about a Machine is asking exactly that: has
// anything been applied, is something running, and did a removal finish. It is
// not the Machine's power, which only its management controller answers.
const (
	LifecycleNotApplied = "not-applied"
	LifecycleApplying   = "applying"
	LifecycleApplied    = "applied"
	LifecycleDestroying = "destroying"
	LifecycleDestroyed  = "destroyed"
	LifecycleFailed     = "failed"
	LifecycleUnknown    = "unknown"
)

// Rows derives one row per selected Machine in canonical name order. A cluster
// selection filters presentation alone: it never changes the graph, the
// evidence, or what a Machine's lifecycle position means.
func Rows(catalog api.Catalog, clusters []string, owned map[string]machine.OwnershipState) ([]MachineRow, error) {
	selection, filtered := normalize(clusters)
	memberships := machine.Memberships(catalog)
	rows := []MachineRow{}
	for _, object := range catalog.OfKind(api.Machine) {
		members := memberships[object.Name()]
		if filtered && !slices.ContainsFunc(members, func(name string) bool { return slices.Contains(selection, name) }) {
			continue
		}
		rows = append(rows, row(object, members, owned[object.Identity()]))
	}
	slices.SortFunc(rows, func(x, y MachineRow) int { return strings.Compare(x.Name, y.Name) })
	return rows, nil
}

// Names is the sorted machine-name projection a silent invocation prints.
func Names(rows []MachineRow) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return names
}

func row(object api.Object, members []string, evidence machine.OwnershipState) MachineRow {
	current := MachineRow{
		Name: object.Name(), OS: "provided", Lifecycle: lifecycle(evidence),
		IPs:      machine.IPAddresses(object),
		Provider: object.Spec().Get("substrate", "providerRef").Text(),
		Clusters: slices.Clone(members),
	}
	if machine.Installed(object) {
		current.OS = "installed"
	}
	if target, ok := machine.SSH(object); ok {
		current.Address = target.Address
	}
	if current.Clusters == nil {
		current.Clusters = []string{}
	}
	return current
}

// lifecycle translates the frozen verb and the least settled state its blocks
// reached into the position an operator reads. A verb that has not completed
// names itself in progress, because a Machine halfway through a removal still
// owns what is not yet removed and must not read as one nothing has touched.
func lifecycle(evidence machine.OwnershipState) string {
	destroying := evidence.Verb == machine.VerbDestroy
	switch evidence.State {
	case "":
		return LifecycleNotApplied
	case machine.BlockUnknown:
		return LifecycleUnknown
	case machine.BlockFailed:
		return LifecycleFailed
	case machine.BlockDone:
		if destroying {
			return LifecycleDestroyed
		}
		return LifecycleApplied
	}
	if destroying {
		return LifecycleDestroying
	}
	return LifecycleApplying
}

// normalize applies the list rules a cluster selector follows: an omitted
// value selects every Machine, and a value that resolves to no member selects
// none rather than silently selecting all.
func normalize(clusters []string) ([]string, bool) {
	if clusters == nil {
		return nil, false
	}
	selection := []string{}
	for _, name := range clusters {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" && !slices.Contains(selection, trimmed) {
			selection = append(selection, trimmed)
		}
	}
	return selection, true
}
