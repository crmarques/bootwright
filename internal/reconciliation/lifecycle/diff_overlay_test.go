package lifecycle

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func filedState(authored, effective []api.Object, paths map[string]string) *compilation.State {
	environment, _ := diffState().Authored().Find(api.Environment, "lab")
	origins := map[string]diagnostics.SourceLocation{}
	for identity, path := range paths {
		origins[identity] = diagnostics.SourceLocation{Path: "/synthetic/" + path, Document: 1}
	}
	return compilation.NewState(api.NewCatalog(append([]api.Object{environment}, authored...)), api.NewCatalog(append([]api.Object{environment}, effective...)), origins)
}

func text(name, value string) api.FieldValue { return field(name, api.StringValue(value)) }

func tlsServer(name, image string, tls ...api.FieldValue) api.Object {
	return api.NewObject(api.ArtifactServer, name, api.Value{}, api.MapValue(text("image", image), field("tls", api.MapValue(append([]api.FieldValue{text("secretRef", "tls")}, tls...)...))))
}

func listener(name, protocol, port string) api.Value {
	return api.MapValue(text("name", name), text("protocol", protocol), field("port", api.IntegerValue(port)))
}

func (f diffFixture) projecting(t *testing.T, paths []ProjectedPath, project func(api.Catalog, reconciliation.Block, json.RawMessage) ([]Projection, error)) *projectingCapability {
	t.Helper()
	projecting := &projectingCapability{testCapability: f.capability, answers: map[string]scriptedObservation{}, paths: paths, project: project}
	f.service.capabilities = testResolver{capability: projecting}
	return projecting
}

func TestDiffComparesEachSideAsAuthoredInItsFile(t *testing.T) {
	f := newDiffHarness(t, "alpha")
	authored := []api.Object{tlsServer("alpha", "v1")}
	effective := []api.Object{tlsServer("alpha", "v1", text("minVersion", "TLSv1.2"))}
	f.compiler.states[digestOf(f.workspace.inputs)] = filedState(authored, effective, map[string]string{"ArtifactServer/alpha": "servers/alpha.yaml", "Environment/lab": "environment.yaml"})
	f.apply(t)
	f.publish("rev-11111111111111111111111111111111", "kind: Environment\nmoved: true\n",
		filedState(authored, effective, map[string]string{"ArtifactServer/alpha": "servers/web.yaml", "Environment/lab": "environment.yaml"}))
	result := f.diff(t)
	alpha, environment := objectOf(t, result, api.ArtifactServer, "alpha"), objectOf(t, result, api.Environment, "lab")
	if alpha.Path != "servers/web.yaml" || alpha.Real.Path != "servers/alpha.yaml" || environment.Path != "environment.yaml" || environment.Real.Path != "environment.yaml" {
		t.Fatalf("alpha in %q beside %q, environment in %q beside %q", alpha.Path, alpha.Real.Path, environment.Path, environment.Real.Path)
	}
	if alpha.Desired == nil || alpha.Desired.Spec().Has("tls", "minVersion") || !sameObject(alpha.Real.Object, alpha.Desired) || alpha.Real.Discovered {
		t.Fatalf("alpha reads %+v beside %+v", alpha.Real, alpha.Desired)
	}
}

func TestAnObservationOverlaysTheAppliedObject(t *testing.T) {
	f := newDiffHarness(t, "alpha", "bravo", "charlie")
	listened := api.NewObject(api.ArtifactServer, "charlie", api.Value{}, api.MapValue(text("image", "v1"),
		field("listeners", api.ListValue(listener("http", "http", "80"), listener("https", "https", "443")))))
	authored := []api.Object{tlsServer("alpha", "v1"), tlsServer("bravo", "v1"), listened}
	effective := []api.Object{tlsServer("alpha", "v1", text("minVersion", "TLSv1.2")), tlsServer("bravo", "v1", text("minVersion", "TLSv1.2")), listened}
	f.compiler.states[digestOf(f.workspace.inputs)] = filedState(authored, effective, nil)
	f.apply(t)
	observed := map[string]api.Value{
		"alpha":   api.MapValue(text("image", "v2"), field("tls", api.MapValue(text("minVersion", "TLSv1.2")))),
		"bravo":   api.MapValue(text("image", "v1"), field("tls", api.MapValue(text("minVersion", "TLSv1.3")))),
		"charlie": api.MapValue(text("image", "v1"), field("listeners", api.ListValue(listener("http", "http", "80"), listener("admin", "http", "9000")))),
	}
	f.projecting(t, []ProjectedPath{
		{Kind: api.ArtifactServer, Path: "spec.image"}, {Kind: api.ArtifactServer, Path: "spec.tls.minVersion"},
		{Kind: api.ArtifactServer, Path: "spec.listeners[].name"}, {Kind: api.ArtifactServer, Path: "spec.listeners[].protocol"}, {Kind: api.ArtifactServer, Path: "spec.listeners[].port"},
	}, func(_ api.Catalog, block reconciliation.Block, _ json.RawMessage) ([]Projection, error) {
		return []Projection{{Kind: api.ArtifactServer, Name: block.Object, Observed: observed[block.Object]}}, nil
	})
	result := f.observe(t)
	alpha, bravo, charlie := objectOf(t, result, api.ArtifactServer, "alpha"), objectOf(t, result, api.ArtifactServer, "bravo"), objectOf(t, result, api.ArtifactServer, "charlie")
	if want := tlsServer("alpha", "v2"); !sameObject(alpha.Real.Object, canonicalObject(want)) || !sameObject(alpha.Desired, canonicalObject(authored[0])) ||
		!slices.Equal(alpha.Real.NotCompared, []string{"spec.tls.secretRef"}) {
		t.Fatalf("a scalar beside a default observed as applied reads %+v against %+v", alpha.Real, alpha.Desired)
	}
	if want := tlsServer("bravo", "v1", text("minVersion", "TLSv1.3")); !sameObject(bravo.Real.Object, canonicalObject(want)) {
		t.Fatalf("a default observed otherwise reads %+v", bravo.Real.Object)
	}
	listeners := charlie.Real.Object.Spec().Get("listeners")
	if want := api.ListValue(listener("admin", "http", "9000"), listener("http", "http", "80"), listener("https", "https", "443")); !listeners.Equal(want) ||
		!slices.Equal(charlie.Real.NotCompared, []string{"spec.listeners[https].protocol", "spec.listeners[https].port"}) ||
		charlie.Desired.Spec().Get("listeners").Len() != 2 {
		t.Fatalf("a named list reads %+v, not compared %v, against %+v", listeners, charlie.Real.NotCompared, charlie.Desired)
	}
}

func profileMachine(name, profile string) api.Object {
	return api.NewObject(api.Machine, name, api.Value{}, api.MapValue(field("os", api.MapValue(field("provided", api.BoolValue(true)), text("installProfileRef", profile)))))
}

func installProfile(name, version string) api.Object {
	return api.NewObject(api.MachineInstallProfile, name, api.Value{}, api.MapValue(field("os", api.MapValue(text("family", "rhel"), text("version", version)))))
}

func splitFixture(t *testing.T, applied string, versions map[string]string, extra ...api.Object) (diffFixture, *DiffResult) {
	t.Helper()
	f := newDiffHarness(t, "alpha", "bravo", "charlie")
	holders := map[string]string{"alpha": "rhel-01", "bravo": "rhel-02", "charlie": "rhel-03"}
	objects := append([]api.Object{installProfile("rhel", applied)}, servers("v1", "alpha", "bravo", "charlie")...)
	for _, block := range []string{"alpha", "bravo", "charlie"} {
		objects = append(objects, profileMachine(holders[block], "rhel"))
	}
	objects = append(objects, extra...)
	f.compiler.states[digestOf(f.workspace.inputs)] = filedState(objects, objects, map[string]string{"MachineInstallProfile/rhel": "profiles/rhel.yaml"})
	f.apply(t)
	f.projecting(t, []ProjectedPath{{Kind: api.ArtifactServer, Path: "spec.image"}, {Kind: api.MachineInstallProfile, Path: "spec.os.version"}},
		func(_ api.Catalog, block reconciliation.Block, _ json.RawMessage) ([]Projection, error) {
			observed := api.MapValue(field("os", api.MapValue(text("version", versions[block.Object]))))
			switch versions[block.Object] {
			case "":
				return nil, nil
			case "unread":
				observed = api.MapValue()
			}
			return []Projection{{
				Kind: api.MachineInstallProfile, Name: "rhel", Observed: observed,
				Holder: &ProjectionHolder{Kind: api.Machine, Name: holders[block.Object], Field: "spec.os.installProfileRef"},
			}}, nil
		})
	return f, f.observe(t)
}

func TestHoldersThatReadASharedObjectDifferentlySplitIt(t *testing.T) {
	_, result := splitFixture(t, "9.4", map[string]string{"alpha": "9.4", "bravo": "9.6", "charlie": "9.4"})
	rhel, copied := objectOf(t, result, api.MachineInstallProfile, "rhel"), objectOf(t, result, api.MachineInstallProfile, "rhel-rhel-02")
	if rhel.Real.Object.Spec().Get("os", "version").Text() != "9.4" || rhel.Real.Discovered || len(rhel.Real.Notes) != 0 {
		t.Fatalf("the object the applied group keeps reads %+v", rhel.Real)
	}
	note := "split from MachineInstallProfile/rhel: Machine/rhel-02 observe spec.os.version 9.6"
	if copied.Desired != nil || copied.Real.State != "present" || copied.Real.Source != "observed" || !copied.Real.Discovered || copied.Real.Path != "profiles/rhel-rhel-02.yaml" ||
		copied.Real.Object.Spec().Get("os", "version").Text() != "9.6" || copied.Real.Object.Spec().Get("os", "family").Text() != "rhel" || !slices.Contains(copied.Real.Notes, note) {
		t.Fatalf("the copy reads %+v", copied.Real)
	}
	for machine, profile := range map[string]string{"rhel-01": "rhel", "rhel-02": "rhel-rhel-02", "rhel-03": "rhel"} {
		if holder := objectOf(t, result, api.Machine, machine); holder.Real.Object.Spec().Get("os", "installProfileRef").Text() != profile {
			t.Fatalf("%s reads %+v", machine, holder.Real.Object)
		}
	}
}

func TestAHolderObservingNoDisputedFieldKeepsTheObject(t *testing.T) {
	_, result := splitFixture(t, "9.4", map[string]string{"alpha": "9.4", "bravo": "9.6", "charlie": "unread"})
	var copies []string
	for _, object := range result.Objects {
		if object.Kind == api.MachineInstallProfile && object.Name != "rhel" {
			copies = append(copies, object.Name)
		}
	}
	if !slices.Equal(copies, []string{"rhel-rhel-02"}) {
		t.Fatalf("the split made copies %v", copies)
	}
	for machine, profile := range map[string]string{"rhel-01": "rhel", "rhel-02": "rhel-rhel-02", "rhel-03": "rhel"} {
		if holder := objectOf(t, result, api.Machine, machine); holder.Real.Object.Spec().Get("os", "installProfileRef").Text() != profile {
			t.Fatalf("%s reads %+v", machine, holder.Real.Object)
		}
	}
}

func TestADiscoveredFileNameIsOnePathElement(t *testing.T) {
	names := claimedNames{names: map[string]bool{}, paths: map[string][]string{}}
	for _, base := range []string{"../x", "a/b", `a\b`, "..", ".", ""} {
		if path := names.place("servers", base, "ArtifactServer/x"); path != "" {
			t.Fatalf("%q is placed at %q", base, path)
		}
	}
	if path := names.place("servers", "artifactserver-x", "ArtifactServer/x"); path != "servers/artifactserver-x.yaml" {
		t.Fatalf("a clean name is placed at %q", path)
	}
}

func TestWithoutAnAppliedGroupTheLargestKeepsTheObjectAndTakenNamesMoveOn(t *testing.T) {
	_, result := splitFixture(t, "9.2", map[string]string{"alpha": "9.6", "bravo": "9.4", "charlie": "9.6"}, installProfile("rhel-rhel-02", "9.0"))
	rhel, copied := objectOf(t, result, api.MachineInstallProfile, "rhel"), objectOf(t, result, api.MachineInstallProfile, "rhel-rhel-02-2")
	if rhel.Real.Object.Spec().Get("os", "version").Text() != "9.6" || copied.Real.Object.Spec().Get("os", "version").Text() != "9.4" ||
		copied.Real.Path != "profiles/rhel-rhel-02-2.yaml" {
		t.Fatalf("rhel reads %+v, its copy %+v", rhel.Real, copied.Real)
	}
	if holder := objectOf(t, result, api.Machine, "rhel-02"); holder.Real.Object.Spec().Get("os", "installProfileRef").Text() != "rhel-rhel-02-2" {
		t.Fatalf("rhel-02 reads %+v", holder.Real.Object)
	}
	_, tied := splitFixture(t, "9.2", map[string]string{"alpha": "9.6", "bravo": "9.4"})
	if rhel := objectOf(t, tied, api.MachineInstallProfile, "rhel"); rhel.Real.Object.Spec().Get("os", "version").Text() != "9.6" {
		t.Fatalf("a tie kept %+v", rhel.Real.Object)
	}
	objectOf(t, tied, api.MachineInstallProfile, "rhel-rhel-02")
}

func TestAHolderMustBeAppliedAndReferenceTheProjectedKind(t *testing.T) {
	for name, tt := range map[string]struct {
		holder ProjectionHolder
		reason string
	}{
		"an undeclared holder": {ProjectionHolder{Kind: api.Machine, Name: "rhel-09", Field: "spec.os.installProfileRef"},
			"its projection of MachineInstallProfile/rhel names a holder the applied revision does not declare: Machine/rhel-09"},
		"a path for a holder": {ProjectionHolder{Kind: api.Machine, Name: "../rhel-01", Field: "spec.os.installProfileRef"},
			`its projection of MachineInstallProfile/rhel names a holder by an invalid name: "../rhel-01"`},
		"a field of another kind": {ProjectionHolder{Kind: api.Machine, Name: "rhel-01", Field: "spec.substrate.providerRef"},
			"its projection of MachineInstallProfile/rhel names a holder field that does not reference MachineInstallProfile: spec.substrate.providerRef"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newDiffHarness(t, "alpha")
			objects := append([]api.Object{installProfile("rhel", "9.4"), profileMachine("rhel-01", "rhel")}, servers("v1", "alpha")...)
			f.compiler.states[digestOf(f.workspace.inputs)] = filedState(objects, objects, nil)
			f.apply(t)
			f.projecting(t, []ProjectedPath{{Kind: api.MachineInstallProfile, Path: "spec.os.version"}}, func(api.Catalog, reconciliation.Block, json.RawMessage) ([]Projection, error) {
				return []Projection{{Kind: api.MachineInstallProfile, Name: "rhel", Observed: api.MapValue(field("os", api.MapValue(text("version", "9.4")))), Holder: &tt.holder}}, nil
			})
			if alpha := objectOf(t, f.observe(t), api.ArtifactServer, "alpha"); alpha.Real.State != "unknown" || alpha.Real.Reason != tt.reason {
				t.Fatalf("alpha reads %+v", alpha.Real)
			}
		})
	}
}

type discoveringCapability struct {
	*projectingCapability
	kinds []api.Kind
}

func (d discoveringCapability) Discovers() []api.Kind { return d.kinds }

func TestADiscoveredObjectIsUnmanagedBesideARemovedOne(t *testing.T) {
	f := newDiffHarness(t, "alpha", "bravo")
	paths := map[string]string{"ArtifactServer/alpha": "servers/alpha.yaml", "ArtifactServer/bravo": "servers/bravo.yaml", "Environment/lab": "servers/artifactserver-zulu.yaml"}
	f.compiler.states[digestOf(f.workspace.inputs)] = filedState(servers("v1", "alpha", "bravo"), servers("v1", "alpha", "bravo"), paths)
	f.apply(t)
	f.publish("rev-22222222222222222222222222222222", "kind: Environment\nrevised: true\n", filedState(servers("v1", "alpha"), servers("v1", "alpha"), paths))
	projecting := f.projecting(t, []ProjectedPath{{Kind: api.ArtifactServer, Path: "spec.image"}}, func(_ api.Catalog, block reconciliation.Block, _ json.RawMessage) ([]Projection, error) {
		projections := []Projection{{Kind: api.ArtifactServer, Name: block.Object, Observed: api.MapValue(text("image", "v1"))}}
		if block.Object == "alpha" {
			projections = append(projections, Projection{Kind: api.ArtifactServer, Name: "zulu", Observed: api.MapValue(text("image", "v9")), Notes: []string{"its retention could not be read"}})
		}
		return projections, nil
	})
	if alpha := objectOf(t, f.observe(t), api.ArtifactServer, "alpha"); alpha.Real.State != "unknown" ||
		alpha.Real.Reason != "its projection names an object the applied revision does not declare: ArtifactServer/zulu" {
		t.Fatalf("an undiscovering capability's undeclared object read alpha %+v", alpha.Real)
	}
	f.service.capabilities = testResolver{capability: discoveringCapability{projectingCapability: projecting, kinds: []api.Kind{api.ArtifactServer}}}
	result := f.observe(t)
	zulu, bravo := objectOf(t, result, api.ArtifactServer, "zulu"), objectOf(t, result, api.ArtifactServer, "bravo")
	if zulu.Desired != nil || zulu.Real.State != "present" || zulu.Real.Source != "observed" || !zulu.Real.Discovered || zulu.Real.Path != "servers/artifactserver-zulu-2.yaml" ||
		zulu.Real.Object.Spec().Get("image").Text() != "v9" || !slices.Equal(zulu.Real.Notes, []string{"its retention could not be read"}) {
		t.Fatalf("zulu reads %+v", zulu.Real)
	}
	if bravo.Desired != nil || bravo.Real.State != "present" || bravo.Real.Discovered || bravo.Real.Path != "servers/bravo.yaml" {
		t.Fatalf("bravo reads %+v", bravo.Real)
	}
}

func TestDiffCarriesTheMarkersOfBothRevisions(t *testing.T) {
	f := newDiffHarness(t, "alpha")
	marked := func(text, marker string) desiredstate.Sources {
		sources := diffSources(text)
		sources.Markers = []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/addons/"+marker, []byte(marker))}
		return sources
	}
	applied := marked("kind: Environment\n", "applied.marker")
	f.compiler.states[digestOf(applied)] = diffState(servers("v1", "alpha")...)
	f.workspace.inputs = applied
	f.apply(t)
	selected := marked("kind: Environment\nrevised: true\n", "selected.marker")
	f.compiler.states[digestOf(selected)] = diffState(servers("v1", "alpha")...)
	f.update("rev-33333333333333333333333333333333", selected)
	files := func(markers []desiredstate.SourceFile) []string {
		var named []string
		for _, marker := range markers {
			named = append(named, marker.Path()+"="+string(marker.Bytes()))
		}
		return named
	}
	result := f.diff(t)
	if !slices.Equal(files(result.Markers.Desired), []string{"addons/selected.marker=selected.marker"}) || !slices.Equal(files(result.Markers.Applied), []string{"addons/applied.marker=applied.marker"}) {
		t.Fatalf("markers = %v beside %v", files(result.Markers.Desired), files(result.Markers.Applied))
	}
	delete(f.workspace.revisions, testRevision)
	if result, err := f.service.Diff(context.Background(), DiffRequest{ContextName: "lab"}); err != nil || len(result.Markers.Applied) != 0 || len(result.Markers.Desired) != 1 {
		t.Fatalf("an unreadable revision's markers = %+v (%v)", result, err)
	}
}
