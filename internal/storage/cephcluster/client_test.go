package cephcluster

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestStorageClientExactlyOneScope(t *testing.T) {
	catalog := replace(fixtureCatalog(), object(api.CephSubvolume, "docs", m("clusterRef", "storage", "filesystemRef", "files")))
	catalog = replace(catalog, object(api.CephFilesystem, "foreign-files", m("clusterRef", "other", "mdsServiceRef", "mds.foreign-files", "metadataPoolRef", "metadata", "dataPoolRefs", api.StringList("data"))))
	catalog = replace(catalog, object(api.CephSubvolume, "foreign-docs", m("clusterRef", "other", "filesystemRef", "foreign-files")))
	scopes := map[string]string{"poolRef": "block", "filesystemRef": "files", "subvolumeRef": "docs"}
	client := func(keys ...string) api.Object {
		pairs := []any{"clusterRef", "storage", "access", "readWrite"}
		for _, key := range keys {
			pairs = append(pairs, key, scopes[key])
		}
		return object(api.CephClient, "writer", m(pairs...))
	}
	const message = "a CephClient grants exactly one scope, so it declares exactly one of spec.poolRef, spec.filesystemRef and spec.subvolumeRef"
	for key := range scopes {
		if issues := Validate(client(key), catalog); len(issues) != 0 {
			t.Fatalf("%s arm: %#v", key, issues)
		}
	}
	twoScopes := api.Issue{Code: "api.invariant", Field: "$.spec", Message: message, Remediation: "keep only one of spec.poolRef, spec.filesystemRef and spec.subvolumeRef; declare another CephClient for another scope"}
	for _, keys := range [][]string{{"poolRef", "filesystemRef"}, {"poolRef", "subvolumeRef"}, {"filesystemRef", "subvolumeRef"}, {"poolRef", "filesystemRef", "subvolumeRef"}} {
		for name, validate := range map[string]func(api.Object, api.Catalog) []api.Issue{"Validate": Validate, "ValidatePartial": ValidatePartial} {
			if issues := validate(client(keys...), catalog); len(issues) != 1 || issues[0] != twoScopes {
				t.Fatalf("%s of %v: %#v", name, keys, issues)
			}
		}
	}
	none := api.Issue{Code: "api.required", Field: "$.spec", Message: message, Remediation: "set spec.poolRef, spec.filesystemRef or spec.subvolumeRef"}
	if issues := Validate(client(), catalog); len(issues) != 1 || issues[0] != none {
		t.Fatalf("no arm: %#v", issues)
	}
	if issues := ValidatePartial(client(), catalog); len(issues) != 0 {
		t.Fatalf("a defaults fragment may leave the scope to the authored input: %#v", issues)
	}
	foreign := client("filesystemRef")
	foreign = foreign.WithSpec(foreign.Spec().With("filesystemRef", api.StringValue("foreign-files")))
	requireIssue(t, Validate(foreign, catalog), "$.spec.filesystemRef")
	foreign = client("subvolumeRef")
	foreign = foreign.WithSpec(foreign.Spec().With("subvolumeRef", api.StringValue("foreign-docs")))
	requireIssue(t, Validate(foreign, catalog), "$.spec.subvolumeRef")
	metadata := client("poolRef")
	metadata = metadata.WithSpec(metadata.Spec().With("poolRef", api.StringValue("metadata")))
	requireIssue(t, Validate(metadata, catalog), "$.spec.poolRef")
}
