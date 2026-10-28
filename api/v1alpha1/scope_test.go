package v1alpha1

import (
	"reflect"
	"testing"
)

func scopeMap(pairs ...any) Value {
	fields := []FieldValue{}
	for index := 0; index < len(pairs); index += 2 {
		value, ok := pairs[index+1].(Value)
		if !ok {
			value = StringValue(pairs[index+1].(string))
		}
		fields = append(fields, FieldValue{Name: pairs[index].(string), Value: value})
	}
	return MapValue(fields...)
}

func TestAScopeResolvesWhereItsReferenceLooks(t *testing.T) {
	addresses := func(names ...string) Value {
		items := []Value{}
		for _, name := range names {
			items = append(items, scopeMap("name", name))
		}
		return scopeMap("addresses", ListValue(items...))
	}
	machineA := NewObject(Machine, "a", Value{}, scopeMap("network", addresses("lan", "wan")))
	machineB := NewObject(Machine, "b", Value{}, scopeMap("network", addresses("lan")))
	cluster := NewObject(CephCluster, "ceph", Value{}, scopeMap(
		"topology", scopeMap("nodes", ListValue(scopeMap("name", "n1", "machineRef", "a"), scopeMap("name", "n2", "machineRef", "b"), scopeMap("name", "n3", "machineRef", "gone"))),
		"bootstrap", scopeMap("nodeRef", "n2"),
		"services", ListValue(scopeMap("service_type", "mon"), scopeMap("service_type", "mds", "service_id", "files"))))
	provider := NewObject(InfraProvider, "lab", Value{}, scopeMap("libvirt", scopeMap("machineProfiles", ListValue(scopeMap("name", "small")))))
	catalog := NewCatalog([]Object{machineA, machineB, cluster, provider})
	type owner struct {
		identity string
		names    []string
	}
	for _, row := range []struct {
		name   string
		scope  Scope
		object Object
		record Value
		want   []owner
	}{
		{"the referring object", Scope{Entries: []string{"network", "addresses"}}, machineA, Value{}, []owner{{"Machine/a", []string{"lan", "wan"}}}},
		{"an owner the spec names", Scope{Owner: []string{"machineRef"}, Kind: Machine, Entries: []string{"network", "addresses"}},
			NewObject(Proxy, "edge", Value{}, scopeMap("machineRef", "b")), Value{}, []owner{{"Machine/b", []string{"lan"}}}},
		{"an owner the record names", Scope{Sibling: "proxyRef", Kind: Machine, Entries: []string{"network", "addresses"}}, machineA, scopeMap("proxyRef", "a"), []owner{{"Machine/a", []string{"lan", "wan"}}}},
		{"an owner that does not resolve", Scope{Sibling: "proxyRef", Kind: Machine, Entries: []string{"network", "addresses"}}, machineA, scopeMap("proxyRef", "gone"), nil},
		{"every owner a list names", Scope{Each: []string{"topology", "nodes"}, Owner: []string{"machineRef"}, Kind: Machine, Entries: []string{"network", "addresses"}}, cluster, Value{},
			[]owner{{"Machine/a", []string{"lan", "wan"}}, {"Machine/b", []string{"lan"}}}},
		{"the owner a nested reference selects", Scope{Each: []string{"topology", "nodes"}, Select: []string{"bootstrap", "nodeRef"}, Owner: []string{"machineRef"}, Kind: Machine, Entries: []string{"network", "addresses"}}, cluster, Value{},
			[]owner{{"Machine/b", []string{"lan"}}}},
		{"native service names", Scope{Entries: []string{"services"}, Key: []string{"service_type", "service_id"}}, cluster, Value{}, []owner{{"CephCluster/ceph", []string{"mon", "mds.files"}}}},
		{"an owner of the addressed shape", Scope{Owner: []string{"substrate", "providerRef"}, Kind: InfraProvider, Within: []string{"libvirt"}, Entries: []string{"libvirt", "machineProfiles"}},
			NewObject(Machine, "vm", Value{}, scopeMap("substrate", scopeMap("providerRef", "lab"))), Value{}, []owner{{"InfraProvider/lab", []string{"small"}}}},
		{"an owner of another shape", Scope{Owner: []string{"substrate", "providerRef"}, Kind: InfraProvider, Within: []string{"vsphere"}, Entries: []string{"vsphere", "machineProfiles"}},
			NewObject(Machine, "vm", Value{}, scopeMap("substrate", scopeMap("providerRef", "lab"))), Value{}, nil},
	} {
		t.Run(row.name, func(t *testing.T) {
			owners, resolved := row.scope.Owners(row.object, row.record, catalog)
			var got []owner
			for _, found := range owners {
				got = append(got, owner{found.Object.Identity(), found.Names})
			}
			if !reflect.DeepEqual(got, row.want) || resolved != (row.want != nil) {
				t.Fatalf("owners = %v (%t), want %v", got, resolved, row.want)
			}
		})
	}
}

func TestEveryNestedReferenceNamesWhereItResolves(t *testing.T) {
	for _, kind := range Kinds() {
		for _, path := range unscopedReferences(string(kind)+".spec", Schema(kind), 0) {
			t.Errorf("%s is a nested, endpoint or native-service reference with no Scope, so the shared unresolved-reference diagnostic never resolves it", path)
		}
	}
	if got := unscopedReferences("probe", record(field("nodeRef", &Shape{Type: String, Nested: NestedReference})), 0); len(got) != 1 {
		t.Fatalf("an unscoped nested reference is reported as %v", got)
	}
}

func unscopedReferences(path string, shape *Shape, depth int) []string {
	if shape == nil || depth > 16 {
		return nil
	}
	var found []string
	if shape.Nested != "" && shape.Nested != ExternalReference && shape.Scope == nil {
		found = append(found, path)
	}
	for _, f := range shape.Fields {
		found = append(found, unscopedReferences(path+"."+f.Name, f.Shape, depth+1)...)
	}
	found = append(found, unscopedReferences(path+"[]", shape.Element, depth+1)...)
	for _, alternative := range shape.Alternatives {
		found = append(found, unscopedReferences(path, alternative, depth+1)...)
	}
	return found
}
