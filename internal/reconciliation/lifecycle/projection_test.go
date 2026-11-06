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

func TestOverlayReplacesOnlyWhatDiffersFromTheEffectiveValue(t *testing.T) {
	shape := api.Schema(api.InfraProvider)
	authored := api.MapValue(field("libvirt", api.MapValue(
		field("uri", api.StringValue("qemu:///system")),
		field("machineProfiles", api.ListValue(profile("rhel", field("cpu", api.IntegerValue("4")), field("diskGiB", api.IntegerValue("80"))))),
	)))
	observed := libvirtSpec(profile("rhel", field("diskGiB", api.IntegerValue("100"))))
	want := api.MapValue(field("libvirt", api.MapValue(
		field("uri", api.StringValue("qemu:///system")),
		field("machineProfiles", api.ListValue(profile("rhel", field("cpu", api.IntegerValue("4")), field("diskGiB", api.IntegerValue("100"))))),
	)))
	if got := canonical(overlay(authored, authored, observed, shape, false, false), shape); !got.Equal(want) {
		t.Fatalf("overlay = %+v", got)
	}
	if same := overlay(authored, authored, libvirtSpec(profile("rhel", field("diskGiB", api.IntegerValue("80")))), shape, false, false); !same.Equal(authored) {
		t.Fatalf("an observation equal to the applied value changed it: %+v", same)
	}
	if paths := uncompared(authored, observed, shape, "spec", "", false, false); !slices.Equal(paths, []string{"spec.libvirt.uri", "spec.libvirt.machineProfiles[rhel].cpu"}) {
		t.Fatalf("not compared = %v", paths)
	}
	paths := make([]string, 14)
	for index := range paths {
		paths[index] = string(rune('a' + index))
	}
	if limited := limitPaths(paths); len(limited) != 13 || limited[12] != "and 2 more" {
		t.Fatalf("limited = %v", limited)
	}
}

func TestOverlayKeepsADefaultedFieldAbsentUnlessItsObservedValueDiffers(t *testing.T) {
	shape := api.Schema(api.InfraProvider)
	authored := libvirtSpec(profile("rhel"))
	effective := libvirtSpec(profile("rhel", field("diskGiB", api.IntegerValue("120"))))
	if got := overlay(authored, effective, effective, shape, false, false); !got.Equal(authored) {
		t.Fatalf("a default observed as applied appeared: %+v", got)
	}
	resized := libvirtSpec(profile("rhel", field("diskGiB", api.IntegerValue("200"))))
	if got := canonical(overlay(authored, effective, resized, shape, false, false), shape); !got.Equal(resized) {
		t.Fatalf("a default observed otherwise = %+v", got)
	}
}

func TestOverlayKeepsUnobservedOuterItemsAndHoldsObservedInnerLists(t *testing.T) {
	shape := api.Schema(api.InfraProvider)
	disk := func(name, size string) api.Value {
		return api.MapValue(field("name", api.StringValue(name)), field("sizeGiB", api.IntegerValue(size)))
	}
	authored := libvirtSpec(
		profile("rhel", field("diskGiB", api.IntegerValue("80")), field("cpu", api.IntegerValue("4")), field("dataDisks", api.ListValue(disk("data1", "50"), disk("data2", "60")))),
		profile("sno", field("diskGiB", api.IntegerValue("120"))),
	)
	observed := libvirtSpec(profile("rhel", field("dataDisks", api.ListValue(disk("data1", "50")))), profile("win", field("diskGiB", api.IntegerValue("50"))))
	want := libvirtSpec(
		profile("rhel", field("diskGiB", api.IntegerValue("80")), field("cpu", api.IntegerValue("4")), field("dataDisks", api.ListValue(disk("data1", "50")))),
		profile("sno", field("diskGiB", api.IntegerValue("120"))), profile("win", field("diskGiB", api.IntegerValue("50"))),
	)
	if got := canonical(overlay(authored, authored, observed, shape, false, false), shape); !got.Equal(want) {
		t.Fatalf("overlay = %+v", got)
	}
	if paths := uncompared(authored, observed, shape, "spec", "", false, false); !slices.Equal(paths, []string{
		"spec.libvirt.machineProfiles[rhel].diskGiB", "spec.libvirt.machineProfiles[rhel].cpu", "spec.libvirt.machineProfiles[sno].diskGiB",
	}) {
		t.Fatalf("not compared = %v", paths)
	}
	kept := libvirtSpec(
		profile("rhel", field("diskGiB", api.IntegerValue("80")), field("cpu", api.IntegerValue("4")), field("dataDisks", api.ListValue(disk("data1", "50"), disk("data2", "60")))),
		profile("sno", field("diskGiB", api.IntegerValue("120"))), profile("win", field("diskGiB", api.IntegerValue("50"))),
	)
	if got := canonical(overlay(authored, authored, observed, shape, true, false), shape); !got.Equal(kept) {
		t.Fatalf("overlay beside an unknown reading = %+v", got)
	}
	if paths := uncompared(authored, observed, shape, "spec", "", true, false); !slices.Contains(paths, "spec.libvirt.machineProfiles[rhel].dataDisks[data2].sizeGiB") {
		t.Fatalf("not compared beside an unknown reading = %v", paths)
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
// whole and the observed map replaces the applied one, so a label removed
// after it was applied shows as a difference rather than as a field left
// uncompared.
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
	if real := overlay(declared, declared, observed, shape, false, false); !real.Equal(observed) {
		t.Fatalf("overlay %+v", real)
	}
	if paths := uncompared(declared, observed, shape, "spec", "", false, false); len(paths) != 0 {
		t.Fatalf("not compared %v", paths)
	}
	if _, conflicts := merge(observed, labels(field("example.test/row", api.StringValue("2"))), shape, "spec"); !slices.Equal(conflicts, []string{"spec.nodes[worker-0].labels"}) {
		t.Fatalf("conflicts = %v", conflicts)
	}
}

func TestTaintComparisonCanonicalizesOnlyItsKeyAndEffect(t *testing.T) {
	taint := func(key, effect, value string) api.Value {
		return api.MapValue(field("key", api.StringValue(key)), field("effect", api.StringValue(effect)), field("value", api.StringValue(value)))
	}
	a, b := taint("a.test/key", "NoSchedule", "one"), taint("b.test/key", "NoExecute", "two")
	shape := childShape(elementShape(childShape(api.Schema(api.OpenShiftCluster), "nodes")), "taints")
	desired := api.ListValue(b, a, a, taint("a.test/key", "NoSchedule", "ignored"))
	for _, test := range []struct {
		name     string
		observed api.Value
		same     bool
	}{
		{"healthy sorted", api.ListValue(a, b), true}, {"healthy repeated", api.ListValue(b, a, a), true},
		{"changed value", api.ListValue(taint("a.test/key", "NoSchedule", "other"), b), false},
		{"changed effect", api.ListValue(taint("a.test/key", "NoExecute", "one"), b), false},
		{"extra effect", api.ListValue(a, b, taint("a.test/key", "NoExecute", "one")), false},
		{"missing", api.ListValue(b), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := overlay(desired, desired, test.observed, shape, false, false)
			if result.Equal(desired) != test.same {
				t.Fatalf("same=%v want %v", result.Equal(desired), test.same)
			}
		})
	}
	ordinary := &api.Shape{Type: api.Sequence, Element: &api.Shape{Type: api.String}}
	values := api.ListValue(api.StringValue("b"), api.StringValue("a"), api.StringValue("a"))
	if !canonical(values, ordinary).Equal(values) {
		t.Fatal("ordinary list order or repeats changed")
	}
}
