package lifecycle

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const maxChangedPaths = 32

type objectChange struct {
	identity string
	change   reconciliation.Change
	fields   []string
}

func objectChanges(before, after api.Catalog) []objectChange {
	var changes []objectChange
	for _, identity := range catalogIdentities(before, after) {
		previous, had := before.Find(identity.kind, identity.name)
		next, has := after.Find(identity.kind, identity.name)
		change := objectChange{identity: string(identity.kind) + "/" + identity.name}
		switch {
		case !had:
			change.change = reconciliation.Added
		case !has:
			change.change = reconciliation.Removed
		default:
			change.fields = objectFields(previous, next)
			if len(change.fields) == 0 {
				continue
			}
			change.change = reconciliation.Changed
		}
		changes = append(changes, change)
	}
	return changes
}

func objectFields(before, after api.Object) []string {
	shape := api.Schema(before.Kind())
	fields := changedPaths(canonical(before.Spec(), shape), canonical(after.Spec(), shape), shape, "spec")
	fields = append(fields, changedPaths(canonical(before.Labels(), nil), canonical(after.Labels(), nil), nil, "metadata.labels")...)
	slices.Sort(fields)
	return fields[:min(len(fields), maxChangedPaths)]
}

type objectIdentity struct {
	kind api.Kind
	name string
}

func catalogIdentities(catalogs ...api.Catalog) []objectIdentity {
	var identities []objectIdentity
	for _, catalog := range catalogs {
		for _, object := range catalog.Objects() {
			identity := objectIdentity{kind: object.Kind(), name: object.Name()}
			if !slices.Contains(identities, identity) {
				identities = append(identities, identity)
			}
		}
	}
	slices.SortFunc(identities, compareIdentities)
	return identities
}

func compareIdentities(first, second objectIdentity) int {
	return cmp.Or(cmp.Compare(api.KindIndex(first.kind), api.KindIndex(second.kind)),
		strings.Compare(string(first.kind), string(second.kind)), strings.Compare(first.name, second.name))
}

func changedPaths(before, after api.Value, shape *api.Shape, prefix string) []string {
	paths := differingPaths(before, after, shape, prefix)
	slices.Sort(paths)
	return paths[:min(len(paths), maxChangedPaths)]
}

func differingPaths(before, after api.Value, shape *api.Shape, prefix string) []string {
	switch {
	case before.Equal(after):
		return nil
	case !before.Present() || !after.Present() || shape == nil || before.Type() != after.Type():
		return []string{prefix}
	case before.Type() == api.Mapping:
		var paths []string
		for _, name := range fieldNames(before, after) {
			paths = append(paths, differingPaths(before.Get(name), after.Get(name), childShape(shape, name), joinPath(prefix, name))...)
		}
		return paths
	case before.Type() == api.Sequence && nameKey(shape) != "":
		return differingItems(before, after, shape, prefix)
	}
	return []string{prefix}
}

func fieldNames(values ...api.Value) []string {
	var names []string
	for _, value := range values {
		for _, field := range value.Fields() {
			if !slices.Contains(names, field.Name) {
				names = append(names, field.Name)
			}
		}
	}
	return names
}

func differingItems(before, after api.Value, shape *api.Shape, prefix string) []string {
	key := nameKey(shape)
	previous, next := itemsByKey(before, key), itemsByKey(after, key)
	var keys []string
	for _, item := range append(before.Items(), after.Items()...) {
		if name := item.Get(key).Text(); !slices.Contains(keys, name) {
			keys = append(keys, name)
		}
	}
	var paths []string
	for _, name := range keys {
		paths = append(paths, differingPaths(previous[name], next[name], shape.Element, itemPath(prefix, name))...)
	}
	return paths
}

func itemsByKey(value api.Value, key string) map[string]api.Value {
	items := map[string]api.Value{}
	for _, item := range value.Items() {
		items[item.Get(key).Text()] = item
	}
	return items
}

const (
	maxNamedObjects = 5
	maxNamedFields  = 3
)

func describeChanges(changes []objectChange) string {
	var ordered []objectChange
	for _, kind := range []reconciliation.Change{reconciliation.Added, reconciliation.Changed, reconciliation.Removed} {
		for _, change := range changes {
			if change.change == kind {
				ordered = append(ordered, change)
			}
		}
	}
	named := ordered[:min(len(ordered), maxNamedObjects)]
	var groups []string
	for _, verb := range []struct {
		change reconciliation.Change
		word   string
	}{{reconciliation.Added, "adds"}, {reconciliation.Changed, "changes"}, {reconciliation.Removed, "removes"}} {
		var objects []string
		for _, change := range named {
			if change.change == verb.change {
				objects = append(objects, namedObject(change))
			}
		}
		if len(objects) != 0 {
			groups = append(groups, verb.word+" "+strings.Join(objects, ", "))
		}
	}
	described := "it " + joinLast(groups)
	if rest := len(ordered) - len(named); rest != 0 {
		described += ", and " + moreOf(rest, "more object", "more objects")
	}
	return described
}

func namedObject(change objectChange) string {
	if change.change != reconciliation.Changed || len(change.fields) == 0 {
		return change.identity
	}
	fields := change.fields
	if len(fields) > maxNamedFields {
		fields = append(slices.Clone(fields[:maxNamedFields-1]), moreOf(len(change.fields)-maxNamedFields+1, "more field", "more fields"))
	}
	return change.identity + " (" + joinLast(fields) + ")"
}

func joinLast(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

func moreOf(count int, one, many string) string {
	if count == 1 {
		return "1 " + one
	}
	return strconv.Itoa(count) + " " + many
}
