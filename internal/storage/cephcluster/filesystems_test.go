package cephcluster

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func withFilesystemServices(t *testing.T, catalog api.Catalog, services ...api.Value) api.Catalog {
	t.Helper()
	cluster := find(t, catalog, api.CephCluster, "storage")
	all := []api.Value{}
	for _, existing := range cluster.Spec().Get("services").Items() {
		if textOf(existing, "service_type") != "mds" {
			all = append(all, existing)
		}
	}
	changed := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(append(all, services...)...), "services"))
	return replace(catalog, changed)
}

func boundFilesystem(t *testing.T) (api.Object, api.Catalog) {
	t.Helper()
	catalog := withFilesystemServices(t, fixtureCatalog(), mdsService("files"))
	return find(t, catalog, api.CephFilesystem, "files"), catalog
}

func renamedFilesystem(fs api.Object, name string) api.Object {
	return object(api.CephFilesystem, name, fs.Spec().With("mdsServiceRef", api.StringValue("mds."+name)))
}

func TestFilesystemMDSServiceRefMustResolveToMDSOfSameName(t *testing.T) {
	fs, catalog := boundFilesystem(t)
	noIssues(t, admitted(catalog, fs))
	mismatch := "spec.mdsServiceRef must name the mds service whose service_id is this filesystem's name, mds.files"
	mismatchRemedy := "set spec.mdsServiceRef to mds.files and declare that service in CephCluster storage's spec.services"
	for _, tt := range []struct {
		ref, message, remedy string
		catalog              api.Catalog
	}{
		{"osd.data", mismatch, mismatchRemedy, catalog},
		{"mds.files", "CephCluster storage declares no native service mds.files", "add a spec.services entry with service_type mds and service_id files to CephCluster storage", fixtureCatalog()},
		{"mds.other", mismatch, mismatchRemedy, withFilesystemServices(t, fixtureCatalog(), mdsService("other"))},
	} {
		changed := fs.WithSpec(fs.Spec().With("mdsServiceRef", api.StringValue(tt.ref)))
		found := issuesAt(admitted(tt.catalog, changed), "$.spec.mdsServiceRef")
		if len(found) != 1 || found[0].Code != "api.invariant" || found[0].Message != tt.message || found[0].Remediation != tt.remedy {
			t.Fatalf("%s: %#v", tt.ref, found)
		}
		if tt.ref != "mds.files" && strings.Contains(found[0].Message+found[0].Remediation, tt.ref) {
			t.Fatalf("%s: the refusal echoed the authored value", tt.ref)
		}
	}
}

func TestFilesystemNameStartingWithDigitRefuses(t *testing.T) {
	fs, _ := boundFilesystem(t)
	digit := renamedFilesystem(fs, "1files")
	catalog := withFilesystemServices(t, fixtureCatalog(), mdsService("1files"))
	onlyIssue(t, issuesAt(admitted(catalog, digit), "$.metadata.name"), "$.metadata.name", "a CephFS filesystem name that starts with a digit cannot name its MDS service, whose service_id equals the filesystem name and cannot start with a digit", "rename the CephFilesystem so its name starts with a letter")
	fs, catalog = boundFilesystem(t)
	noIssues(t, issuesAt(admitted(catalog, fs), "$.metadata.name"))
}

func TestFilesystemNameHasNoBlockDerivedLimit(t *testing.T) {
	fs, _ := boundFilesystem(t)
	name := "f" + strings.Repeat("s", 62)
	catalog := withFilesystemServices(t, fixtureCatalog(), mdsService(name))
	noIssues(t, issuesAt(admitted(catalog, renamedFilesystem(fs, name)), "$.metadata.name"))
}

func requireResolvable(t *testing.T, catalog api.Catalog, service api.Value) {
	t.Helper()
	if _, issues := ResolveServicePlacement(find(t, catalog, api.CephCluster, "storage"), service); len(issues) != 0 {
		t.Fatalf("placement %v does not resolve: %v", service.Get("placement"), issues)
	}
}

func TestMDSCoverage(t *testing.T) {
	fs, _ := boundFilesystem(t)
	hosts := func(count int) api.Value { return m("label", "mgr", "count", count) }
	pairs := func(count int) api.Value {
		return m("hosts", api.StringList("node-1.storage.example.test", "node-2.storage.example.test"), "count_per_host", count)
	}
	for _, tt := range []struct {
		mds           api.Value
		enough, fewer api.Value
		message       string
	}{
		{m("activeCount", 1, "standbyCountWanted", 1, "standbyReplay", false), hosts(2), hosts(1), "MDS service mds.files places 1 daemons, fewer than the 1 active ranks plus max(1 wanted standbys, 0 standby-replay followers) need (2)"},
		{m("activeCount", 2, "standbyCountWanted", 0, "standbyReplay", true), pairs(2), hosts(3), "MDS service mds.files places 3 daemons, fewer than the 2 active ranks plus max(0 wanted standbys, 2 standby-replay followers) need (4)"},
		{m("activeCount", 1, "standbyCountWanted", 2, "standbyReplay", true), hosts(3), hosts(2), "MDS service mds.files places 2 daemons, fewer than the 1 active ranks plus max(2 wanted standbys, 1 standby-replay followers) need (3)"},
	} {
		changed := fs.WithSpec(fs.Spec().With("mds", tt.mds))
		service := m("service_type", "mds", "service_id", "files", "placement", tt.enough)
		requireResolvable(t, withFilesystemServices(t, fixtureCatalog(), service), service)
		noIssues(t, admitted(withFilesystemServices(t, fixtureCatalog(), service), changed))
		service = service.With("placement", tt.fewer)
		requireResolvable(t, withFilesystemServices(t, fixtureCatalog(), service), service)
		onlyIssue(t, admitted(withFilesystemServices(t, fixtureCatalog(), service), changed), "$.spec.mds", tt.message, "add hosts to the placement of mds.files or raise its count, or lower spec.mds.activeCount, spec.mds.standbyCountWanted or spec.mds.standbyReplay")
	}
}

func cephfsDataPool(name string, pairs ...any) api.Object {
	return object(api.CephPool, name, m(append([]any{"clusterRef", "storage", "role", "cephfsData"}, pairs...)...))
}

func TestDefaultDataPoolMustBeReplicated(t *testing.T) {
	fs, catalog := boundFilesystem(t)
	catalog = replace(catalog, cephfsDataPool("erasure", "erasure", chunks(2, 1)))
	sole := fs.WithSpec(fs.Spec().With("dataPoolRefs", api.ListValue(m("name", "erasure", "default", true))))
	onlyIssue(t, admitted(catalog, sole), "$.spec.dataPoolRefs[0]", "the default data pool of a CephFS filesystem must be replicated: ceph fs new refuses an erasure-coded default data pool unless forced, and Bootwright never forces it", "mark a replicated cephfsData pool default in spec.dataPoolRefs and attach erasure-coded pools as additional data pools")
	extra := fs.WithSpec(fs.Spec().With("dataPoolRefs", api.ListValue(m("name", "data", "default", true), m("name", "erasure"))))
	noIssues(t, admitted(catalog, extra))
}

func TestExtraECDataPoolAllowedWithDerivedOverwrites(t *testing.T) {
	catalog := withFilesystemServices(t, clusterOf(t, 6, true), mdsService("files"))
	pool := cephfsDataPool("erasure", "erasure", chunks(4, 2))
	noIssues(t, admitted(catalog, pool))
	catalog = replace(catalog, pool)
	fs := find(t, catalog, api.CephFilesystem, "files")
	fs = fs.WithSpec(fs.Spec().With("dataPoolRefs", api.ListValue(m("name", "data", "default", true), m("name", "erasure"))))
	noIssues(t, admitted(catalog, fs))
}

func TestGroupLayoutMustBeAttachedPool(t *testing.T) {
	fs, catalog := boundFilesystem(t)
	catalog = replace(catalog, cephfsDataPool("unattached"))
	attached := fs.WithSpec(fs.Spec().With("subvolumeGroups", api.ListValue(m("name", "csi", "poolLayoutRef", "data"))))
	noIssues(t, admitted(catalog, attached))
	detached := fs.WithSpec(fs.Spec().With("subvolumeGroups", api.ListValue(m("name", "csi", "poolLayoutRef", "unattached"))))
	found := issuesAt(admitted(catalog, detached), "$.spec.subvolumeGroups[0].poolLayoutRef")
	if len(found) != 1 || found[0].Message != "group layout must use an attached filesystem data pool" {
		t.Fatalf("unattached layout: %#v", found)
	}
}

func TestGroupReservedNamesRefuse(t *testing.T) {
	fs, catalog := boundFilesystem(t)
	const field = "$.spec.subvolumeGroups[0].name"
	groups := func(name string) api.Object {
		return fs.WithSpec(fs.Spec().With("subvolumeGroups", api.ListValue(m("name", name))))
	}
	partial := func(name string) api.Object {
		return object(api.CephFilesystem, "files", m("subvolumeGroups", api.ListValue(m("name", name))))
	}
	for _, name := range []string{"_nogroup", "_index", "_deleting", "_legacy"} {
		message := "subvolume group name " + name + " is an internal directory of the CephFS volumes module (_nogroup, _index, _deleting, _legacy)"
		onlyIssue(t, admitted(catalog, groups(name)), field, message, "rename the subvolume group")
		onlyIssue(t, ValidatePartial(partial(name), api.Catalog{}), field, message, "rename the subvolume group")
	}
	grammar := "a subvolume group name uses letters, digits, '_', '.' and '-', starts with a letter or digit and has at most 128 characters"
	for _, name := range []string{"-csi", "c/si", strings.Repeat("g", 129)} {
		onlyIssue(t, admitted(catalog, groups(name)), field, grammar, "rename the subvolume group within that grammar")
		onlyIssue(t, ValidatePartial(partial(name), api.Catalog{}), field, grammar, "rename the subvolume group within that grammar")
	}
	noIssues(t, admitted(catalog, groups("csi")))
	noIssues(t, ValidatePartial(partial(strings.Repeat("g", 128)), api.Catalog{}))
}

func TestSeveralFilesystemsPerClusterEachBindOwnMDS(t *testing.T) {
	files, catalog := boundFilesystem(t)
	catalog = withFilesystemServices(t, catalog, mdsService("files"), mdsService("archive"))
	catalog = replace(catalog, object(api.CephPool, "archive-metadata", m("clusterRef", "storage", "role", "cephfsMetadata")))
	catalog = replace(catalog, cephfsDataPool("archive-data"))
	archive := object(api.CephFilesystem, "archive", m("clusterRef", "storage", "mdsServiceRef", "mds.archive", "metadataPoolRef", "archive-metadata", "dataPoolRefs", api.ListValue(m("name", "archive-data", "default", true))))
	catalog = replace(catalog, archive)
	noIssues(t, Validate(files, catalog))
	noIssues(t, Validate(archive, catalog))
	noIssues(t, Validate(find(t, catalog, api.CephCluster, "storage"), catalog))
	for _, tt := range []struct {
		shared, field string
		change        api.Value
	}{
		{"metadata", "$.spec.metadataPoolRef", archive.Spec().With("metadataPoolRef", api.StringValue("metadata"))},
		{"data", "$.spec.dataPoolRefs[0]", archive.Spec().With("dataPoolRefs", api.ListValue(m("name", "data", "default", true)))},
	} {
		changed := archive.WithSpec(tt.change)
		shared := replace(catalog, changed)
		onlyIssue(t, Validate(changed, shared), tt.field, "CephPool "+tt.shared+" is already used by CephFilesystem files; a pool belongs to one filesystem", "give CephFilesystem archive and CephFilesystem files separate pools")
		onlyIssue(t, Validate(files, shared), tt.field, "CephPool "+tt.shared+" is already used by CephFilesystem archive; a pool belongs to one filesystem", "give CephFilesystem files and CephFilesystem archive separate pools")
	}
}
