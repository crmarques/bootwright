package storage

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
	catalog = replace(catalog, object(api.StoragePool, "remote-data", m("clusterRef", "other", "role", "cephfs-data")))
	return replace(catalog, object(api.StorageFilesystem, "remote", m("clusterRef", "other", "mdsServiceRef", "mds.remote", "metadataPoolRef", "metadata", "dataPoolRefs", api.StringList("remote-data"))))
}

func subvolumeObject(name string, pairs ...any) api.Object {
	return object(api.StorageSubvolume, name, m(append([]any{"clusterRef", "storage", "filesystemRef", "files"}, pairs...)...))
}

func TestSubvolumeGroupMustBeDeclared(t *testing.T) {
	catalog := subvolumeCatalog(t)
	noIssues(t, admitted(catalog, subvolumeObject("docs", "group", "csi")))
	noIssues(t, admitted(catalog, subvolumeObject("docs")))
	onlyIssue(t, admitted(catalog, subvolumeObject("docs", "group", "apps")), "$.spec.group", "StorageFilesystem files declares no subvolume group apps", "add apps to spec.subvolumeGroups of StorageFilesystem files, or omit spec.group to use the native default group")
}

func TestSubvolumeLayoutMustBeAttached(t *testing.T) {
	catalog := subvolumeCatalog(t)
	noIssues(t, admitted(catalog, subvolumeObject("docs", "poolLayoutRef", "data")))
	onlyIssue(t, admitted(catalog, subvolumeObject("docs", "poolLayoutRef", "unattached")), "$.spec.poolLayoutRef", "StoragePool unattached is not a data pool of StorageFilesystem files; a subvolume layout must name an attached data pool", "name one of StorageFilesystem files's spec.dataPoolRefs in spec.poolLayoutRef, or omit it to inherit the group's layout")
	role := issuesAt(admitted(catalog, subvolumeObject("docs", "poolLayoutRef", "block")), "$.spec.poolLayoutRef")
	if len(role) == 0 || role[0].Message != "referenced pool does not have the consumer's required role" {
		t.Fatalf("rbd layout pool: %+v", role)
	}
	onlyIssue(t, admitted(catalog, subvolumeObject("docs", "poolLayoutRef", "remote-data")), "$.spec.poolLayoutRef", "referenced storage child belongs to a different cluster", "correct $.spec.poolLayoutRef to satisfy this requirement: referenced storage child belongs to a different cluster")
}

func TestSubvolumeNameBound(t *testing.T) {
	catalog := subvolumeCatalog(t)
	noIssues(t, admitted(catalog, subvolumeObject(strings.Repeat("s", 48))))
	if len("ceph-subvolume-"+strings.Repeat("s", 48)) != 63 {
		t.Fatal("the bound no longer matches the 63-character block identity")
	}
	onlyIssue(t, admitted(catalog, subvolumeObject(strings.Repeat("s", 49))), "$.metadata.name", "a subvolume name longer than 48 characters does not fit its block identity ceph-subvolume-<name> of at most 63 characters", "shorten metadata.name to at most 48 characters")
}

func TestSubvolumeFilesystemOfAnotherClusterRefuses(t *testing.T) {
	catalog := subvolumeCatalog(t)
	candidate := object(api.StorageSubvolume, "docs", m("clusterRef", "storage", "filesystemRef", "remote", "group", "csi"))
	onlyIssue(t, admitted(catalog, candidate), "$.spec.filesystemRef", "referenced storage child belongs to a different cluster", "correct $.spec.filesystemRef to satisfy this requirement: referenced storage child belongs to a different cluster")
}

func TestSubvolumeModeIsLexicallyChecked(t *testing.T) {
	catalog := subvolumeCatalog(t)
	noIssues(t, admitted(catalog, subvolumeObject("docs", "mode", "0750")))
	if found := issuesAt(admitted(catalog, subvolumeObject("docs", "mode", "0x750")), "$.spec.mode"); len(found) != 1 {
		t.Fatalf("mode 0x750: %+v", found)
	}
	if found := issuesAt(ValidatePartial(object(api.StorageSubvolume, "docs", m("mode", "888")), api.Catalog{}), "$.spec.mode"); len(found) != 1 {
		t.Fatalf("partial mode 888: %+v", found)
	}
}
