package controller_test

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/tooltable"
)

func TestADeclaredReleaseBelowTheMinimumIsRefusedOnlyWhereItsClientIsSelected(t *testing.T) {
	helm, _, err := tooltable.PinOf("helm")
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
	guest := api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue().WithPath(api.StringValue("vsphere"), "hardware", "providerRef"))
	tools, err := controller.SelectTools(mirroredGraph(same, openshiftCluster("openshift"), vsphere, guest))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"helm": "https://mirror.example.test/tools/helm/" + defaultTool(t, "helm").Version, "govc": "https://mirror.example.test/tools/govc", "openshift-clients": "", "openshift-install": ""}
	for _, tool := range tools {
		if mirror, listed := want[tool.Kind]; !listed || tool.Mirror != mirror {
			t.Errorf("%s takes the mirror %q, want %q", tool.Kind, tool.Mirror, mirror)
		}
		if tooltable.PublicOrigin(tool) && tool.SHA256 == "" {
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

func TestProviderToolsUseTheSharedArtifactMirror(t *testing.T) {
	catalog := mirroredGraph(func(spec api.Value) api.Value { return spec })
	for _, kind := range []string{"govc", "virtctl"} {
		tool, err := tooltable.ProviderTool(catalog, kind)
		if err != nil {
			t.Fatal(err)
		}
		expected := defaultTool(t, kind)
		if tool.Mirror != "https://mirror.example.test/tools/"+kind || tool.Version != expected.Version || tool.SHA256 != expected.SHA256 {
			t.Errorf("%s does not use its pinned shared mirror: %+v", kind, tool)
		}
	}
}
