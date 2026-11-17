package v1alpha1

import (
	"reflect"
	"sync"
	"testing"
)

func viewMachine(name string, os Value) Object {
	return NewObject(Machine, name, Value{}, scopeMap("os", os, "hardware", scopeMap("providerRef", "lab")))
}

func TestAMachineViewReadsWhatTheTreeHolds(t *testing.T) {
	installed := viewMachine("installed", scopeMap("install", scopeMap("profileRef", "rhel")))
	provided := viewMachine("provided", scopeMap("provided", MapValue()))
	cluster := viewMachine("cluster", scopeMap("clusterInstalled", MapValue()))
	unset := viewMachine("unset", scopeMap("profileRef", "rhel"))
	catalog := NewCatalog([]Object{installed, provided, cluster, unset})
	for _, test := range []struct {
		object                      Object
		declared, provided, install bool
		profile                     string
	}{
		{installed, true, false, true, "rhel"},
		{provided, true, true, false, ""},
		{cluster, true, false, false, ""},
		{unset, false, false, false, ""},
	} {
		view, found := catalog.Machine(test.object.Name())
		if !found || !reflect.DeepEqual(view.Object, test.object) || view.ProviderRef != "lab" {
			t.Fatalf("%s: view %+v, found %t", test.object.Name(), view, found)
		}
		if view.ProvidedDeclared != test.declared || view.Provided != test.provided || view.Installed() != test.install || view.InstallProfileRef != test.profile || view.ClusterInstalled != (test.object.Name() == "cluster") {
			t.Errorf("%s: view %+v, installed %t", test.object.Name(), view, view.Installed())
		}
	}
	if _, found := catalog.Machine("absent"); found {
		t.Error("an absent Machine has a view")
	}
}

func TestTheMachineViewsFollowTheCatalogsOwnOrderAndUniqueness(t *testing.T) {
	first, second := viewMachine("twin", scopeMap("install", scopeMap("profileRef", "a"))), viewMachine("twin", scopeMap("install", scopeMap("profileRef", "b")))
	other := viewMachine("other", Value{})
	catalog := NewCatalog([]Object{first, other, second})
	views := catalog.Machines()
	if len(views) != 3 || views[0].InstallProfileRef != "a" || views[1].Object.Name() != "other" || views[2].InstallProfileRef != "b" {
		t.Fatalf("views = %+v", views)
	}
	if _, found := catalog.Machine("twin"); found {
		t.Error("a duplicated Machine name has a view, where Find finds none")
	}
	if len(Catalog{}.Machines()) != 0 {
		t.Error("the empty catalog has Machine views")
	}
}

func TestACephClusterViewReadsItsNodesServicesAndStretch(t *testing.T) {
	cluster := NewObject(CephCluster, "ceph", Value{}, scopeMap(
		"topology", scopeMap("nodes", ListValue(scopeMap("name", "n1", "machineRef", "m1"), scopeMap("name", "n2", "machineRef", "m2")), "stretch", scopeMap("failureDomain", "datacenter")),
		"services", ListValue(scopeMap("service_type", "osd", "service_id", "all"), scopeMap("service_type", "mon"))))
	catalog := NewCatalog([]Object{cluster})
	view, found := catalog.CephCluster("ceph")
	if !found || !view.Stretched || !reflect.DeepEqual(view.Nodes, []CephNodeView{{"n1", "m1"}, {"n2", "m2"}}) || len(view.Services) != 2 ||
		view.Services[0].Type != "osd" || view.Services[0].Spec.Get("service_id").Text() != "all" || view.Services[1].Type != "mon" {
		t.Fatalf("view = %+v", view)
	}
	view.Nodes[0].MachineRef = "changed"
	view.Services[0].Type = "changed"
	if again := catalog.CephClusters(); again[0].Nodes[0].MachineRef != "m1" || again[0].Services[0].Type != "osd" {
		t.Fatalf("a caller's change reached the catalog's views: %+v", again)
	}
}

func TestTheViewsAreBuiltOnceAndReadConcurrently(t *testing.T) {
	catalog := NewCatalog([]Object{viewMachine("a", scopeMap("provided", MapValue()))}).WithUndecodable(nil)
	var wait sync.WaitGroup
	for range 8 {
		wait.Go(func() {
			if view, found := catalog.Machine("a"); !found || !view.Provided {
				t.Errorf("view = %+v", view)
			}
		})
	}
	wait.Wait()
	built := catalog.views()
	if catalog.views() != built || NewCatalog(nil).views() == built {
		t.Error("the catalog rebuilt its views, or another catalog shares them")
	}
}
