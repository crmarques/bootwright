package environment_test

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
)

// replicationFixture adds a third storage cluster s3 and a replication from
// s1's pool to s3's, beside the selection fixture's clusters.
func replicationFixture(env api.Object) []api.Object {
	return selectionFixture(env,
		selectionObject(api.Machine, "s3-n"),
		selectionStorage("s3", "s3-n"),
		selectionObject(api.CephPool, "s3-rbd", "clusterRef", "s3"),
		selectionObject(api.CephReplication, "s1-to-s3", "clusterRef", "s1", "type", "rbd", "poolRef", "s1-rbd", "peerClusterRef", "s3", "peerPoolRef", "s3-rbd"),
	)
}

// TestSelectingEitherEndOfAReplicationRetainsBothClustersWhole holds the
// CephReplication rule: selecting the primary or the peer retains the
// relationship and both clusters with every child, and a cluster kept only
// through an export keeps its node closure alone.
func TestSelectingEitherEndOfAReplicationRetainsBothClustersWhole(t *testing.T) {
	whole := []string{"CephReplication/s1-to-s3", "CephCluster/s1", "CephCluster/s3", "CephPool/s1-other", "CephPool/s3-rbd", "CephNFSExport/s1-nfs", "Machine/s1-n", "Machine/s3-n"}
	for _, row := range []struct {
		name     string
		selected []string
	}{{"the primary", []string{"s1"}}, {"the peer", []string{"s3"}}} {
		t.Run(row.name, func(t *testing.T) {
			selection := environment.Select(api.NewCatalog(replicationFixture(selectionEnvironment("env", "storageClusters", row.selected))), nil, testClusterMembers)
			kept := selectionIdentities(selection.Catalog.Objects())
			for _, identity := range whole {
				if !slices.Contains(kept, identity) {
					t.Errorf("selecting %s dropped %s: %v", row.name, identity, kept)
				}
			}
			if slices.Contains(selection.ExcludedStorageClusters, "s1") || slices.Contains(selection.ExcludedStorageClusters, "s3") {
				t.Errorf("a replication end is reported excluded: %v", selection.ExcludedStorageClusters)
			}
			if slices.Contains(kept, "CephCluster/s2") {
				t.Errorf("the replication retained an unrelated cluster: %v", kept)
			}
		})
	}
	t.Run("an export-only cluster keeps its node closure", func(t *testing.T) {
		env := selectionEnvironment("env", "containerClusters", []string{"c1"}, "storageClusters", []string{"s2"})
		selection := environment.Select(api.NewCatalog(replicationFixture(env)), []environment.Attachment{{ClusterRef: "c1", ConsumerRef: "e1"}}, testClusterMembers)
		kept := selectionIdentities(selection.Catalog.Objects())
		if !slices.Contains(kept, "CephCluster/s1") || !slices.Contains(kept, "Machine/s1-n") {
			t.Fatalf("the attached export's cluster and node are gone: %v", kept)
		}
		omitted := false
		for _, problem := range selection.Problems {
			omitted = omitted || (problem.Issue.Code == "api.invariant" && problem.Issue.Field == "$.spec.storageClusters" && problem.Target == "CephCluster/s1")
		}
		if !omitted {
			t.Errorf("the attachment-only cluster is not refused by the omitted-root rule: %+v", selection.Problems)
		}
		for _, identity := range []string{"CephReplication/s1-to-s3", "CephCluster/s3", "CephPool/s1-other"} {
			if slices.Contains(kept, identity) {
				t.Errorf("export-only retention widened to %s: %v", identity, kept)
			}
		}
	})
}

// TestAnAuthoredClusterListKeepsStandaloneInstalledMachines holds the Machine
// rows: a Bootwright-installed Machine that is a node or host of no declared
// cluster stays a root with its provider closure, a Machine nothing retains
// warns by name, and a node of an excluded cluster is covered by that root's
// own warning.
func TestAnAuthoredClusterListKeepsStandaloneInstalledMachines(t *testing.T) {
	installed := func(name, provider string) api.Object {
		return selectionObject(api.Machine, name, "os", api.MapValue(
			api.FieldValue{Name: "provided", Value: api.BoolValue(false)},
			api.FieldValue{Name: "installProfileRef", Value: api.StringValue("rhel")}),
			"substrate", selectionValue("providerRef", provider))
	}
	provided := selectionObject(api.Machine, "existing", "os", api.MapValue(api.FieldValue{Name: "provided", Value: api.BoolValue(true)}))
	objects := selectionFixture(selectionEnvironment("env", "containerClusters", []string{"c0"}),
		installed("bastion", "prov-bastion"),
		selectionObject(api.InfraProvider, "prov-bastion", "libvirt", selectionValue("machineRef", "bastion-host")),
		selectionObject(api.Machine, "bastion-host"),
		installed("c1-installed", "prov-c1"),
		selectionObject(api.InfraProvider, "prov-c1", "libvirt", selectionValue("machineRef", "c1-host")),
		selectionObject(api.Machine, "c1-host"),
		selectionObject(api.InfraProvider, "unused"),
		provided,
		selectionContainer("c2", "c1-installed"),
	)
	selection := environment.Select(api.NewCatalog(objects), nil, testClusterMembers)
	kept := selectionIdentities(selection.Catalog.Objects())
	for _, identity := range []string{"Machine/bastion", "InfraProvider/prov-bastion", "Machine/bastion-host"} {
		if !slices.Contains(kept, identity) {
			t.Errorf("the standalone installation lost %s: %v", identity, kept)
		}
	}
	for _, identity := range []string{"Machine/c1-installed", "InfraProvider/prov-c1", "Machine/c1-host", "Machine/existing", "InfraProvider/unused"} {
		if slices.Contains(kept, identity) {
			t.Errorf("%s is retained, but no rule reaches it: %v", identity, kept)
		}
	}
	want := sortedSelection([]string{
		"OpenShiftCluster/c1 api.selection $.metadata.name", "OpenShiftCluster/c2 api.selection $.metadata.name",
		"Machine/orphan api.selection $.metadata.name", "Machine/existing api.selection $.metadata.name", "InfraProvider/unused api.selection $.metadata.name",
	})
	if problems := selectionProblems(selection); !slices.Equal(problems, want) {
		t.Errorf("problems = %v, want %v", problems, want)
	}
}

// TestSelectingANestedClusterRetainsItsHostCluster is the multidc shape B732
// lifts: hub-multidc-01's nodes are KubeVirt VMs on a provider whose
// hostClusterRef is ocp-multidc-01, so selecting the hub alone retains the
// host cluster as a root with its own closure (its nodes and bindings).
func TestSelectingANestedClusterRetainsItsHostCluster(t *testing.T) {
	objects := []api.Object{
		selectionEnvironment("multidc", "containerClusters", []string{"hub-multidc-01"}),
		selectionObject(api.Machine, "ctl", "substrate", selectionValue("providerRef", "prov-ctl")),
		selectionObject(api.InfraProvider, "prov-ctl", "libvirt", selectionValue("machineRef", "host-ctl")),
		selectionObject(api.Machine, "host-ctl"),
		selectionObject(api.Machine, "ocp-01-n"),
		selectionContainer("ocp-multidc-01", "ocp-01-n"),
		selectionObject(api.ClusterAddonBinding, "ocp-01-addons", "clusterRef", "ocp-multidc-01", "addonRefs", []string{"openshift-virtualization"}),
		selectionObject(api.ClusterAddon, "openshift-virtualization"),
		selectionObject(api.InfraProvider, "kubevirt-ocp-multidc-01", "kubevirt", selectionValue("hostClusterRef", "ocp-multidc-01")),
		selectionObject(api.Machine, "hub-01-n", "substrate", selectionValue("providerRef", "kubevirt-ocp-multidc-01")),
		selectionContainer("hub-multidc-01", "hub-01-n"),
		selectionObject(api.Machine, "ocp-02-n"),
		selectionContainer("ocp-multidc-02", "ocp-02-n"),
	}
	selection := environment.Select(api.NewCatalog(objects), nil, testClusterMembers)
	kept := selectionIdentities(selection.Catalog.Objects())
	for _, identity := range []string{"OpenShiftCluster/hub-multidc-01", "Machine/hub-01-n", "InfraProvider/kubevirt-ocp-multidc-01", "OpenShiftCluster/ocp-multidc-01", "Machine/ocp-01-n", "ClusterAddonBinding/ocp-01-addons", "ClusterAddon/openshift-virtualization"} {
		if !slices.Contains(kept, identity) {
			t.Errorf("selecting the nested cluster dropped %s: %v", identity, kept)
		}
	}
	if !slices.Equal(selection.ExcludedContainerClusters, []string{"ocp-multidc-02"}) {
		t.Errorf("excluded container clusters = %v, want only ocp-multidc-02", selection.ExcludedContainerClusters)
	}
}

// TestExcludedObjectsNameTheListThatRetainsThem holds the remedy a reference
// to an excluded object carries: an excluded root names its own list, and any
// other excluded object names the authored lists.
func TestExcludedObjectsNameTheListThatRetainsThem(t *testing.T) {
	selection := environment.Select(api.NewCatalog(selectionFixture(selectionEnvironment("env", "containerClusters", []string{"c0"}))), nil, testClusterMembers)
	for identity, want := range map[string]string{
		"OpenShiftCluster/c1": "add c1 to Environment spec.containerClusters",
		"Machine/c1-n":        "select the cluster that retains Machine/c1-n in Environment spec.containerClusters",
	} {
		if got := selection.Excluded[identity]; got != want {
			t.Errorf("remedy for %s = %q, want %q", identity, got, want)
		}
	}
	if _, found := selection.Excluded["OpenShiftCluster/c0"]; found {
		t.Error("a selected root is listed as excluded")
	}
}
