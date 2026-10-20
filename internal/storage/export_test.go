package storage

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func hasIssue(issues []api.Issue, want api.Issue) bool {
	for _, issue := range issues {
		if issue == want {
			return true
		}
	}
	return false
}

func TestStorageExportDeferredToB86(t *testing.T) {
	catalog := fixtureCatalog()
	export := find(t, catalog, api.StorageExport, "export")
	issues := Validate(export, catalog)
	want := api.Issue{
		Code:        "api.invariant",
		Field:       "$.spec",
		Message:     "this storage resource family requires its later qualified service slice",
		Remediation: "remove this StorageExport from the input; B86 delivers it",
	}
	if !hasIssue(issues, want) {
		t.Fatalf("missing managed export deferral %#v: %#v", want, issues)
	}
	for _, issue := range issues {
		if strings.Contains(issue.Remediation, "B489") {
			t.Fatalf("remediation names B489: %#v", issue)
		}
	}
	external := object(api.StorageCluster, "external", m("type", "ceph", "management", "external"))
	ext := object(api.StorageExport, "external-export", m("clusterRef", "external", "externalDetails", m("fromSecretRef", "details")))
	want.Remediation = "remove this StorageExport from the input; parked item B107 delivers it"
	if got := Validate(ext, api.NewCatalog([]api.Object{external, ext})); !hasIssue(got, want) {
		t.Fatalf("missing external export deferral %#v: %#v", want, got)
	}
}
