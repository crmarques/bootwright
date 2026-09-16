package inventory

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
)

// The state a Machine is reported in. Ownership is what an operation proved,
// so a Machine no frozen plan names is unmanaged rather than absent.
const (
	StateUnmanaged = "unmanaged"
	StateOwned     = "owned"
	StatePending   = "pending"
	StateReleased  = "released"
	StateFailed    = "failed"
	StateUnknown   = "unknown"
)

// Rows derives one row per selected Machine in canonical name order. A cluster
// selection filters presentation alone: it never changes the graph, the
// evidence, or what a Machine's state means.
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
		Name: object.Name(), OS: "provided", State: state(evidence),
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

// state translates what the frozen verb and its blocks proved into what an
// operator owns now. A removal that completed released the Machine; one that
// has not completed still owns the effects it has not removed.
func state(evidence machine.OwnershipState) string {
	switch evidence.State {
	case "":
		return StateUnmanaged
	case machine.BlockUnknown, machine.BlockRunning:
		return StateUnknown
	case machine.BlockFailed:
		return StateFailed
	case machine.BlockDone:
		if evidence.Verb == machine.VerbDestroy {
			return StateReleased
		}
		return StateOwned
	}
	// A removal that has not completed still owns what it has not removed.
	if evidence.Verb == machine.VerbDestroy {
		return StateOwned
	}
	return StatePending
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
