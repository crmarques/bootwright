package cephcluster

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func subvolumeCatalog(t *testing.T) api.Catalog {
	t.Helper()
	fs, catalog := boundFilesystem(t)
	catalog = replace(catalog, fs.WithSpec(fs.Spec().With("subvolumeGroups", api.ListValue(m("name", "csi")))))
	catalog = replace(catalog, cephfsDataPool("unattached"))
	catalog = replace(catalog, object(api.CephPool, "remote-data", m("clusterRef", "other", "role", "cephfsData")))
	return replace(catalog, object(api.CephFilesystem, "remote", m("clusterRef", "other", "mdsServiceRef", "mds.remote", "metadataPoolRef", "metadata", "dataPoolRefs", api.StringList("remote-data"))))
}

func subvolumeObject(name string, pairs ...any) api.Object {
	return object(api.CephSubvolume, name, m(append([]any{"clusterRef", "storage", "filesystemRef", "files"}, pairs...)...))
}

func TestSubvolumeGroupMustBeDeclared(t *testing.T) {
	catalog := subvolumeCatalog(t)
	noIssues(t, admitted(catalog, subvolumeObject("docs", "group", "csi")))
	noIssues(t, admitted(catalog, subvolumeObject("docs")))
	onlyIssue(t, admitted(catalog, subvolumeObject("docs", "group", "apps")), "$.spec.group", "CephFilesystem files declares no subvolume group apps", "add apps to spec.subvolumeGroups of CephFilesystem files, or omit spec.group to use the native default group")
}

func TestSubvolumeLayoutMustBeAttached(t *testing.T) {
	catalog := subvolumeCatalog(t)
	noIssues(t, admitted(catalog, subvolumeObject("docs", "poolLayoutRef", "data")))
	onlyIssue(t, admitted(catalog, subvolumeObject("docs", "poolLayoutRef", "unattached")), "$.spec.poolLayoutRef", "CephPool unattached is not a data pool of CephFilesystem files; a subvolume layout must name an attached data pool", "name one of CephFilesystem files's spec.dataPoolRefs in spec.poolLayoutRef, or omit it to inherit the group's layout")
	role := issuesAt(admitted(catalog, subvolumeObject("docs", "poolLayoutRef", "block")), "$.spec.poolLayoutRef")
	if len(role) == 0 || role[0].Message != "referenced pool does not have the consumer's required role" {
		t.Fatalf("rbd layout pool: %+v", role)
	}
	onlyIssue(t, admitted(catalog, subvolumeObject("docs", "poolLayoutRef", "remote-data")), "$.spec.poolLayoutRef", "referenced storage child belongs to a different cluster", "select a referenced resource whose spec.clusterRef matches this resource")
}

func TestSubvolumeNameHasNoBlockDerivedLimit(t *testing.T) {
	noIssues(t, admitted(subvolumeCatalog(t), subvolumeObject(strings.Repeat("s", 63))))
}

func TestSubvolumeFilesystemOfAnotherClusterRefuses(t *testing.T) {
	catalog := subvolumeCatalog(t)
	candidate := object(api.CephSubvolume, "docs", m("clusterRef", "storage", "filesystemRef", "remote", "group", "csi"))
	onlyIssue(t, admitted(catalog, candidate), "$.spec.filesystemRef", "referenced storage child belongs to a different cluster", "select a referenced resource whose spec.clusterRef matches this resource")
}

func TestSubvolumeModeIsLexicallyChecked(t *testing.T) {
	catalog := subvolumeCatalog(t)
	noIssues(t, admitted(catalog, subvolumeObject("docs", "mode", "0750")))
	if found := issuesAt(admitted(catalog, subvolumeObject("docs", "mode", "0x750")), "$.spec.mode"); len(found) != 1 {
		t.Fatalf("mode 0x750: %+v", found)
	}
	if found := issuesAt(ValidatePartial(object(api.CephSubvolume, "docs", m("mode", "888")), api.Catalog{}), "$.spec.mode"); len(found) != 1 {
		t.Fatalf("partial mode 888: %+v", found)
	}
}
