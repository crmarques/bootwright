package libvirt

import (
	"encoding/json"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const projectionSpec = "../../../specs/state-reconciliation.md"

func projectionRequest(t *testing.T, data ...Disk) MachineRequest {
	t.Helper()
	request := machineRequest(t)
	request.Disks = slices.Concat(request.Disks, data)
	return request
}

func projectionBlock(t *testing.T, request MachineRequest) reconciliation.Block {
	t.Helper()
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{ID: "machine-" + request.Identity.Object, Request: canonical},
		RequestDigest:   evidenceDigest,
	}
}

func projectionEvidence(t *testing.T, name string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile("testdata/projection-" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func project(t *testing.T, applied api.Catalog, request MachineRequest, evidence json.RawMessage) ([]lifecycle.Projection, error) {
	t.Helper()
	return NewMachine(nil).Project(applied, projectionBlock(t, request), evidence)
}

func mustProject(t *testing.T, request MachineRequest, evidence json.RawMessage) []lifecycle.Projection {
	t.Helper()
	projections, err := project(t, labCatalog(), request, evidence)
	if err != nil {
		t.Fatalf("project = %v", diagnostics.Of(err))
	}
	if len(projections) != 2 || projections[0].Kind != api.Machine || projections[1].Kind != api.InfraProvider {
		t.Fatalf("project = %+v, want the Machine then the InfraProvider", projections)
	}
	return projections
}

func profile(fields ...api.FieldValue) api.Value {
	return api.MapValue(field("libvirt", api.MapValue(field("machineProfiles", api.ListValue(api.MapValue(fields...))))))
}

func dataDisk(name, size string, media ...string) api.Value {
	fields := []api.FieldValue{text("name", name), number("sizeGiB", size)}
	for _, value := range media {
		fields = append(fields, text("media", value))
	}
	return api.MapValue(fields...)
}

func requireProjection(t *testing.T, got lifecycle.Projection, kind api.Kind, name string, observed api.Value, notes []string) {
	t.Helper()
	if got.Kind != kind || got.Name != name || !got.Observed.Equal(observed) || !slices.Equal(got.Notes, notes) {
		t.Fatalf("projection = %s/%s %s notes %q\nwant %s/%s %s notes %q", got.Kind, got.Name, render(got.Observed), got.Notes, kind, name, render(observed), notes)
	}
}

func render(value api.Value) string {
	switch value.Type() {
	case api.Mapping:
		fields := value.Fields()
		sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
		parts := make([]string, 0, len(fields))
		for _, entry := range fields {
			parts = append(parts, entry.Name+":"+render(entry.Value))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case api.Sequence:
		parts := make([]string, 0, value.Len())
		for _, item := range value.Items() {
			parts = append(parts, render(item))
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	return value.Text()
}

func machineSpec() api.Value {
	return api.MapValue(field("substrate", api.MapValue(text("providerRef", "lab-libvirt"))))
}

func TestMachineProjectionRendersObservedDiskSizes(t *testing.T) {
	request := projectionRequest(t, Disk{Name: "data1", SizeGiB: 50}, Disk{Name: "data2", SizeGiB: 20})
	projections := mustProject(t, request, projectionEvidence(t, "resized-root"))
	requireProjection(t, projections[0], api.Machine, "rhel-01", machineSpec(), []string{
		"machine-rhel-01: domain running", "machine-rhel-01: its controller reports power On",
	})
	requireProjection(t, projections[1], api.InfraProvider, "lab-libvirt", profile(
		text("name", "rhel"), number("diskGiB", "100"),
		field("dataDisks", api.ListValue(dataDisk("data1", "50"), dataDisk("data2", "20"))),
	), nil)
}

func TestMachineProjectionOmitsDataDisksTheRequestFreezesNone(t *testing.T) {
	projections := mustProject(t, projectionRequest(t), projectionEvidence(t, "root-only"))
	requireProjection(t, projections[1], api.InfraProvider, "lab-libvirt", profile(text("name", "rhel"), number("diskGiB", "60")), nil)
}

func TestMachineProjectionMarksAnSSDDataDisk(t *testing.T) {
	request := projectionRequest(t, Disk{Name: "data1", SizeGiB: 50, Media: "ssd", Bus: "scsi", Target: "sda", RotationRate: 1})
	projections := mustProject(t, request, projectionEvidence(t, "ssd-data"))
	requireProjection(t, projections[1], api.InfraProvider, "lab-libvirt", profile(
		text("name", "rhel"), number("diskGiB", "60"), field("dataDisks", api.ListValue(dataDisk("data1", "50", "ssd"))),
	), nil)
}

func TestMachineProjectionNotesAMissingDisk(t *testing.T) {
	request := projectionRequest(t, Disk{Name: "data1", SizeGiB: 50})
	projections := mustProject(t, request, projectionEvidence(t, "missing-disk"))
	requireProjection(t, projections[1], api.InfraProvider, "lab-libvirt", profile(
		text("name", "rhel"), number("diskGiB", "60"), field("dataDisks", api.ListValue()),
	), []string{"machine-rhel-01: disk data1 is missing"})
}

func TestMachineProjectionOmitsARootDiskTheHostDoesNotShow(t *testing.T) {
	var evidence MachineEvidence
	if err := json.Unmarshal(projectionEvidence(t, "root-only"), &evidence); err != nil {
		t.Fatal(err)
	}
	evidence.Disks = nil
	projections := mustProject(t, projectionRequest(t), encode(t, evidence))
	requireProjection(t, projections[1], api.InfraProvider, "lab-libvirt", profile(text("name", "rhel")),
		[]string{"machine-rhel-01: disk root is missing"})
}

func TestMachineProjectionRefusesEvidenceItDoesNotOwn(t *testing.T) {
	for name, damage := range map[string]func(*MachineEvidence){
		"not owned":      func(e *MachineEvidence) { e.Owned = false },
		"not answered":   func(e *MachineEvidence) { e.Answered = false },
		"another domain": func(e *MachineEvidence) { e.Domain = "bootwright-lab-rhel-02" },
	} {
		t.Run(name, func(t *testing.T) {
			var evidence MachineEvidence
			if err := json.Unmarshal(projectionEvidence(t, "root-only"), &evidence); err != nil {
				t.Fatal(err)
			}
			damage(&evidence)
			projections, err := project(t, labCatalog(), projectionRequest(t), encode(t, evidence))
			reported := diagnostics.Of(err)
			if projections != nil || len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
				reported[0].Message != "the machine evidence does not prove this context's domain" {
				t.Fatalf("project = %+v, %+v", projections, reported)
			}
		})
	}
}

func TestMachineProjectionRefusesEvidenceOfAnotherRequest(t *testing.T) {
	var evidence MachineEvidence
	if err := json.Unmarshal(projectionEvidence(t, "root-only"), &evidence); err != nil {
		t.Fatal(err)
	}
	evidence.Request = strings.Repeat("a", 64)
	if projections, err := project(t, labCatalog(), projectionRequest(t), encode(t, evidence)); err == nil || projections != nil {
		t.Fatalf("project = %+v, %v", projections, err)
	}
}

func TestMachineProjectionRefusesAMachineTheAppliedRevisionDoesNotPlace(t *testing.T) {
	unplaced := api.NewObject(api.Machine, "rhel-01", api.Value{}, api.MapValue(
		field("os", api.MapValue(field("provided", api.BoolValue(true)))),
	))
	noProfile := api.NewObject(api.Machine, "rhel-01", api.Value{}, api.MapValue(
		field("substrate", api.MapValue(text("providerRef", "lab-libvirt"))),
	))
	for name, catalog := range map[string]api.Catalog{
		"missing":       catalogOf(controller(), provider()),
		"no substrate":  catalogOf(controller(), provider(), unplaced),
		"no profileRef": catalogOf(controller(), provider(), noProfile),
	} {
		t.Run(name, func(t *testing.T) {
			projections, err := project(t, catalog, projectionRequest(t), projectionEvidence(t, "root-only"))
			reported := diagnostics.Of(err)
			if projections != nil || len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
				reported[0].Message != "the applied revision does not place Machine/rhel-01 on a libvirt profile" {
				t.Fatalf("project = %+v, %+v", projections, reported)
			}
		})
	}
}

func projectedChildShape(shape *api.Shape, name string) *api.Shape {
	if field, found := shape.Field(name); found {
		return field.Shape
	}
	for _, alternative := range shape.Alternatives {
		if alternative == nil {
			continue
		}
		if field, found := alternative.Field(name); found {
			return field.Shape
		}
	}
	return nil
}

func projectedNameKey(shape *api.Shape) string {
	if shape == nil || shape.Type != api.Sequence {
		return ""
	}
	return shape.NameKey
}

func projectedLeaves(value api.Value, shape *api.Shape, prefix string) []string {
	switch {
	case !value.Present():
		return nil
	case value.Type() == api.Mapping && shape != nil:
		var paths []string
		for _, entry := range value.Fields() {
			paths = append(paths, projectedLeaves(entry.Value, projectedChildShape(shape, entry.Name), prefix+"."+entry.Name)...)
		}
		return paths
	case value.Type() == api.Sequence && projectedNameKey(shape) != "":
		if value.Len() == 0 {
			return []string{prefix + "[]." + shape.NameKey}
		}
		var paths []string
		for _, item := range value.Items() {
			paths = append(paths, projectedLeaves(item, shape.Element, prefix+"[]")...)
		}
		return paths
	}
	return []string{prefix}
}

func TestMachineProjectionClaimsOnlyItsProjectedPaths(t *testing.T) {
	capability := NewMachine(nil)
	cases := map[string]struct {
		request  MachineRequest
		evidence string
	}{
		"resized root":  {projectionRequest(t, Disk{Name: "data1", SizeGiB: 50}, Disk{Name: "data2", SizeGiB: 20}), "resized-root"},
		"ssd data disk": {projectionRequest(t, Disk{Name: "data1", SizeGiB: 50, Media: "ssd", Bus: "scsi", Target: "sda", RotationRate: 1}), "ssd-data"},
		"missing disk":  {projectionRequest(t, Disk{Name: "data1", SizeGiB: 50}), "missing-disk"},
		"root only":     {projectionRequest(t), "root-only"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			for _, projection := range mustProject(t, test.request, projectionEvidence(t, test.evidence)) {
				leaves := projectedLeaves(projection.Observed, api.Schema(projection.Kind), "spec")
				if len(leaves) == 0 {
					t.Fatalf("%s/%s claims no field", projection.Kind, projection.Name)
				}
				for _, leaf := range leaves {
					if !slices.Contains(capability.ProjectedPaths(), lifecycle.ProjectedPath{Kind: projection.Kind, Path: leaf}) {
						t.Errorf("%s/%s claims %s, which ProjectedPaths does not list", projection.Kind, projection.Name, leaf)
					}
				}
			}
		})
	}
}

func TestMachineProjectionCarriesNoSecretReference(t *testing.T) {
	request := projectionRequest(t, Disk{Name: "data1", SizeGiB: 50})
	secretNames := slices.Concat(request.SecretReferences(), []string{
		"credentialsRef", "certificateRef", "privateKeyRef", "knownHostsRef", "lab-bmc-credentials", "lab-bmc-tls", "hypervisor-key", "host-key",
	})
	for _, projection := range mustProject(t, request, projectionEvidence(t, "resized-root")) {
		rendered := projection.Name + render(projection.Observed) + strings.Join(projection.Notes, "\n")
		for _, name := range secretNames {
			if name != "" && strings.Contains(rendered, name) {
				t.Errorf("%s/%s carries %q", projection.Kind, projection.Name, name)
			}
		}
	}
}

func tableRows(t *testing.T, implementation string) map[api.Kind][]string {
	t.Helper()
	data, err := os.ReadFile(projectionSpec)
	if err != nil {
		t.Fatal(err)
	}
	_, table, found := strings.Cut(string(data), "### Observable fields by implementation")
	if !found {
		t.Fatal("the spec has no observable-field table")
	}
	rows := map[api.Kind][]string{}
	for _, line := range strings.Split(table, "\n") {
		if strings.HasPrefix(line, "## ") {
			break
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) < 3 || strings.Trim(strings.TrimSpace(cells[0]), "`") != implementation {
			continue
		}
		kind := api.Kind(strings.Trim(strings.TrimSpace(cells[1]), "`"))
		base := ""
		for _, field := range strings.Split(cells[2], ",") {
			path := strings.Trim(strings.TrimSpace(field), "`")
			if strings.HasPrefix(path, "[]") {
				path = base + path
			} else {
				base, _, _ = strings.Cut(path, "[]")
			}
			rows[kind] = append(rows[kind], path)
		}
	}
	return rows
}

func TestMachineObservableFieldTableMatchesProjector(t *testing.T) {
	declared := map[api.Kind][]string{}
	for _, path := range NewMachine(nil).ProjectedPaths() {
		declared[path.Kind] = append(declared[path.Kind], path.Path)
	}
	documented := tableRows(t, MachineImplementation)
	if len(documented) == 0 {
		t.Fatalf("the spec table has no row for %s", MachineImplementation)
	}
	for kind, paths := range declared {
		slices.Sort(paths)
		got := slices.Clone(documented[kind])
		slices.Sort(got)
		if !slices.Equal(paths, got) {
			t.Errorf("%s: the table lists %q, ProjectedPaths returns %q", kind, got, paths)
		}
		delete(documented, kind)
	}
	for kind, paths := range documented {
		t.Errorf("the table lists %s fields %q that ProjectedPaths does not return", kind, paths)
	}
}
