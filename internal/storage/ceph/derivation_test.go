package ceph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func derivedPool(name string) api.Object {
	return api.NewObject(api.CephPool, name, api.Value{}, requestMap("clusterRef", "example", "role", "rbd"))
}

// Two requests derived from one plan's base share none of its mutable
// state: changing every nested value of one leaves the other, and the base a
// third derivation receives, equal to a fresh derivation.
func TestDerivedRequestsCannotAffectEachOther(t *testing.T) {
	catalog := poolRequestCatalog(4, 4)
	cluster, _ := catalog.Find(api.CephCluster, "example")
	want, err := baseRequest(catalog, cluster, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	d := planDerivation(lifecycle.PlanInput{Context: lifecycle.ContextIdentity{Name: "synthetic"}, Derived: lifecycle.NewDerivations()})
	first, err := poolRequest(catalog, derivedPool("volumes"), d)
	if err != nil {
		t.Fatal(err)
	}
	second, err := poolRequest(catalog, derivedPool("images"), d)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Hosts) == 0 || len(first.Hosts[0].Devices) == 0 || len(first.Services) == 0 || len(first.Delegated) == 0 || len(first.Delegated[0].Hosts) == 0 {
		t.Fatalf("the fixture's base holds no nested value to change: %+v", first)
	}
	first.Hosts[0].Name = "changed"
	first.Hosts[0].Devices[0].Path = "/dev/changed"
	first.PublicNetworks[0] = "198.51.100.0/24"
	first.Delegated[0].Hosts[0] = "changed"
	first.Hosts[0].Labels[0] = "changed"
	first.Services[0][0] = ' '
	first.Pool.Name = "changed"
	again, err := d.base(catalog, cluster)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, want) {
		t.Fatal("a change to one derived request reached the plan's base")
	}
	second.Action, second.Pool = want.Action, want.Pool
	if !reflect.DeepEqual(second, want) {
		t.Fatal("a change to one derived request reached another")
	}
}

// One plan derives a cluster's base once: a later derivation in the same plan
// receives it even from a catalog that would derive another, while a
// derivation outside a plan derives afresh.
func TestOnePlanDerivesEachBaseOnce(t *testing.T) {
	four, five := poolRequestCatalog(4, 4), poolRequestCatalog(5, 5)
	cluster, _ := four.Find(api.CephCluster, "example")
	larger, _ := five.Find(api.CephCluster, "example")
	d := planDerivation(lifecycle.PlanInput{Context: lifecycle.ContextIdentity{Name: "synthetic"}, Derived: lifecycle.NewDerivations()})
	if r, err := d.base(four, cluster); err != nil || len(r.Hosts) != 4 {
		t.Fatalf("first derivation: %d hosts, %v", len(r.Hosts), err)
	}
	if r, err := d.base(five, larger); err != nil || len(r.Hosts) != 4 {
		t.Fatalf("the plan derived its base again: %d hosts, %v", len(r.Hosts), err)
	}
	if r, err := contextDerivation("synthetic").base(five, larger); err != nil || len(r.Hosts) != 5 {
		t.Fatalf("a derivation outside a plan: %d hosts, %v", len(r.Hosts), err)
	}
}

func sharedReferences(t *testing.T, a, b reflect.Value, path string) {
	t.Helper()
	switch a.Kind() {
	case reflect.Pointer, reflect.Map:
		if !a.IsNil() && a.UnsafePointer() == b.UnsafePointer() {
			t.Fatalf("%s is shared", path)
		}
	case reflect.Slice:
		if a.Cap() > 0 && a.UnsafePointer() == b.UnsafePointer() {
			t.Fatalf("%s is shared", path)
		}
	}
	switch a.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !a.IsNil() {
			sharedReferences(t, a.Elem(), b.Elem(), path)
		}
	case reflect.Slice:
		for i := range a.Len() {
			sharedReferences(t, a.Index(i), b.Index(i), path+"[]")
		}
	case reflect.Map:
		for entry := a.MapRange(); entry.Next(); {
			sharedReferences(t, entry.Value(), b.MapIndex(entry.Key()), path+"{}")
		}
	case reflect.Struct:
		for i := range a.NumField() {
			sharedReferences(t, a.Field(i), b.Field(i), path+"."+a.Type().Field(i).Name)
		}
	}
}

// Every core request golden clones into an equal value that shares no slice, map
// or pointer with its source.
func TestACloneSharesNothingWithItsRequest(t *testing.T) {
	paths, err := filepath.Glob("testdata/request-*.golden")
	if err != nil || len(paths) < 20 {
		t.Fatalf("request goldens: %d %v", len(paths), err)
	}
	cloned := 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var version struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(data, &version); err != nil || version.Version != Version {
			continue
		}
		var r Request
		if err := json.Unmarshal(data, &r); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if len(r.Hosts) != 0 {
			r.Hosts[0].RootWWN = "naa.600508b1001c0000000000000000000a"
		}
		clone := cloneRequest(r)
		if !reflect.DeepEqual(clone, r) {
			t.Fatalf("%s: the clone differs", path)
		}
		sharedReferences(t, reflect.ValueOf(clone), reflect.ValueOf(r), filepath.Base(path))
		cloned++
	}
	if cloned < 20 {
		t.Fatalf("only %d core request goldens were cloned", cloned)
	}
}
