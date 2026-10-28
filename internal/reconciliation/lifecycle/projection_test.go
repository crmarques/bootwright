package lifecycle

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func profile(name string, fields ...api.FieldValue) api.Value {
	return api.MapValue(append([]api.FieldValue{field("name", api.StringValue(name))}, fields...)...)
}

func libvirtSpec(profiles ...api.Value) api.Value {
	return api.MapValue(field("libvirt", api.MapValue(field("machineProfiles", api.ListValue(profiles...)))))
}

func TestMergeUnionsAndExcludesConflicts(t *testing.T) {
	shape := api.Schema(api.InfraProvider)
	first := libvirtSpec(profile("rhel", field("diskGiB", api.IntegerValue("100"))))
	second := libvirtSpec(profile("rhel", field("diskGiB", api.IntegerValue("80")), field("dataDisks", api.ListValue())), profile("sno", field("diskGiB", api.IntegerValue("120"))))
	third := libvirtSpec(profile("rhel", field("diskGiB", api.IntegerValue("90"))))
	merged, conflicts := merge(first, second, shape, "spec")
	if want := []string{"spec.libvirt.machineProfiles[rhel].diskGiB"}; !slices.Equal(conflicts, want) {
		t.Fatalf("conflicts = %v, want %v", conflicts, want)
	}
	want := libvirtSpec(profile("rhel", field("dataDisks", api.ListValue())), profile("sno", field("diskGiB", api.IntegerValue("120"))))
	if !merged.Equal(want) {
		t.Fatalf("merged = %+v", merged)
	}
	merged, more := merge(merged, third, shape, "spec")
	if len(more) != 0 || !without(merged, shape, "spec", conflicts).Equal(want) {
		t.Fatalf("a later fragment re-added an excluded path: %+v (%v)", merged, more)
	}
	if same, none := merge(first, first, shape, "spec"); len(none) != 0 || !same.Equal(first) {
		t.Fatalf("merging equal fragments = %+v (%v)", same, none)
	}
	notes := conflictNotes([]fragment{
		{block: "machine-rhel-01", projection: Projection{Observed: first}},
		{block: "machine-rhel-03", projection: Projection{Observed: second}},
	}, shape, conflicts)
	if want := []string{"spec.libvirt.machineProfiles[rhel].diskGiB: machine-rhel-01 observes 100, machine-rhel-03 observes 80"}; !slices.Equal(notes, want) {
		t.Fatalf("notes = %v", notes)
	}
}

func TestPruneKeepsOnlyObservedLeavesAndNamesTheRest(t *testing.T) {
	shape := api.Schema(api.InfraProvider)
	declared := api.MapValue(field("libvirt", api.MapValue(
		field("uri", api.StringValue("qemu:///system")),
		field("machineProfiles", api.ListValue(profile("rhel", field("cpu", api.IntegerValue("4")), field("diskGiB", api.IntegerValue("80"))))),
	)))
	observed := libvirtSpec(profile("rhel", field("diskGiB", api.IntegerValue("100"))))
	pruned, dropped := prune(declared, observed, shape, "spec", false)
	if want := libvirtSpec(profile("rhel", field("diskGiB", api.IntegerValue("80")))); !pruned.Equal(want) {
		t.Fatalf("pruned = %+v", pruned)
	}
	if want := []string{"spec.libvirt.uri", "spec.libvirt.machineProfiles[rhel].cpu"}; !slices.Equal(dropped, want) {
		t.Fatalf("not compared = %v", dropped)
	}
	paths := make([]string, 14)
	for index := range paths {
		paths[index] = string(rune('a' + index))
	}
	if limited := limitPaths(paths); len(limited) != 13 || limited[12] != "and 2 more" {
		t.Fatalf("limited = %v", limited)
	}
}

func TestPruneKeepsADeclaredItemWithoutAnObservedCounterpart(t *testing.T) {
	shape := api.Schema(api.InfraProvider)
	declared := libvirtSpec(
		profile("rhel", field("diskGiB", api.IntegerValue("80")), field("cpu", api.IntegerValue("4"))),
		profile("sno", field("diskGiB", api.IntegerValue("120"))),
		profile("win", field("cpu", api.IntegerValue("2"))),
	)
	observed := libvirtSpec(profile("sno", field("diskGiB", api.IntegerValue("120"))), profile("rhel"))
	pruned, dropped := prune(declared, observed, shape, "spec", false)
	want := libvirtSpec(profile("rhel", field("diskGiB", api.IntegerValue("80"))), profile("sno", field("diskGiB", api.IntegerValue("120"))), profile("win"))
	if !pruned.Equal(want) {
		t.Fatalf("pruned = %+v", pruned)
	}
	if want := []string{"spec.libvirt.machineProfiles[rhel].cpu", "spec.libvirt.machineProfiles[win].cpu"}; !slices.Equal(dropped, want) {
		t.Fatalf("not compared = %v", dropped)
	}
	pruned, dropped = prune(declared, observed, shape, "spec", true)
	if want := libvirtSpec(profile("rhel", field("diskGiB", api.IntegerValue("80"))), profile("sno", field("diskGiB", api.IntegerValue("120")))); !pruned.Equal(want) {
		t.Fatalf("pruned beside an unknown reading = %+v", pruned)
	}
	if want := []string{"spec.libvirt.machineProfiles[rhel].cpu", "spec.libvirt.machineProfiles[win]"}; !slices.Equal(dropped, want) {
		t.Fatalf("not compared beside an unknown reading = %v", dropped)
	}
}

func TestClaimedPathsGeneralizeNamedItemsThatValueAtAddressesByKey(t *testing.T) {
	shape := api.Schema(api.InfraProvider)
	value := libvirtSpec(profile("rhel", field("diskGiB", api.IntegerValue("80")), field("dataDisks", api.ListValue(profile("data1", field("sizeGiB", api.IntegerValue("50")))))), profile("sno"))
	if got, want := claimedPaths(value, shape, "spec"), []string{
		"spec.libvirt.machineProfiles[].name", "spec.libvirt.machineProfiles[].diskGiB", "spec.libvirt.machineProfiles[].dataDisks[].name",
		"spec.libvirt.machineProfiles[].dataDisks[].sizeGiB", "spec.libvirt.machineProfiles[].name",
	}; !slices.Equal(got, want) {
		t.Fatalf("claimed paths = %v", got)
	}
	if got, want := claimedPaths(libvirtSpec(), shape, "spec"), []string{"spec.libvirt.machineProfiles[].name"}; !slices.Equal(got, want) {
		t.Fatalf("an empty named list claims %v", got)
	}
	if got := claimedPaths(api.MapValue(field("libvirt", api.MapValue(field("uri", api.StringValue("qemu:///system"))))), shape, "spec"); !slices.Equal(got, []string{"spec.libvirt.uri"}) {
		t.Fatalf("a scalar claims %v", got)
	}
	if found, ok := valueAt(value, shape, "spec", "spec.libvirt.machineProfiles[rhel].dataDisks[data1].sizeGiB"); !ok || found.Text() != "50" {
		t.Fatalf("value at a named path = %+v (%t)", found, ok)
	}
	if found, ok := valueAt(value, shape, "spec", "spec.libvirt.machineProfiles[win].diskGiB"); ok {
		t.Fatalf("value at an absent item = %+v", found)
	}
}

// A free map such as a node's labels is one field: a projection claims it
// whole, and a declared key the observation lacks stays declared, so a label
// removed after it was applied shows as a difference rather than as a field
// left uncompared.
func TestAFreeMapIsClaimedAndComparedWhole(t *testing.T) {
	shape := api.Schema(api.OpenShiftCluster)
	labels := func(fields ...api.FieldValue) api.Value {
		return api.MapValue(field("nodes", api.ListValue(api.MapValue(field("name", api.StringValue("worker-0")), field("labels", api.MapValue(fields...))))))
	}
	declared := labels(field("example.test/rack", api.StringValue("a")), field("example.test/row", api.StringValue("1")))
	observed := labels(field("example.test/row", api.StringValue("1")))
	if paths := claimedPaths(observed, shape, "spec"); !slices.Equal(paths, []string{"spec.nodes[].name", "spec.nodes[].labels"}) {
		t.Fatalf("claimed %v", paths)
	}
	pruned, dropped := prune(declared, observed, shape, "spec", false)
	if len(dropped) != 0 || !pruned.Equal(declared) {
		t.Fatalf("pruned %+v, dropping %v", pruned, dropped)
	}
	if _, conflicts := merge(observed, labels(field("example.test/row", api.StringValue("2"))), shape, "spec"); !slices.Equal(conflicts, []string{"spec.nodes[worker-0].labels"}) {
		t.Fatalf("conflicts = %v", conflicts)
	}
}
