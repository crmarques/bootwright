package lifecycle

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// A named list differs by the items whose content changed, added or removed,
// each at its own name, and reordering the same items is no change.
func TestChangedPathsCompareNamedListItemsByNameNotPosition(t *testing.T) {
	text := &api.Shape{Type: api.String}
	shape := &api.Shape{Type: api.Sequence, NameKey: "name", Element: &api.Shape{Type: api.Mapping, Fields: []api.Field{{Name: "name", Shape: text}, {Name: "size", Shape: text}}}}
	item := func(name, size string) api.Value {
		return api.MapValue().With("name", api.StringValue(name)).With("size", api.StringValue(size))
	}
	before := api.ListValue(item("a", "1"), item("b", "2"), item("gone", "5"))
	after := api.ListValue(item("b", "3"), item("a", "1"), item("added", "9"))
	if got, want := changedPaths(before, after, shape, "disks"), []string{"disks[added]", "disks[b].size", "disks[gone]"}; !slices.Equal(got, want) {
		t.Fatalf("changed paths = %v, want %v", got, want)
	}
	if got := changedPaths(before, api.ListValue(item("b", "2"), item("gone", "5"), item("a", "1")), shape, "disks"); len(got) != 0 {
		t.Fatalf("a reordered list changed %v", got)
	}
}

// An object carried by several hosts realizes no better than its worst host, an
// object with its own realization keeps it, and a carrier no host names adds
// nothing.
func TestACarriedObjectRealizesAsItsWorstHost(t *testing.T) {
	realized := map[string]RealizationStatus{
		"Host/done": RealizationDone, "Host/pending": RealizationPending, "Host/failed": RealizationFailed, "Host/unknown": RealizationUnknown, "Own/x": RealizationDone,
	}
	carriers := map[string][]string{
		"Obj/pending": {"Host/done", "Host/pending"},
		"Obj/failed":  {"Host/pending", "Host/failed", "Host/done"},
		"Obj/unknown": {"Host/done", "Host/unknown", "Host/failed"},
		"Obj/absent":  {"Host/not-named"},
		"Own/x":       {"Host/failed"},
	}
	got := carriedRealizations(realized, carriers)
	want := map[string]RealizationStatus{"Obj/pending": RealizationPending, "Obj/failed": RealizationFailed, "Obj/unknown": RealizationUnknown, "Own/x": RealizationDone}
	for identity, status := range want {
		if got[identity] != status {
			t.Errorf("%s realized as %q, want %q", identity, got[identity], status)
		}
	}
	if _, named := got["Obj/absent"]; named {
		t.Error("an object carried only by an unnamed host was given a realization")
	}
}
