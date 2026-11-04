package ceph

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const projectionSpec = "../../../specs/state-reconciliation.md"

const addedOSD = "7da65e47-8789-4a1c-88e8-3bbd195719a2"

func projectionBlock(t *testing.T, catalog api.Catalog, kind api.Kind, name string) (reconciliation.Block, Request) {
	t.Helper()
	r, err := planned(t, catalog, kind, name)
	if err != nil {
		t.Fatalf("plan %s/%s: %v", kind, name, diagnostics.Of(err))
	}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	id := "ceph-pool-" + name
	if kind == api.CephCluster {
		id = ClusterBlockID(name)
	}
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: id, Kind: string(kind), Object: name, Request: data}, RequestDigest: "digest"}, r
}

func observedEvidence(r Request) Evidence {
	r.Owner = "apply-original"
	r.FSID = FSIDFor(r.Owner, r.Cluster)
	return readyEvidence(r)
}

func projected(t *testing.T, catalog api.Catalog, kind api.Kind, name string, change func(*Evidence)) []lifecycle.Projection {
	t.Helper()
	block, r := projectionBlock(t, catalog, kind, name)
	e := observedEvidence(r)
	change(&e)
	projections, err := New(kind, nil).Project(catalog, block, evidenceBytes(t, e))
	if err != nil {
		t.Fatalf("project = %v", diagnostics.Of(err))
	}
	requireAdmissible(t, New(kind, nil), projections)
	return projections
}

func requireAdmissible(t *testing.T, capability Capability, projections []lifecycle.Projection) {
	t.Helper()
	for _, projection := range projections {
		if projection.Name == "" || !api.ValidLexical("name", projection.Name) || projection.Observed.Type() != api.Mapping || len(projection.Notes) > 8 {
			t.Fatalf("inadmissible projection %+v", projection)
		}
		for _, note := range projection.Notes {
			if len(note) > 200 || strings.ContainsFunc(note, func(r rune) bool { return r < 0x20 }) {
				t.Fatalf("inadmissible note %q", note)
			}
		}
		for _, path := range claimedLeaves(projection.Observed, api.Schema(projection.Kind), "spec") {
			if !slices.Contains(capability.ProjectedPaths(), lifecycle.ProjectedPath{Kind: projection.Kind, Path: path}) {
				t.Errorf("%s/%s claims %s, which ProjectedPaths does not list", projection.Kind, projection.Name, path)
			}
		}
	}
}

func claimedLeaves(value api.Value, shape *api.Shape, prefix string) []string {
	switch {
	case !value.Present():
		return nil
	case value.Type() == api.Mapping && shape != nil && !(shape.Open && len(shape.Fields) == 0):
		var paths []string
		for _, entry := range value.Fields() {
			var child *api.Shape
			if field, found := shape.Field(entry.Name); found {
				child = field.Shape
			}
			paths = append(paths, claimedLeaves(entry.Value, child, prefix+"."+entry.Name)...)
		}
		return paths
	case value.Type() == api.Sequence && shape != nil && shape.NameKey != "":
		var paths []string
		for _, item := range value.Items() {
			paths = append(paths, claimedLeaves(item, shape.Element, prefix+"[]")...)
		}
		return paths
	}
	return []string{prefix}
}

func spec(pairs ...any) api.Value { return requestMap(pairs...) }

func requireProjection(t *testing.T, got lifecycle.Projection, kind api.Kind, name string, observed api.Value, notes ...string) {
	t.Helper()
	if got.Kind != kind || got.Name != name || !got.Observed.Equal(observed) || !slices.Equal(got.Notes, notes) {
		t.Fatalf("projection = %s/%s %s notes %q\nwant %s/%s %s notes %q", got.Kind, got.Name, renderValue(got.Observed), got.Notes, kind, name, renderValue(observed), notes)
	}
}

func renderValue(value api.Value) string {
	data, _ := json.Marshal(plainValue(value))
	return string(data)
}

func plainValue(value api.Value) any {
	switch value.Type() {
	case api.Mapping:
		out := map[string]any{}
		for _, field := range value.Fields() {
			out[field.Name] = plainValue(field.Value)
		}
		return out
	case api.Sequence:
		out := []any{}
		for _, item := range value.Items() {
			out = append(out, plainValue(item))
		}
		return out
	}
	return value.Text()
}

func readPool(name string, size, minSize int, applications ...string) *PoolEvidence {
	return &PoolEvidence{Name: name, Type: "replicated", Size: size, MinSize: minSize, Applications: applications, Autoscale: "on"}
}

func TestPoolProjectionRendersAChangedSize(t *testing.T) {
	catalog := planCatalog(3, 3, withCorePair, replicatedPool("volumes"))
	projections := projected(t, catalog, api.CephPool, "volumes", func(e *Evidence) { e.Pool = readPool("volumes", 3, 2, "rbd") })
	if len(projections) != 1 {
		t.Fatalf("projections %+v", projections)
	}
	requireProjection(t, projections[0], api.CephPool, "volumes", spec("replicated", spec("size", 3, "minSize", 2)))
}

func TestPoolProjectionRendersAnUnauthoredDefaultOnlyWhenItDiffers(t *testing.T) {
	catalog := planCatalog(3, 3, withCorePair, childOf(api.CephPool, "volumes", "role", "rbd"))
	unchanged := projected(t, catalog, api.CephPool, "volumes", func(e *Evidence) { e.Pool = readPool("volumes", 2, 2, "rbd") })
	requireProjection(t, unchanged[0], api.CephPool, "volumes", spec())
	changed := projected(t, catalog, api.CephPool, "volumes", func(e *Evidence) {
		e.Pool = readPool("volumes", 2, 2, "cephfs")
		e.Pool.Autoscale, e.Pool.MaxBytes = "off", 1<<40
	})
	requireProjection(t, changed[0], api.CephPool, "volumes", spec("application", "cephfs", "autoscale", spec("mode", "off"), "quota", spec("maxBytes", api.IntegerValue("1099511627776"))))
}

func TestPoolProjectionNamesWhatItCouldNotRead(t *testing.T) {
	catalog := planCatalog(3, 3, withCorePair, replicatedPool("volumes", "application", "rbd", "quota", spec("maxObjects", 10)))
	projections := projected(t, catalog, api.CephPool, "volumes", func(e *Evidence) {
		e.Pool = &PoolEvidence{Name: "volumes", Type: "replicated", Applications: []string{"rbd", "rgw"}, MaxObjects: 10}
	})
	requireProjection(t, projections[0], api.CephPool, "volumes", spec("quota", spec("maxObjects", 10)),
		"ceph-pool-volumes: pool volumes: its replica count is not read", "ceph-pool-volumes: pool volumes enables applications rbd, rgw",
		"ceph-pool-volumes: pool volumes: its autoscale mode is not read")
}

func TestPoolProjectionWithoutAReadPoolRendersNothing(t *testing.T) {
	catalog := planCatalog(3, 3, withCorePair, replicatedPool("volumes"))
	if projections := projected(t, catalog, api.CephPool, "volumes", func(*Evidence) {}); len(projections) != 0 {
		t.Fatalf("projections %+v", projections)
	}
}

func TestPoolProjectionRefusesEvidenceOfAnotherPoolOrRequest(t *testing.T) {
	catalog := planCatalog(3, 3, withCorePair, replicatedPool("volumes"))
	block, r := projectionBlock(t, catalog, api.CephPool, "volumes")
	for name, change := range map[string]func(*Evidence){
		"other pool": func(e *Evidence) { e.Pool = readPool("other", 2, 2, "rbd") },
		"request":    func(e *Evidence) { e.Request = "other"; e.Pool = readPool("volumes", 2, 2, "rbd") },
		"fsid":       func(e *Evidence) { e.FSID = "other"; e.Pool = readPool("volumes", 2, 2, "rbd") },
		"inventory":  func(e *Evidence) { e.Inventory = &InventoryEvidence{Pools: []PoolEvidence{}, OSDs: []OSDEvidence{}} },
	} {
		t.Run(name, func(t *testing.T) {
			e := observedEvidence(r)
			change(&e)
			if projections, err := New(api.CephPool, nil).Project(catalog, block, evidenceBytes(t, e)); err == nil {
				t.Fatalf("foreign evidence projected %+v", projections)
			}
		})
	}
}

func inventoryCatalog() api.Catalog {
	declared := replicatedPool("volumes", "nativeName", "vol-native")
	gateway := objectGateway("objects")
	return planCatalog(3, 3, withPairAndRGW, declared, replicatedPool("scratch-copy"), gateway, api.NewObject(api.CephPool, "taken", api.Value{}, requestMap("clusterRef", "other", "role", "rbd")))
}

func TestClusterProjectionDiscoversAPoolCreatedByHandBesideDerivedAndSystemPools(t *testing.T) {
	catalog := inventoryCatalog()
	read := func(name string) PoolEvidence { return *readPool(name, 3, 2, "rbd") }
	pools := []PoolEvidence{read(".mgr"), read(".nfs"), read(".rgw.root"), read("Scratch_2"), read("by-hand"), read("objects.rgw.buckets.data"),
		read("objects.rgw.log"), read("objects.rgw.meta"), read("objects.rgw.unrelated"), read("scratch-copy"), read("taken"), read("vol-native")}
	erasure := PoolEvidence{Name: "wide", Type: "erasure", Size: 6, MinSize: 5, Applications: []string{}, MaxObjects: 100}
	pools = append(pools, erasure)
	projections := projected(t, catalog, api.CephCluster, "example", func(e *Evidence) {
		e.Inventory = &InventoryEvidence{Pools: pools, OSDs: []OSDEvidence{}}
	})
	if len(projections) != 5 {
		t.Fatalf("projections %s", projectionNames(projections))
	}
	replicated := func(pairs ...any) api.Value {
		return spec(append(pairs, "replicated", spec("size", 3, "minSize", 2), "application", "rbd", "autoscale", spec("mode", "on"))...)
	}
	role := func(pool string) string { return "ceph-cluster-example: pool " + pool + ": spec.role is not read" }
	requireProjection(t, projections[0], api.CephPool, "scratch-2-"+projections[0].Name[len("scratch-2-"):], replicated("clusterRef", "example", "nativeName", "Scratch_2"),
		role("Scratch_2"), "ceph-cluster-example: pool Scratch_2: its native name is not a DNS label, which spec.nativeName requires")
	requireProjection(t, projections[1], api.CephPool, "by-hand", replicated("clusterRef", "example"), role("by-hand"))
	if !strings.HasPrefix(projections[2].Name, "objects-rgw-unrelated-") {
		t.Fatalf("a hand-made pool in the zone namespace that the gateway does not derive was not discovered: %s", projectionNames(projections))
	}
	requireProjection(t, projections[3], api.CephPool, projections[3].Name, replicated("clusterRef", "example", "nativeName", "taken"), role("taken"))
	if projections[3].Name == "taken" || !strings.HasPrefix(projections[3].Name, "taken-") {
		t.Fatalf("a discovered pool took the name of an applied CephPool of another cluster: %s", projections[3].Name)
	}
	requireProjection(t, projections[4], api.CephPool, "wide", spec("clusterRef", "example", "quota", spec("maxObjects", 100)),
		role("wide"), "ceph-cluster-example: pool wide is erasure-coded; spec.erasure is not read", "ceph-cluster-example: pool wide enables no application")
	again := projected(t, catalog, api.CephCluster, "example", func(e *Evidence) {
		e.Inventory = &InventoryEvidence{Pools: pools, OSDs: []OSDEvidence{}}
	})
	if projectionNames(again) != projectionNames(projections) {
		t.Fatalf("discovered names are not deterministic: %s, %s", projectionNames(again), projectionNames(projections))
	}
}

func projectionNames(projections []lifecycle.Projection) string {
	names := []string{}
	for _, projection := range projections {
		names = append(names, string(projection.Kind)+"/"+projection.Name)
	}
	return strings.Join(names, ", ")
}

func TestClusterProjectionWithoutAnInventoryRendersNothing(t *testing.T) {
	if projections := projected(t, inventoryCatalog(), api.CephCluster, "example", func(*Evidence) {}); len(projections) != 0 {
		t.Fatalf("projections %+v", projections)
	}
}

func TestClusterProjectionAddsOneOSDServiceForTheHostAnOSDWasAddedOn(t *testing.T) {
	catalog := inventoryCatalog()
	block, r := projectionBlock(t, catalog, api.CephCluster, "example")
	host := r.Hosts[1].Name
	e := observedEvidence(r)
	e.Inventory = &InventoryEvidence{Pools: []PoolEvidence{}, Omitted: 2, OSDs: []OSDEvidence{
		{Host: host, ID: 7, Path: "/dev/vdd", UUID: "8da65e47-8789-4a1c-88e8-3bbd195719a3"}, {Host: host, ID: 6, Path: "/dev/vdc", UUID: addedOSD}}}
	projections, err := New(api.CephCluster, nil).Project(catalog, block, evidenceBytes(t, e))
	if err != nil || len(projections) != 1 {
		t.Fatalf("project = %+v %v", projections, diagnostics.Of(err))
	}
	requireAdmissible(t, New(api.CephCluster, nil), projections)
	cluster, _ := catalog.Find(api.CephCluster, "example")
	declared := cluster.Spec().Get("services").Items()
	added := spec("service_type", "osd", "service_id", "observed-"+label(host, 40, "host"), "placement", spec("hosts", api.StringList(host)),
		"spec", spec("data_devices", spec("paths", api.StringList("/dev/vdc", "/dev/vdd"))))
	requireProjection(t, projections[0], api.CephCluster, "example", spec("services", api.ListValue(append(declared, added)...)),
		"ceph-cluster-example: "+host+" runs 2 OSD devices no declared OSD service places", "ceph-cluster-example: 2 more pools are not listed")
}

func TestOnlyTheClusterBlockOfACephClusterProjects(t *testing.T) {
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: SettingsBlockID("example"), Kind: string(api.CephCluster), Object: "example"}}
	if projections, err := New(api.CephCluster, nil).Project(inventoryCatalog(), block, nil); err != nil || projections != nil {
		t.Fatalf("a settings block projected %+v %v", projections, err)
	}
	for _, kind := range []api.Kind{api.CephObjectGateway, api.CephFilesystem, api.CephClient} {
		if New(kind, nil).ProjectedPaths() != nil || New(kind, nil).Discovers() != nil {
			t.Fatalf("%s claims projected fields or discovered kinds", kind)
		}
	}
	if !slices.Equal(New(api.CephCluster, nil).Discovers(), []api.Kind{api.CephPool}) || New(api.CephPool, nil).Discovers() != nil {
		t.Fatal("only the cluster capability discovers pools")
	}
}

func TestDiscoveredPoolNamesAreValidAndBounded(t *testing.T) {
	for _, native := range []string{"by-hand", "UPPER", "a.b.c", "x_" + strings.Repeat("y", 61), "___", "-lead"} {
		name := discoveredPoolName(native, "example", false, map[string]bool{})
		if !api.ValidLexical("name", name) {
			t.Fatalf("%q derives invalid name %q", native, name)
		}
	}
	if discoveredPoolName("by-hand", "example", false, map[string]bool{}) != "by-hand" {
		t.Fatal("a valid native name was not kept")
	}
	if discoveredPoolName("by-hand", "a", false, map[string]bool{"by-hand": true}) == discoveredPoolName("by-hand", "b", false, map[string]bool{"by-hand": true}) {
		t.Fatal("the derived name does not depend on the cluster")
	}
}

func TestCoreObservableFieldTableMatchesProjector(t *testing.T) {
	data, err := os.ReadFile(projectionSpec)
	if err != nil {
		t.Fatal(err)
	}
	_, table, found := strings.Cut(string(data), "### Observable fields by implementation")
	if !found {
		t.Fatal("the spec has no observable-field table")
	}
	documented := map[string][]string{}
	var previous []string
	for _, line := range strings.Split(table, "\n") {
		if strings.HasPrefix(line, "## ") {
			break
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) < 3 || strings.Trim(strings.TrimSpace(cells[0]), "`") != Implementation {
			continue
		}
		row := strings.TrimSpace(cells[1])
		paths := backticked(cells[2], "spec.")
		if strings.Contains(cells[2], "the fields above") {
			paths = append(paths, previous...)
		}
		for i, path := range paths {
			if base, list := strings.CutSuffix(path, "[]"); list {
				if field, found := api.Schema(api.Kind(strings.Trim(strings.Fields(row)[0], "`,"))).Field(strings.TrimPrefix(base, "spec.")); !found || field.Shape.Element == nil || field.Shape.NameKey != "" {
					t.Fatalf("%s is not an unkeyed list, so its items are not claimed field by field", base)
				}
				paths[i] = base
			}
		}
		documented[row] = paths
		previous = paths
	}
	rows := map[string]Capability{"`CephPool`": New(api.CephPool, nil), "`CephPool`, discovered": New(api.CephCluster, nil), "`CephCluster`": New(api.CephCluster, nil)}
	if len(documented) != len(rows) {
		t.Fatalf("the spec table lists rows %q for %s", mapKeysOf(documented), Implementation)
	}
	for row, capability := range rows {
		kind := api.Kind(strings.Trim(strings.Fields(row)[0], "`,"))
		var declared []string
		for _, path := range capability.ProjectedPaths() {
			if path.Kind == kind {
				declared = append(declared, path.Path)
			}
		}
		got := slices.Clone(documented[row])
		slices.Sort(got)
		slices.Sort(declared)
		if !slices.Equal(got, declared) {
			t.Errorf("%s: the table lists %q, ProjectedPaths returns %q", row, got, declared)
		}
	}
}

func backticked(cell, prefix string) []string {
	var out []string
	parts := strings.Split(cell, "`")
	for i := 1; i < len(parts); i += 2 {
		if strings.HasPrefix(parts[i], prefix) {
			out = append(out, parts[i])
		}
	}
	return out
}

func mapKeysOf(values map[string][]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func TestOnlyAnUnrecordedClusterObservationAsksForTheUnrecordedReading(t *testing.T) {
	for name, test := range map[string]struct {
		attempt, resolution int
		want                string
	}{
		"diff --observe": {want: UnrecordedReading},
		"apply attempt":  {attempt: 1},
		"resolution":     {resolution: 1},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &recordingRunner{}
			r := coreRequest()
			e := withMaterial(executionFor(t, r), r)
			e.Attempt, e.Resolution = test.attempt, test.resolution
			_, _ = New(api.CephCluster, runner).Observe(context.Background(), e)
			got, found := runner.request.MaterialValues["reading"]
			if runner.calls != 1 || runner.request.Operation != "observe" || got != test.want || found != (test.want != "") || runner.request.Unrecorded != (test.want != "") {
				t.Fatalf("run %d %s values %v unrecorded %v", runner.calls, runner.request.Operation, runner.request.MaterialValues, runner.request.Unrecorded)
			}
		})
	}
}

func withSecondCluster(catalog api.Catalog, children ...api.Object) api.Catalog {
	cluster, _ := catalog.Find(api.CephCluster, "example")
	return api.NewCatalog(append(append(catalog.Objects(), api.NewObject(api.CephCluster, "second", api.Value{}, cluster.Spec())), children...))
}

func clusterBlock(name string) reconciliation.Block {
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: ClusterBlockID(name), Kind: string(api.CephCluster), Object: name}}
}

func TestDiscoveredPoolsOfSeveralClustersAreNamedPerCluster(t *testing.T) {
	read := func(name string) PoolEvidence { return *readPool(name, 3, 2, "rbd") }
	pools := []PoolEvidence{read("Odd_Name"), read("data")}
	taken := api.NewObject(api.CephPool, "second-data", api.Value{}, requestMap("clusterRef", "other", "role", "rbd"))
	catalog := withSecondCluster(inventoryCatalog(), taken)
	example := discoveredPools(catalog, clusterBlock("example"), pools)
	second := discoveredPools(catalog, clusterBlock("second"), pools)
	requireAdmissible(t, New(api.CephCluster, nil), slices.Concat(example, second))
	if len(example) != 2 || len(second) != 2 {
		t.Fatalf("projections %s; %s", projectionNames(example), projectionNames(second))
	}
	if example[1].Name != "example-data" || !example[1].Observed.Get("nativeName").Present() || example[1].Observed.Get("nativeName").Text() != "data" {
		t.Fatalf("the first cluster's pool data is %s %s", example[1].Name, renderValue(example[1].Observed))
	}
	if second[1].Name == "second-data" || !strings.HasPrefix(second[1].Name, "data-") || second[1].Observed.Get("nativeName").Text() != "data" {
		t.Fatalf("a name the applied revision takes was reused: %s", second[1].Name)
	}
	names := map[string]bool{}
	for _, projection := range slices.Concat(example, second) {
		if names[projection.Name] {
			t.Fatalf("two clusters project one pool name %s: %s; %s", projection.Name, projectionNames(example), projectionNames(second))
		}
		names[projection.Name] = true
	}
	if single := discoveredPools(inventoryCatalog(), clusterBlock("example"), pools); single[1].Name != "data" || single[1].Observed.Get("nativeName").Present() {
		t.Fatalf("one cluster's pool data is %s %s", single[1].Name, renderValue(single[1].Observed))
	}
}

func TestProjectionNotesStayWithinTheirBound(t *testing.T) {
	block := reconciliation.BlockIdentity("ceph-cluster-", strings.Repeat("c", 80))
	native := "_" + strings.Repeat("p", 62)
	applications := []string{strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32), strings.Repeat("d", 32)}
	pool := PoolEvidence{Name: native, Type: "replicated", Applications: applications}
	if !validPoolReading(pool) || len(block) != 63 {
		t.Fatalf("the longest reading is not admitted: %d", len(block))
	}
	discovered := discoveredPool(block, "example", "pool-0123abcd", pool)
	_, own := ownPoolFields(block, api.MapValue(), Pool{Type: "erasure"}, pool)
	requireAdmissible(t, New(api.CephCluster, nil), []lifecycle.Projection{discovered, {Kind: api.CephPool, Name: "volumes", Observed: api.MapValue(), Notes: own}})
	if !slices.Contains(discovered.Notes, block+": pool "+native+" enables 4 applications") {
		t.Fatalf("notes %q", discovered.Notes)
	}
}

func TestClusterProjectionNotesUnplacedOSDsItCannotPlace(t *testing.T) {
	catalog := inventoryCatalog()
	block, r := projectionBlock(t, catalog, api.CephCluster, "example")
	host := r.Hosts[1].Name
	e := observedEvidence(r)
	e.Inventory = &InventoryEvidence{Pools: []PoolEvidence{}, OSDs: []OSDEvidence{
		{Host: host, ID: 6, Path: "/dev/vdc"}, {Host: host, ID: 7}, {ID: 8, UUID: addedOSD}, {ID: 9}}}
	projections, err := New(api.CephCluster, nil).Project(catalog, block, evidenceBytes(t, e))
	if err != nil || len(projections) != 1 {
		t.Fatalf("project = %+v %v", projections, diagnostics.Of(err))
	}
	requireAdmissible(t, New(api.CephCluster, nil), projections)
	cluster, _ := catalog.Find(api.CephCluster, "example")
	id := "observed-" + label(host, 40, "host")
	added := spec("service_type", "osd", "service_id", id, "placement", spec("hosts", api.StringList(host)),
		"spec", spec("data_devices", spec("paths", api.StringList("/dev/vdc"))))
	requireProjection(t, projections[0], api.CephCluster, "example", spec("services", api.ListValue(append(cluster.Spec().Get("services").Items(), added)...)),
		"ceph-cluster-example: "+host+" runs 1 OSD devices no declared OSD service places",
		"ceph-cluster-example: 3 OSDs beyond the declared disks name no declared host and device")
}

func TestUnplacedOSDsNameADeviceOnlyOnADeclaredHost(t *testing.T) {
	catalog := inventoryCatalog()
	block, r := projectionBlock(t, catalog, api.CephCluster, "example")
	for name, osd := range map[string]OSDEvidence{
		"device without host": {ID: 6, Path: "/dev/vdc"},
		"undeclared host":     {Host: "node-9", ID: 6},
		"bad device":          {Host: r.Hosts[1].Name, ID: 6, Path: "/dev/"},
		"bad fsid":            {Host: r.Hosts[1].Name, ID: 6, UUID: "x"},
	} {
		t.Run(name, func(t *testing.T) {
			e := observedEvidence(r)
			e.Inventory = &InventoryEvidence{Pools: []PoolEvidence{}, OSDs: []OSDEvidence{osd}}
			if projections, err := New(api.CephCluster, nil).Project(catalog, block, evidenceBytes(t, e)); err == nil {
				t.Fatalf("an unplaced OSD %+v projected %+v", osd, projections)
			}
		})
	}
}
