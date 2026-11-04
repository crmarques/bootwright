package lifecycle

import (
	"cmp"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type holderPatch struct{ field, name string }

type split struct {
	kind         api.Kind
	origin, name string
	fragments    []fragment
	notes        []string
}

func (s split) identity() string { return string(s.kind) + "/" + s.name }

type holderGroup struct {
	members []int
	values  []api.Value
	holders []ProjectionHolder
}

func (r *diffRead) attribute(identity string, fragments []fragment, names claimedNames) ([]fragment, []split) {
	kindName, name, _ := strings.Cut(identity, "/")
	kind := api.Kind(kindName)
	shape := api.Schema(kind)
	var held []int
	for index, source := range fragments {
		if source.projection.Holder != nil {
			held = append(held, index)
		}
	}
	disputed := disputedPaths(fragments, held, shape)
	if len(disputed) == 0 {
		return fragments, nil
	}
	groups := groupHolders(fragments, held, disputed, shape)
	winner := r.keeper(kind, name, groups, disputed, shape)
	moved := map[int]bool{}
	var splits []split
	for index, group := range groups {
		if index == winner {
			continue
		}
		copied := r.copyName(kind, name, group.holders[0].Name, names)
		if copied == "" {
			continue
		}
		next := split{kind: kind, origin: name, name: copied, notes: splitNotes(kind, name, group, disputed)}
		for _, member := range group.members {
			moved[member] = true
			next.fragments = append(next.fragments, fragments[member])
		}
		for _, holder := range group.holders {
			key := string(holder.Kind) + "/" + holder.Name
			r.patches[key] = append(r.patches[key], holderPatch{field: holder.Field, name: copied})
		}
		splits = append(splits, next)
	}
	var kept []fragment
	for index, source := range fragments {
		if !moved[index] {
			kept = append(kept, source)
		}
	}
	return kept, splits
}

func disputedPaths(fragments []fragment, held []int, shape *api.Shape) []string {
	merged := api.MapValue()
	var disputed []string
	for _, index := range held {
		value, found := merge(merged, canonical(fragments[index].projection.Observed, shape), shape, "spec")
		merged = value
		for _, path := range found {
			if !slices.Contains(disputed, path) {
				disputed = append(disputed, path)
			}
		}
	}
	slices.Sort(disputed)
	return disputed
}

func groupHolders(fragments []fragment, held []int, disputed []string, shape *api.Shape) []holderGroup {
	var groups []holderGroup
	for _, index := range held {
		observed := canonical(fragments[index].projection.Observed, shape)
		values := make([]api.Value, 0, len(disputed))
		for _, path := range disputed {
			value, _ := valueAt(observed, shape, "spec", path)
			values = append(values, value)
		}
		if !slices.ContainsFunc(values, api.Value.Present) {
			continue
		}
		at := slices.IndexFunc(groups, func(group holderGroup) bool { return slices.EqualFunc(group.values, values, api.Value.Equal) })
		if at < 0 {
			groups, at = append(groups, holderGroup{values: values}), len(groups)
		}
		groups[at].members = append(groups[at].members, index)
		if holder := *fragments[index].projection.Holder; !slices.ContainsFunc(groups[at].holders, func(other ProjectionHolder) bool {
			return other.Kind == holder.Kind && other.Name == holder.Name
		}) {
			groups[at].holders = append(groups[at].holders, holder)
		}
	}
	for _, group := range groups {
		slices.SortFunc(group.holders, compareHolders)
	}
	slices.SortFunc(groups, func(first, second holderGroup) int { return compareHolders(first.holders[0], second.holders[0]) })
	return groups
}

func compareHolders(first, second ProjectionHolder) int {
	return cmp.Or(strings.Compare(first.Name, second.Name), strings.Compare(string(first.Kind), string(second.Kind)))
}

func (r diffRead) keeper(kind api.Kind, name string, groups []holderGroup, disputed []string, shape *api.Shape) int {
	effective, _ := r.applied.effective.Find(kind, name)
	spec := canonical(effective.Spec(), shape)
	for index, group := range groups {
		if !slices.ContainsFunc(disputed, func(path string) bool {
			value, _ := valueAt(spec, shape, "spec", path)
			return !value.Equal(group.values[slices.Index(disputed, path)])
		}) {
			return index
		}
	}
	largest := 0
	for index, group := range groups {
		if len(group.holders) > len(groups[largest].holders) {
			largest = index
		}
	}
	return largest
}

func (r diffRead) copyName(kind api.Kind, origin, holder string, names claimedNames) string {
	base := origin + "-" + holder
	for n := 1; ; n++ {
		candidate := base
		if n > 1 {
			candidate += "-" + strconv.Itoa(n)
		}
		if !api.ValidLexical("name", candidate) {
			return ""
		}
		identity := string(kind) + "/" + candidate
		if _, declared := r.applied.authored.Find(kind, candidate); declared || names.names[identity] {
			continue
		}
		names.names[identity] = true
		return candidate
	}
}

func splitNotes(kind api.Kind, origin string, group holderGroup, disputed []string) []string {
	holders := make([]string, 0, len(group.holders))
	for _, holder := range group.holders {
		holders = append(holders, string(holder.Kind)+"/"+holder.Name)
	}
	var notes []string
	for index, path := range disputed {
		if value := group.values[index]; value.Present() {
			notes = append(notes, "split from "+string(kind)+"/"+origin+": "+strings.Join(holders, ", ")+" observe "+path+" "+shortValue(value))
		}
	}
	return notes
}

func (r diffRead) splitSide(copied split, readings map[string]blockReading, names claimedNames) RealSide {
	ids := fragmentBlocks(copied.fragments)
	side := RealSide{Source: sideObserved, State: foldState(ids, readings)}
	for _, id := range ids {
		side.Notes = append(side.Notes, readings[id].notes...)
	}
	side = r.projectedSide(copied.kind, copied.origin, side, copied.fragments)
	renamed := api.NewObject(copied.kind, copied.name, side.Object.Labels(), side.Object.Spec())
	side.Object = &renamed
	side.Notes = append(side.Notes, copied.notes...)
	origin := string(copied.kind) + "/" + copied.origin
	side.Path = names.place(filepath.Dir(r.applied.paths[origin]), copied.name, copied.identity())
	return side
}

func patchHolder(object *api.Object, patches []holderPatch) *api.Object {
	if object == nil || len(patches) == 0 {
		return object
	}
	spec := object.Spec()
	for _, patch := range patches {
		spec = spec.WithPath(api.StringValue(patch.name), strings.Split(strings.TrimPrefix(patch.field, "spec."), ".")...)
	}
	patched := api.NewObject(object.Kind(), object.Name(), object.Labels(), canonical(spec, api.Schema(object.Kind())))
	return &patched
}

func referencesKind(holder api.Kind, field string, kind api.Kind) bool {
	rest, found := strings.CutPrefix(field, "spec.")
	if !found {
		return false
	}
	shape := api.Schema(holder)
	for _, name := range strings.Split(rest, ".") {
		if shape = childShape(shape, name); shape == nil {
			return false
		}
	}
	return slices.Contains(shape.Reference, kind)
}
