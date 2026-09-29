package environment_test

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
)

type selectionRow struct {
	name                               string
	objects                            []api.Object
	attachments                        []environment.Attachment
	asGiven                            bool
	dropped                            []string
	excludedContainer, excludedStorage []string
	problems                           []string
}

func TestSelectionRetainsExactlyWhatTheClosureTableNames(t *testing.T) {
	for _, row := range selectionRows() {
		t.Run(row.name, func(t *testing.T) {
			selection := environment.Select(api.NewCatalog(row.objects), row.attachments)
			kept := selectionIdentities(selection.Catalog.Objects())
			if dropped := selectionDropped(t, selectionIdentities(row.objects), kept); !slices.Equal(dropped, sortedSelection(row.dropped)) {
				t.Errorf("dropped = %v, want %v", dropped, sortedSelection(row.dropped))
			}
			if row.asGiven && !slices.Equal(kept, selectionIdentities(row.objects)) {
				t.Errorf("catalog = %v, want the input as given", kept)
			}
			if !slices.Equal(selection.ExcludedContainerClusters, selectionNames(row.excludedContainer)) {
				t.Errorf("excluded container clusters = %#v, want %v", selection.ExcludedContainerClusters, row.excludedContainer)
			}
			if !slices.Equal(selection.ExcludedStorageClusters, selectionNames(row.excludedStorage)) {
				t.Errorf("excluded storage clusters = %#v, want %v", selection.ExcludedStorageClusters, row.excludedStorage)
			}
			if problems := selectionProblems(selection); !slices.Equal(problems, sortedSelection(row.problems)) {
				t.Errorf("problems = %v, want %v", problems, sortedSelection(row.problems))
			}
		})
	}
}

func TestSelectionDoesNotDependOnInputOrder(t *testing.T) {
	for _, row := range selectionRows() {
		t.Run(row.name, func(t *testing.T) {
			reversed := slices.Clone(row.objects)
			slices.Reverse(reversed)
			input := api.NewCatalog(reversed)
			backward := environment.Select(input, row.attachments)
			if !slices.Equal(selectionIdentities(input.Objects()), selectionIdentities(reversed)) {
				t.Fatalf("selection changed its input catalog to %v", selectionIdentities(input.Objects()))
			}
			forward := environment.Select(api.NewCatalog(row.objects), row.attachments)
			got := selectionIdentities(backward.Catalog.Objects())
			if row.asGiven {
				if !slices.Equal(got, selectionIdentities(reversed)) {
					t.Fatalf("catalog = %v, want the reversed input as given", got)
				}
				return
			}
			if !slices.Equal(got, selectionIdentities(forward.Catalog.Objects())) {
				t.Errorf("reversed input kept %v, forward input %v", got, selectionIdentities(forward.Catalog.Objects()))
			}
			if !slices.IsSortedFunc(backward.Catalog.Objects(), canonicalSelectionOrder) {
				t.Errorf("catalog %v is not in canonical order", got)
			}
			if !slices.Equal(backward.ExcludedContainerClusters, forward.ExcludedContainerClusters) || !slices.Equal(backward.ExcludedStorageClusters, forward.ExcludedStorageClusters) {
				t.Errorf("reversed input excluded %v %v, forward input %v %v", backward.ExcludedContainerClusters, backward.ExcludedStorageClusters, forward.ExcludedContainerClusters, forward.ExcludedStorageClusters)
			}
			if !slices.Equal(selectionProblems(backward), selectionProblems(forward)) {
				t.Errorf("reversed input reported %v, forward input %v", selectionProblems(backward), selectionProblems(forward))
			}
		})
	}
}

func selectionRows() []selectionRow {
	c1e1 := environment.Attachment{ClusterRef: "c1", ExportRef: "e1"}
	c1e2 := environment.Attachment{ClusterRef: "c1", ExportRef: "e2"}
	c0e2 := environment.Attachment{ClusterRef: "c0", ExportRef: "e2"}
	withoutContainerOne := []string{"Machine/orphan", "ContainerCluster/c1", "Machine/c1-n", "ClusterAddonBinding/b1", "ClusterAddonProfile/p1", "ClusterAddonProfile/p2", "ClusterAddon/a1", "ClusterAddon/a2", "ClusterAddon/a3"}
	withoutStorageOne := []string{"StorageCluster/s1", "Machine/s1-n", "StoragePlacementPolicy/s1-policy", "StoragePool/s1-rbd", "StoragePool/s1-meta", "StoragePool/s1-other", "StorageFilesystem/s1-fs", "StorageNFSExport/s1-nfs", "StorageExport/e1"}
	withoutContainers := append([]string{"ContainerCluster/c0", "Machine/c0-n"}, withoutContainerOne...)
	excludedContainerOne := "ContainerCluster/c1 api.selection $.metadata.name"
	return []selectionRow{
		{name: "1 no Environment", objects: selectionFixture(), asGiven: true},
		{name: "2 two Environments", objects: selectionFixture(selectionEnvironment("env", "containerClusters", []string{"c0"}), selectionEnvironment("other")), asGiven: true},
		{name: "3 both lists omitted", objects: selectionFixture(selectionEnvironment("env")), asGiven: true},
		{
			name: "4 one container cluster", objects: selectionFixture(selectionEnvironment("env", "containerClusters", []string{"c0"})),
			dropped: withoutContainerOne, excludedContainer: []string{"c1"}, problems: []string{excludedContainerOne},
		},
		{
			name: "5 one storage cluster", objects: selectionFixture(selectionEnvironment("env", "storageClusters", []string{"s2"})),
			dropped: append([]string{"Machine/orphan", "ClusterAddon/a3"}, withoutStorageOne...), excludedStorage: []string{"s1"},
			problems: []string{"StorageCluster/s1 api.selection $.metadata.name"},
		},
		{
			name: "6 empty container list", objects: selectionFixture(selectionEnvironment("env", "containerClusters", []string{})),
			dropped: withoutContainers, excludedContainer: []string{"c0", "c1"},
			problems: []string{"Environment/env api.value $.spec.containerClusters", "ContainerCluster/c0 api.selection $.metadata.name", excludedContainerOne},
		},
		{
			name: "7 unresolved container name", objects: selectionFixture(selectionEnvironment("env", "containerClusters", []string{"missing"})),
			dropped: withoutContainers, excludedContainer: []string{"c0", "c1"},
			problems: []string{"Environment/env api.reference $.spec.containerClusters", "ContainerCluster/c0 api.selection $.metadata.name", excludedContainerOne},
		},
		{
			name:    "8 duplicate container root",
			objects: selectionFixture(selectionEnvironment("env", "containerClusters", []string{"c0"}), selectionContainer("c0", "c0-n")),
			dropped: withoutContainerOne, excludedContainer: []string{"c1"},
			problems: []string{"Environment/env api.reference $.spec.containerClusters", excludedContainerOne},
		},
		{
			name:              "9 an attached export keeps its storage cluster's nodes but not its other children",
			objects:           selectionFixture(selectionEnvironment("env", "containerClusters", []string{"c1"}, "storageClusters", []string{"s2"})),
			attachments:       []environment.Attachment{c1e1},
			dropped:           []string{"Machine/orphan", "ContainerCluster/c0", "Machine/c0-n", "StoragePool/s1-other", "ClusterAddon/a3"},
			excludedContainer: []string{"c0"}, problems: []string{"ContainerCluster/c0 api.selection $.metadata.name"},
		},
		{
			name:            "10 a full storage root keeps its attached container root",
			objects:         selectionFixture(selectionEnvironment("env", "containerClusters", []string{"c0"}, "storageClusters", []string{"s1"})),
			attachments:     []environment.Attachment{c1e1},
			dropped:         []string{"Machine/orphan", "StorageCluster/s2", "Machine/s2-n", "StoragePool/s2-rbd", "StorageExport/e2", "ClusterAddon/a3"},
			excludedStorage: []string{"s2"}, problems: []string{"StorageCluster/s2 api.selection $.metadata.name"},
		},
		{
			name:        "11 an attached container root keeps its other attached exports",
			objects:     selectionFixture(selectionEnvironment("env", "containerClusters", []string{"c0"}, "storageClusters", []string{"s1"})),
			attachments: []environment.Attachment{c1e1, c1e2},
			dropped:     []string{"Machine/orphan", "ClusterAddon/a3"},
		},
		{
			name:              "12 an attachment to an excluded root keeps nothing",
			objects:           selectionFixture(selectionEnvironment("env", "containerClusters", []string{"c0"}, "storageClusters", []string{"s2"})),
			attachments:       []environment.Attachment{c1e1},
			dropped:           append(slices.Clone(withoutContainerOne), withoutStorageOne...),
			excludedContainer: []string{"c1"}, excludedStorage: []string{"s1"},
			problems: []string{excludedContainerOne, "StorageCluster/s1 api.selection $.metadata.name"},
		},
		{
			name:        "13 a storage root kept through an export keeps no other attached root",
			objects:     selectionFixture(selectionEnvironment("env", "containerClusters", []string{"c0"}, "storageClusters", []string{"s1"})),
			attachments: []environment.Attachment{c0e2, c1e2},
			dropped:     withoutContainerOne, excludedContainer: []string{"c1"}, problems: []string{excludedContainerOne},
		},
	}
}

func selectionFixture(extra ...api.Object) []api.Object {
	objects := []api.Object{
		selectionObject(api.Machine, "ctl", "substrate", selectionValue("providerRef", "prov-ctl")),
		selectionObject(api.InfraProvider, "prov-ctl", "libvirt", selectionValue("machineRef", "host-ctl")),
		selectionObject(api.Machine, "host-ctl"),
		selectionObject(api.Machine, "orphan"),
		selectionObject(api.Machine, "svc-host"),
		selectionObject(api.Machine, "c0-n"),
		selectionObject(api.Machine, "c1-n"),
		selectionObject(api.Machine, "s1-n"),
		selectionObject(api.Machine, "s2-n"),
		selectionObject(api.Proxy, "managed-proxy", "management", "managed", "machineRef", "svc-host"),
		selectionObject(api.Proxy, "external-proxy", "management", "external", "machineRef", "orphan"),
		selectionObject(api.Secret, "secret"),
		selectionContainer("c0", "c0-n"),
		selectionContainer("c1", "c1-n"),
		selectionStorage("s1", "s1-n"),
		selectionStorage("s2", "s2-n"),
		selectionObject(api.StoragePlacementPolicy, "s1-policy", "clusterRef", "s1"),
		selectionObject(api.StoragePool, "s1-rbd", "clusterRef", "s1", "placementPolicyRef", "s1-policy"),
		selectionObject(api.StoragePool, "s1-meta", "clusterRef", "s1"),
		selectionObject(api.StoragePool, "s1-other", "clusterRef", "s1"),
		selectionObject(api.StoragePool, "s2-rbd", "clusterRef", "s2"),
		selectionObject(api.StorageFilesystem, "s1-fs", "clusterRef", "s1", "metadataPoolRef", "s1-meta", "dataPoolRefs", []string{"s1-meta"}),
		selectionObject(api.StorageNFSExport, "s1-nfs", "clusterRef", "s1"),
		selectionObject(api.StorageExport, "e1", "clusterRef", "s1", "dataFoundation", selectionValue("rbdPoolRef", "s1-rbd", "filesystemRef", "s1-fs")),
		selectionObject(api.StorageExport, "e2", "clusterRef", "s2", "dataFoundation", selectionValue("rbdPoolRef", "s2-rbd")),
		selectionObject(api.ClusterAddonBinding, "b1", "clusterRef", "c1", "profileRefs", []string{"p1"}),
		selectionObject(api.ClusterAddonProfile, "p1", "addonRefs", []string{"a1"}, "profileRefs", []string{"p2"}),
		selectionObject(api.ClusterAddonProfile, "p2", "addonRefs", []string{"a2"}),
		selectionObject(api.ClusterAddon, "a1"),
		selectionObject(api.ClusterAddon, "a2"),
		selectionObject(api.ClusterAddon, "a3"),
	}
	return append(extra, objects...)
}

func selectionEnvironment(name string, fields ...any) api.Object {
	return selectionObject(api.Environment, name, append([]any{"controller", selectionValue("machineRef", "ctl")}, fields...)...)
}

func selectionContainer(name, node string) api.Object {
	return selectionObject(api.ContainerCluster, name, "nodes", api.ListValue(selectionValue("machineRef", node)))
}

func selectionStorage(name, node string) api.Object {
	nodes := api.ListValue(selectionValue("machineRef", node))
	return selectionObject(api.StorageCluster, name, "ceph", selectionValue("topology", selectionValue("nodes", nodes)))
}

func selectionObject(kind api.Kind, name string, fields ...any) api.Object {
	return api.NewObject(kind, name, api.Value{}, selectionValue(fields...))
}

func selectionValue(fields ...any) api.Value {
	values := make([]api.FieldValue, 0, len(fields)/2)
	for index := 0; index < len(fields); index += 2 {
		var value api.Value
		switch typed := fields[index+1].(type) {
		case string:
			value = api.StringValue(typed)
		case []string:
			items := []api.Value{}
			for _, item := range typed {
				items = append(items, api.StringValue(item))
			}
			value = api.ListValue(items...)
		case api.Value:
			value = typed
		}
		values = append(values, api.FieldValue{Name: fields[index].(string), Value: value})
	}
	return api.MapValue(values...)
}

func selectionIdentities(objects []api.Object) []string {
	identities := make([]string, 0, len(objects))
	for _, object := range objects {
		identities = append(identities, object.Identity())
	}
	return identities
}

func selectionDropped(t *testing.T, input, kept []string) []string {
	t.Helper()
	remaining := slices.Clone(input)
	for _, identity := range kept {
		index := slices.Index(remaining, identity)
		if index < 0 {
			t.Fatalf("selection returned %s, which its input does not hold", identity)
		}
		remaining = slices.Delete(remaining, index, index+1)
	}
	return sortedSelection(remaining)
}

func selectionProblems(selection environment.Selection) []string {
	problems := []string{}
	for _, problem := range selection.Problems {
		problems = append(problems, problem.Object.Identity()+" "+problem.Issue.Code+" "+problem.Issue.Field)
	}
	return sortedSelection(problems)
}

func sortedSelection(values []string) []string {
	sorted := append([]string{}, values...)
	slices.Sort(sorted)
	return sorted
}

func selectionNames(names []string) []string {
	return append([]string{}, names...)
}

func canonicalSelectionOrder(a, b api.Object) int {
	if rank := api.KindIndex(a.Kind()) - api.KindIndex(b.Kind()); rank != 0 {
		return rank
	}
	return strings.Compare(a.Name(), b.Name())
}
