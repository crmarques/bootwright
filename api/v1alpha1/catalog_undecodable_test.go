package v1alpha1

import "testing"

func TestCatalogKnowsUndecodableIdentities(t *testing.T) {
	present := NewObject(Secret, "present", MapValue(), MapValue())
	original := NewCatalog([]Object{present})
	if original.Undecodable(ClusterAddon, "broken") || original.Undecodable(Secret, "present") {
		t.Fatal("a new catalog reports an undecodable identity")
	}
	identities := map[string]bool{"ClusterAddon/broken": true}
	marked := original.WithUndecodable(identities)
	identities["Secret/present"] = true
	if original.Undecodable(ClusterAddon, "broken") {
		t.Fatal("WithUndecodable changed the catalog it was called on")
	}
	if !marked.Undecodable(ClusterAddon, "broken") {
		t.Fatal("the recorded identity is not reported")
	}
	for _, other := range []struct {
		kind Kind
		name string
	}{{ClusterAddonProfile, "broken"}, {ClusterAddon, "Broken"}, {ClusterAddon, "broken2"}, {Secret, "present"}} {
		if marked.Undecodable(other.kind, other.name) {
			t.Fatalf("%s/%s is reported undecodable", other.kind, other.name)
		}
	}
	if _, found := marked.Find(ClusterAddon, "broken"); found {
		t.Fatal("an undecodable identity is found")
	}
	if found, ok := marked.Find(Secret, "present"); !ok || found.Name() != "present" {
		t.Fatal("the decoded object is no longer found")
	}
}
