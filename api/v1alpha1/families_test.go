package v1alpha1

import (
	"slices"
	"testing"
)

func layoutPathResolves(shape *Shape, path []string) bool {
	for _, name := range path {
		for shape != nil && shape.Element != nil && shape.Type == Sequence {
			shape = shape.Element
		}
		if shape == nil {
			return false
		}
		field, ok := shape.Field(name)
		if !ok {
			return false
		}
		shape = field.Shape
	}
	return shape != nil
}

// Every cluster kind declares where its nodes are, and every path a layout
// names is a field of that kind's schema, so an accessor never reads a field
// the kind does not have.
func TestEveryClusterKindLaysOutItsFactsInItsSchema(t *testing.T) {
	for _, kind := range ClusterKinds() {
		layout, ok := clusterLayouts[kind]
		if !ok || layout.nodes == nil {
			t.Errorf("%s has no cluster layout naming its nodes", kind)
			continue
		}
		paths := append([][]string{layout.nodes, layout.successorLogin, layout.controllerFIPS, layout.agentInstall}, layout.nodeLogins...)
		for _, path := range paths {
			if path != nil && !layoutPathResolves(Schema(kind), path) {
				t.Errorf("%s layout names %v, which its schema does not declare", kind, path)
			}
		}
		if !layoutPathResolves(Schema(kind), append(slices.Clone(layout.nodes), "machineRef")) {
			t.Errorf("%s node entries declare no machineRef", kind)
		}
	}
	for kind := range clusterLayouts {
		if !IsClusterKind(kind) {
			t.Errorf("%s has a cluster layout but is no family's kind", kind)
		}
	}
}

func TestClusterAccessorsReadEachKindsOwnLayout(t *testing.T) {
	openshift := NewObject(OpenShiftCluster, "ocp", MapValue(), MapValue(
		FieldValue{Name: "nodes", Value: ListValue(MapValue(FieldValue{Name: "machineRef", Value: StringValue("m1")}), MapValue())},
		FieldValue{Name: "install", Value: MapValue(FieldValue{Name: "agent", Value: MapValue()}, FieldValue{Name: "nodeSSH", Value: MapValue(FieldValue{Name: "publicKeyRef", Value: StringValue("k")}, FieldValue{Name: "privateKeyRef", Value: StringValue("k")})})},
		FieldValue{Name: "security", Value: MapValue(FieldValue{Name: "fips", Value: MapValue(FieldValue{Name: "enabled", Value: BoolValue(true)})})},
	))
	ceph := NewObject(CephCluster, "ceph", MapValue(), MapValue(
		FieldValue{Name: "management", Value: StringValue("managed")},
		FieldValue{Name: "topology", Value: MapValue(FieldValue{Name: "nodes", Value: ListValue(MapValue(FieldValue{Name: "machineRef", Value: StringValue("m2")}))})},
		FieldValue{Name: "cephadm", Value: MapValue(FieldValue{Name: "clusterSSH", Value: MapValue(FieldValue{Name: "user", Value: StringValue("ceph")}, FieldValue{Name: "keyRef", Value: StringValue("k")})})},
		FieldValue{Name: "security", Value: MapValue(FieldValue{Name: "fips", Value: MapValue(FieldValue{Name: "enabled", Value: BoolValue(true)})})},
	))
	if got := ClusterNodeMachines(openshift); !slices.Equal(got, []string{"m1", ""}) {
		t.Errorf("OpenShift node machines = %q", got)
	}
	if got := ClusterNodeMachines(ceph); !slices.Equal(got, []string{"m2"}) {
		t.Errorf("Ceph node machines = %q", got)
	}
	if field, ok := ClusterNodeLoginField(openshift, "k"); !ok || field != "spec.install.nodeSSH.publicKeyRef" {
		t.Errorf("OpenShift login field = %q %v", field, ok)
	}
	if field, ok := ClusterNodeLoginField(ceph, "k"); !ok || field != "spec.cephadm.clusterSSH.keyRef" {
		t.Errorf("Ceph login field = %q %v", field, ok)
	}
	if _, ok := ClusterNodeLoginField(ceph, ""); ok {
		t.Error("an empty key matched an absent field")
	}
	if ClusterSuccessorLogin(openshift) || !ClusterSuccessorLogin(ceph) {
		t.Error("only the managed Ceph cluster's non-root keyed login succeeds root")
	}
	if !ClusterRequiresControllerFIPS(openshift) || ClusterRequiresControllerFIPS(ceph) {
		t.Error("only the OpenShift FIPS choice demands a FIPS controller")
	}
	if !ClusterInstallsByAgent(openshift) || ClusterInstallsByAgent(ceph) {
		t.Error("only the OpenShift agent installation is an agent installation")
	}
}

// A diagnostic names the kinds it concerns from the registry, so a second
// family kind joins the phrase without an edit to the message.
func TestKindPhrasesFollowTheRegistry(t *testing.T) {
	if got := KindPhrase(ClusterKinds(), false); got != "OpenShiftCluster or CephCluster" {
		t.Fatalf("singular phrase = %q", got)
	}
	if got := KindPhrase([]Kind{CephCluster, CephCluster, Kind("ExampleCluster")}, true); got != "CephClusters or ExampleClusters" {
		t.Fatalf("plural phrase = %q", got)
	}
	if got := KindPhrase(KindsOf([]Object{NewObject(CephCluster, "a", Value{}, Value{})}), false); got != "CephCluster" {
		t.Fatalf("phrase of objects = %q", got)
	}
	if Article(OpenShiftCluster) != "an" || Article(CephCluster) != "a" || Article("") != "a" {
		t.Fatal("articles do not follow the kind's first letter")
	}
}

func TestOnlyAKindWithASuccessorLoginNamesItsField(t *testing.T) {
	if field, ok := ClusterSuccessorLoginField(CephCluster); !ok || field != "spec.cephadm.clusterSSH" {
		t.Fatalf("CephCluster successor login field = %q, %t", field, ok)
	}
	if _, ok := ClusterSuccessorLoginField(OpenShiftCluster); ok {
		t.Fatal("OpenShiftCluster names a successor login field")
	}
}
