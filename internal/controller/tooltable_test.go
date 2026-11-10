package controller_test

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
)

func TestTheEmbeddedToolTablePinsEachClientAtOrAboveItsMinimum(t *testing.T) {
	pins, err := controller.ToolPins()
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, pin := range pins {
		kinds = append(kinds, pin.Kind)
		if order, comparable := controller.CompareReleases(pin.Default.Version, pin.Minimum); !comparable || order < 0 {
			t.Errorf("%s defaults to %s below its minimum %s", pin.Kind, pin.Default.Version, pin.Minimum)
		}
	}
	if strings.Join(kinds, ",") != "govc,helm,kubectl,virtctl" {
		t.Fatalf("the table pins %v", kinds)
	}
}

func TestATableThatBreaksItsShapeIsRefused(t *testing.T) {
	entry := func(kind, minimum, version, sha string) string {
		return `{"kind":"` + kind + `","minimum":"` + minimum + `","license":"Apache-2.0","default":{"version":"` + version + `","bytes":10,"sha256":"` + sha + `"}}`
	}
	digest := strings.Repeat("a", 64)
	table := func(format string, entries ...string) string {
		return `{"format":"` + format + `","tools":[` + strings.Join(entries, ",") + `]}`
	}
	good := []string{entry("govc", "0.1.0", "0.1.0", digest), entry("helm", "1.0.0", "1.2.0", digest), entry("kubectl", "1.0.0", "1.0.0", digest), entry("virtctl", "1.0.0", "1.0.0", digest)}
	if _, err := controller.ParseToolTable([]byte(table(controller.ToolTableFormat, good...))); err != nil {
		t.Fatalf("a well-formed table was refused: %v", err)
	}
	for name, data := range map[string]string{
		"another format":         table("bootwright.controller.tool-table-v0", good...),
		"a missing client":       table(controller.ToolTableFormat, good[:3]...),
		"clients out of order":   table(controller.ToolTableFormat, good[1], good[0], good[2], good[3]),
		"a default below":        table(controller.ToolTableFormat, entry("govc", "0.2.0", "0.1.0", digest), good[1], good[2], good[3]),
		"a v prefix":             table(controller.ToolTableFormat, entry("govc", "v0.1.0", "v0.1.0", digest), good[1], good[2], good[3]),
		"a prerelease default":   table(controller.ToolTableFormat, entry("govc", "0.1.0", "0.1.1-rc.1", digest), good[1], good[2], good[3]),
		"an uppercase digest":    table(controller.ToolTableFormat, entry("govc", "0.1.0", "0.1.0", strings.Repeat("A", 64)), good[1], good[2], good[3]),
		"a short digest":         table(controller.ToolTableFormat, entry("govc", "0.1.0", "0.1.0", "abc"), good[1], good[2], good[3]),
		"no license":             strings.Replace(table(controller.ToolTableFormat, good...), `"license":"Apache-2.0"`, `"license":""`, 1),
		"a license in prose":     strings.Replace(table(controller.ToolTableFormat, good...), `"license":"Apache-2.0"`, `"license":"Apache License 2"`, 1),
		"an unknown field":       strings.Replace(table(controller.ToolTableFormat, good...), `"tools"`, `"extra":1,"tools"`, 1),
		"two documents":          table(controller.ToolTableFormat, good...) + "{}",
		"a size outside a bound": strings.Replace(table(controller.ToolTableFormat, good...), `"bytes":10`, `"bytes":0`, 1),
	} {
		if _, err := controller.ParseToolTable([]byte(data)); err == nil {
			t.Errorf("%s was admitted", name)
		}
	}
}

func TestReleasesCompareByTheirTripleWithAPrereleaseBelowItsRelease(t *testing.T) {
	for _, test := range []struct {
		a, b  string
		order int
	}{
		{"4.3.1", "4.3.1", 0}, {"v4.3.1", "4.3.1", 0}, {"4.3.2", "4.3.1", 1}, {"4.10.0", "4.9.9", 1},
		{"1.9.0-rc.1", "1.9.0", -1}, {"1.9.0rc1", "1.9.0", -1}, {"1.9.0+build", "1.9.0", 0}, {"0.56.1", "0.56.2", -1},
	} {
		if order, comparable := controller.CompareReleases(test.a, test.b); !comparable || order != test.order {
			t.Errorf("%s against %s orders %d (%t), want %d", test.a, test.b, order, comparable, test.order)
		}
	}
	for _, tag := range []string{"0.54", "1", "latest", "1.02.3"} {
		if _, comparable := controller.CompareReleases(tag, "1.0.0"); comparable {
			t.Errorf("%s compared as a release", tag)
		}
	}
}

func TestADeclaredReleaseBelowTheMinimumIsRefusedOnlyWhereItsClientIsSelected(t *testing.T) {
	helm, _, err := controller.PinOf("helm")
	if err != nil {
		t.Fatal(err)
	}
	graph := func(declared string, cluster bool) api.Catalog {
		objects := selectionObjects()
		if declared != "" {
			objects[0] = api.NewObject(api.Environment, "example", api.Value{}, objects[0].Spec().WithPath(api.StringValue(declared), "dependencyVersions", "helm"))
		}
		if cluster {
			objects = append(objects, api.NewObject(api.OpenShiftCluster, "cluster", api.Value{}, api.MapValue().WithPath(api.StringValue("openshift"), "distribution", "type").WithPath(api.StringValue("4.21.15"), "distribution", "release", "version")))
		}
		return api.NewCatalog(objects)
	}
	for _, accepted := range []string{"", helm.Minimum, "99.0.0"} {
		if shapes := controller.Unsupported(graph(accepted, true)); len(shapes) != 0 {
			t.Errorf("helm %q was refused: %+v", accepted, shapes)
		}
	}
	if shapes := controller.Unsupported(graph("1.0.0", false)); len(shapes) != 0 {
		t.Errorf("an unselected helm below the minimum was refused: %+v", shapes)
	}
	for declared, phrase := range map[string]string{"1.0.0": "accepts helm " + helm.Minimum + " or newer", helm.Minimum + "-rc.1": "accepts helm " + helm.Minimum + " or newer", "4.3": "which names none"} {
		shapes := controller.Unsupported(graph(declared, true))
		if len(shapes) != 1 || shapes[0].Field != "$.spec.dependencyVersions.helm" || !strings.Contains(shapes[0].Reason, phrase) || !strings.Contains(shapes[0].Remediation, "or remove it for this build's default") {
			t.Errorf("helm %q: %+v", declared, shapes)
		}
	}
}

func mirroredGraph(spec func(api.Value) api.Value, extra ...api.Object) api.Catalog {
	objects := selectionObjects()
	objects[0] = api.NewObject(api.Environment, "example", api.Value{}, spec(objects[0].Spec().WithPath(api.StringValue("https://mirror.example.test/tools"), "mirror", "artifacts", "url")))
	return api.NewCatalog(append(objects, extra...))
}

func openshiftCluster(distribution string) api.Object {
	return api.NewObject(api.OpenShiftCluster, "cluster", api.Value{}, api.MapValue().WithPath(api.StringValue(distribution), "distribution", "type").WithPath(api.StringValue("4.21.15"), "distribution", "release", "version"))
}

func TestADeclaredArtifactMirrorServesEveryPublicOriginClientAndNoVendorOne(t *testing.T) {
	same := func(spec api.Value) api.Value { return spec }
	vsphere := api.NewObject(api.InfraProvider, "vsphere", api.Value{}, api.MapValue().With("vsphere", api.MapValue()))
	guest := api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue().WithPath(api.StringValue("vsphere"), "substrate", "providerRef"))
	tools, err := controller.SelectTools(mirroredGraph(same, openshiftCluster("openshift"), vsphere, guest))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"helm": "https://mirror.example.test/tools/helm/" + defaultTool(t, "helm").Version, "govc": "https://mirror.example.test/tools/govc", "openshift-clients": "", "openshift-install": ""}
	for _, tool := range tools {
		if mirror, listed := want[tool.Kind]; !listed || tool.Mirror != mirror {
			t.Errorf("%s takes the mirror %q, want %q", tool.Kind, tool.Mirror, mirror)
		}
		if controller.PublicOrigin(tool) && tool.SHA256 == "" {
			t.Errorf("%s behind the mirror lost its pin", tool.Kind)
		}
	}
	if shapes := controller.Unsupported(mirroredGraph(same, openshiftCluster("openshift"))); len(shapes) != 0 {
		t.Fatalf("a mirror serving pinned defaults was refused: %+v", shapes)
	}
}

func TestADeclaredArtifactMirrorRefusesWhatItCannotServe(t *testing.T) {
	for name, test := range map[string]struct {
		spec   func(api.Value) api.Value
		extra  []api.Object
		field  string
		phrase string
	}{
		"a per-client mirror beside it": {func(spec api.Value) api.Value {
			return spec.WithPath(api.StringValue("https://other.example.test"), "downloads", "helmMirror")
		}, []api.Object{openshiftCluster("openshift")}, "$.spec.downloads.helmMirror", "names it again"},
		"an unpinned client release": {func(spec api.Value) api.Value {
			return spec.WithPath(api.StringValue("99.0.0"), "dependencyVersions", "helm")
		}, []api.Object{openshiftCluster("openshift")}, "$.spec.mirror.artifacts", "selects helm 99.0.0"},
		"the OKD clients": {func(spec api.Value) api.Value { return spec }, []api.Object{openshiftCluster("okd")}, "$.spec.mirror.artifacts", "select an openshift distribution"},
	} {
		shapes := controller.Unsupported(mirroredGraph(test.spec, test.extra...))
		found := false
		for _, shape := range shapes {
			found = found || shape.Field == test.field && (strings.Contains(shape.Reason, test.phrase) || strings.Contains(shape.Remediation, test.phrase))
		}
		if !found {
			t.Errorf("%s: %+v", name, shapes)
		}
	}
}
