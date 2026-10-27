package cephcluster

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func smbHosts(names ...string) api.Value {
	hosts := []string{}
	for _, name := range names {
		hosts = append(hosts, name+".storage.example.test")
	}
	return api.StringList(hosts...)
}

func smbClusterObject(name, id string, placement api.Value, extra ...any) api.Object {
	native := m("resource_type", api.SMBCluster, "cluster_id", id, "auth_mode", "user", "user_group_settings", []api.Value{m("source_type", "resource", "ref", "users")}, "placement", placement)
	for i := 0; i < len(extra); i += 2 {
		native = native.With(extra[i].(string), v(extra[i+1]))
	}
	return object(api.CephSMBResource, name, m("clusterRef", "storage", "resourceRefs", api.StringList("smb-users"), "native", native))
}

func smbShareObject(name, cluster, smbCluster, id string, backing ...any) api.Object {
	native := m("resource_type", api.SMBShare, "cluster_id", cluster, "share_id", id)
	spec := m("clusterRef", "storage", "smbClusterRef", smbCluster, "native", native)
	for i := 0; i < len(backing); i += 2 {
		spec = spec.With(backing[i].(string), v(backing[i+1]))
	}
	return object(api.CephSMBResource, name, spec)
}

func smbAdd(catalog api.Catalog, objects ...api.Object) api.Catalog {
	for _, object := range objects {
		catalog = replace(catalog, object)
	}
	return catalog
}

func smbValidate(t *testing.T, catalog api.Catalog, name string, change func(api.Value) api.Value) []api.Issue {
	t.Helper()
	resource, changed := smbWith(t, catalog, name, change)
	return Validate(resource, changed)
}

func smbOnly(t *testing.T, issues []api.Issue, code, field, message, remedy string) {
	t.Helper()
	if len(issues) != 1 {
		t.Fatalf("want one issue at %s, got %#v", field, issues)
	}
	exactIssue(t, issues, code, field, message, remedy)
}

func smbKeep(spec api.Value) api.Value { return spec }

func TestSMBResourceRefsMustBeConsumedExactly(t *testing.T) {
	catalog := smbCatalog(t)
	unused, unusedFix := "no native reference of this SMB cluster consumes this resource", "remove it from spec.resourceRefs, or name its ID in domain_settings.join_sources or user_group_settings"
	refs := func(names ...string) func(api.Value) api.Value {
		return smbSpec("resourceRefs", api.StringList(names...))
	}
	smbOnly(t, smbValidate(t, catalog, "smb-files", refs("smb-users", "smb-join")), "api.invariant", "$.spec.resourceRefs[1]", unused, unusedFix)
	smbOnly(t, smbValidate(t, catalog, "smb-files", refs("smb-users", "smb-docs")), "api.invariant", "$.spec.resourceRefs[1]", "spec.resourceRefs names only ceph.smb.join.auth and ceph.smb.usersgroups resources", "remove this member from spec.resourceRefs")
	foreign := object(api.CephSMBResource, "smb-foreign", m("clusterRef", "elsewhere", "native", m("resource_type", api.SMBUsersGroups, "users_groups_id", "users")))
	smbOnly(t, smbValidate(t, smbAdd(catalog, foreign), "smb-files", refs("smb-users", "smb-foreign")), "api.invariant", "$.spec.resourceRefs[1]", "referenced storage child belongs to a different cluster", "")
	smbOnly(t, smbValidate(t, catalog, "smb-files", smbChain(refs(), smbNative("user_group_settings", []api.Value{m("ref", "nobody")}))), "api.invariant", "$.spec.native.user_group_settings[0].ref", "this reference names no ceph.smb.usersgroups resource in spec.resourceRefs", "add the CephSMBResource that declares this ID to spec.resourceRefs, or correct the reference")
	twin := find(t, catalog, api.CephSMBResource, "smb-users")
	twin = api.NewObject(api.CephSMBResource, "smb-users-twin", api.MapValue(), twin.Spec())
	smbOnly(t, smbValidate(t, smbAdd(catalog, twin), "smb-files", refs("smb-users", "smb-users-twin")), "api.invariant", "$.spec.native.user_group_settings[0].ref", "this reference matches more than one CephSMBResource in spec.resourceRefs", "keep one resource with this ID in spec.resourceRefs")
	ad := func(ref string, members ...string) func(api.Value) api.Value {
		domain := m("realm", "example.com", "join_sources", []api.Value{m("source_type", "resource", "ref", ref)})
		return smbChain(refs(members...), smbNative("auth_mode", "active-directory", "user_group_settings", nil, "domain_settings", domain))
	}
	issues := smbValidate(t, catalog, "smb-files", ad("users", "smb-users"))
	if len(issues) != 2 {
		t.Fatalf("a join source naming a usersgroups ID: %#v", issues)
	}
	exactIssue(t, issues, "api.invariant", "$.spec.native.domain_settings.join_sources[0].ref", "this reference names no ceph.smb.join.auth resource in spec.resourceRefs", "")
	exactIssue(t, issues, "api.invariant", "$.spec.resourceRefs[0]", unused, unusedFix)
	for _, change := range []func(api.Value) api.Value{smbKeep, ad("join", "smb-join")} {
		resource, changed := smbWith(t, catalog, "smb-files", change)
		requireAdmits(t, resource, changed)
	}
}

func TestSMBShareBindingAgreesWithBacking(t *testing.T) {
	catalog := smbCatalog(t)
	cephfs := func(pairs ...any) func(api.Value) api.Value { return smbNative("cephfs", m(pairs...)) }
	for _, row := range []struct {
		leaf            string
		value           string
		message, remedy string
	}{
		{"volume", "archive", "cephfs.volume disagrees with the filesystem the share's binding selects", "omit spec.native.cephfs.volume so the binding supplies it, or set it to the bound filesystem's name"},
		{"subvolumegroup", "apps", "cephfs.subvolumegroup disagrees with the group of the CephSubvolume the share binds", "omit spec.native.cephfs.subvolumegroup so the binding supplies it, or set it to the bound subvolume's group"},
		{"subvolume", "media", "cephfs.subvolume disagrees with the CephSubvolume the share binds", "omit spec.native.cephfs.subvolume so the binding supplies it, or set it to the bound subvolume's name"},
	} {
		smbOnly(t, smbValidate(t, catalog, "smb-docs", cephfs(row.leaf, row.value)), "api.invariant", "$.spec.native.cephfs."+row.leaf, row.message, row.remedy)
	}
	grouped := find(t, catalog, api.CephSubvolume, "docs")
	grouped = grouped.WithSpec(grouped.Spec().With("group", api.StringValue("apps")))
	smbOnly(t, smbValidate(t, replace(catalog, grouped), "smb-docs", cephfs("subvolumegroup", "other")), "api.invariant", "$.spec.native.cephfs.subvolumegroup", "cephfs.subvolumegroup disagrees with the group of the CephSubvolume the share binds", "")
	filesystem := smbChain(smbSpec("subvolumeRef", nil), smbSpec("filesystemRef", "files"))
	for _, leaf := range []string{"subvolume", "subvolumegroup"} {
		full, partial := smbBoth(t, catalog, "smb-docs", smbChain(filesystem, cephfs(leaf, "docs")))
		for _, issues := range [][]api.Issue{full, partial} {
			smbOnly(t, issues, "api.invariant", "$.spec.native.cephfs."+leaf, "a share bound through spec.filesystemRef names no subvolume", "bind the subvolume through spec.subvolumeRef, or omit spec.native.cephfs."+leaf)
		}
	}
	smbOnly(t, smbValidate(t, catalog, "smb-docs", cephfs("path", "/a/../b")), "api.value", "$.spec.native.cephfs.path", "an SMB share path must be clean and absolute", "")
	for _, admitted := range []struct {
		catalog api.Catalog
		change  func(api.Value) api.Value
	}{
		{catalog, smbKeep},
		{catalog, cephfs("volume", "files", "subvolume", "docs", "path", "/")},
		{replace(catalog, grouped), cephfs("volume", "files", "subvolumegroup", "apps", "subvolume", "docs")},
		{catalog, smbChain(filesystem, cephfs("volume", "files", "path", "/"))},
	} {
		resource, changed := smbWith(t, admitted.catalog, "smb-docs", admitted.change)
		requireAdmits(t, resource, changed)
	}
}

func TestSMBShareAndNFSExportCannotExposeOneBacking(t *testing.T) {
	catalog := smbAdd(smbCatalog(t), object(api.CephFilesystem, "archive", m("clusterRef", "storage")))
	exclusive, exclusiveFix := "exposes the same CephFS data over NFS, and one backing is exposed by one protocol", "bind both protocols through separate CephSubvolumes"
	filesystem := smbChain(smbSpec("subvolumeRef", nil), smbSpec("filesystemRef", "files"))
	export := func(key, name string) api.Object {
		return object(api.CephNFSExport, "nfs-export", m("clusterRef", "storage", key, name))
	}
	for _, row := range []struct {
		export api.Object
		share  func(api.Value) api.Value
		field  string
	}{
		{export("subvolumeRef", "docs"), smbKeep, "$.spec.subvolumeRef"},
		{export("filesystemRef", "files"), smbKeep, "$.spec.subvolumeRef"},
		{export("subvolumeRef", "media"), filesystem, "$.spec.filesystemRef"},
		{export("filesystemRef", "files"), filesystem, "$.spec.filesystemRef"},
	} {
		smbOnly(t, smbValidate(t, smbAdd(catalog, row.export), "smb-docs", row.share), "api.invariant", row.field, "CephNFSExport nfs-export "+exclusive, exclusiveFix)
	}
	for _, other := range []api.Object{export("subvolumeRef", "media"), export("filesystemRef", "archive")} {
		resource, changed := smbWith(t, smbAdd(catalog, other), "smb-docs", smbKeep)
		requireAdmits(t, resource, changed)
	}
	second := smbAdd(catalog, smbClusterObject("smb-other", "other", m("hosts", smbHosts("node-2"), "count", 1)), smbShareObject("smb-other-docs", "other", "smb-other", "docs", "subvolumeRef", "docs"))
	smbOnly(t, smbValidate(t, second, "smb-docs", smbKeep), "api.invariant", "$.spec.subvolumeRef", "CephSMBResource smb-other-docs shares this subvolume from another SMB cluster", "bind each SMB cluster's shares to its own CephSubvolume")
}

func TestSMBClusterPlacementRequiredAndDisjoint(t *testing.T) {
	catalog := smbCatalog(t)
	smbOnly(t, smbValidate(t, catalog, "smb-files", smbNative("placement", nil)), "api.required", "$.spec.native.placement", "this declaration requires the named field", "")
	placed := func(names []string, count int) func(api.Value) api.Value {
		return smbNative("placement", m("hosts", smbHosts(names...), "count", count))
	}
	overlap := smbAdd(catalog, smbClusterObject("smb-other", "other", m("hosts", smbHosts("node-2", "node-3"), "count", 1)))
	smbOnly(t, smbValidate(t, overlap, "smb-files", placed([]string{"node-1", "node-2"}, 2)), "api.invariant", "$.spec.native.placement", "SMB cluster CephSMBResource smb-other may also run on host node-2, and one host serves one SMB cluster because each binds the SMB port", "select disjoint hosts for the two SMB clusters")
	disjoint := smbAdd(catalog, smbClusterObject("smb-other", "other", m("hosts", smbHosts("node-2"), "count", 1)))
	resource, changed := smbWith(t, disjoint, "smb-files", placed([]string{"node-1"}, 1))
	requireAdmits(t, resource, changed)
}

func TestSMBListenersJoinTheClusterRegistry(t *testing.T) {
	catalog := smbCatalog(t)
	withService := func(port int) api.Catalog {
		_, changed := withGraph(t, catalog, []api.Value{m("service_type", "grafana", "placement", m("hosts", smbHosts("node-1")), "spec", m("port", port))})
		return changed
	}
	remedy := "place this SMB cluster on other hosts, or move the other listener to another port"
	smbOnly(t, smbValidate(t, withService(445), "smb-files", smbKeep), "api.invariant", "$.spec.native.placement", "the SMB port 445 is already bound on host node-1 by CephCluster storage $.spec.services[3].spec.port", remedy)
	smbOnly(t, smbValidate(t, withService(9922), "smb-files", smbKeep), "api.invariant", "$.spec.native.placement", "the SMB port 9922 is already bound on host node-1 by CephCluster storage $.spec.services[3].spec.port", remedy)
	ctdb := withService(4379)
	smbOnly(t, smbValidate(t, ctdb, "smb-files", smbNative("clustering", "always")), "api.invariant", "$.spec.native.placement", "the SMB port 4379 is already bound on host node-1 by CephCluster storage $.spec.services[3].spec.port", remedy)
	smbOnly(t, smbValidate(t, ctdb, "smb-files", smbNative("placement", m("hosts", smbHosts("node-1")))), "api.invariant", "$.spec.native.placement", "the SMB port 4379 is already bound on host node-1 by CephCluster storage $.spec.services[3].spec.port", remedy)
	for _, change := range []func(api.Value) api.Value{smbKeep, smbNative("clustering", "never")} {
		resource, changed := smbWith(t, ctdb, "smb-files", change)
		requireAdmits(t, resource, changed)
	}
}

func TestSMBPublicAddrsNeedClusteringAndPublicCIDR(t *testing.T) {
	_, catalog := withGraph(t, smbCatalog(t), []api.Value{ingressService(m())})
	addrs := func(clustering string, addresses ...string) func(api.Value) api.Value {
		list := []api.Value{}
		for _, address := range addresses {
			list = append(list, m("address", address))
		}
		if clustering == "" {
			return smbNative("public_addrs", list)
		}
		return smbNative("public_addrs", list, "clustering", clustering)
	}
	first := "$.spec.native.public_addrs[0].address"
	full, partial := smbBoth(t, catalog, "smb-files", addrs("", "192.0.2.80/24"))
	for _, issues := range [][]api.Issue{full, partial} {
		smbOnly(t, issues, "api.invariant", "$.spec.native.public_addrs", "CTDB assigns SMB public addresses, so they need clustering", "set clustering to always, or to default with a placement.count other than 1, or remove public_addrs")
	}
	full, partial = smbBoth(t, catalog, "smb-files", addrs("always", "192.0.2.80"))
	for _, issues := range [][]api.Issue{full, partial} {
		smbOnly(t, issues, "api.value", first, "an SMB public address is an address with its prefix length", "write address as <ip>/<prefix>")
	}
	cidr := "an SMB public address must lie inside a declared public CIDR and carry that CIDR's prefix length"
	network := "an SMB public address cannot be the network or broadcast address of its public CIDR"
	for _, row := range []struct{ address, message string }{
		{"198.51.100.80/24", cidr},
		{"192.0.2.80/25", cidr},
		{"192.0.2.0/24", network},
		{"192.0.2.255/24", network},
		{"192.0.2.1/24", "an SMB public address equals a declared Machine address"},
		{"192.0.2.50/24", "an SMB public address is already the virtual IP of an ingress service"},
	} {
		smbOnly(t, smbValidate(t, catalog, "smb-files", addrs("always", row.address)), "api.invariant", first, row.message, "")
	}
	smbOnly(t, smbValidate(t, catalog, "smb-files", addrs("always", "192.0.2.80/24", "192.0.2.80/24")), "api.invariant", "$.spec.native.public_addrs[1].address", "public addresses of one SMB cluster must be unique", "remove the repeated address")
	other := smbAdd(catalog, smbClusterObject("smb-other", "other", m("hosts", smbHosts("node-2"), "count", 1), "clustering", "always", "public_addrs", []api.Value{m("address", "192.0.2.80/24")}))
	smbOnly(t, smbValidate(t, other, "smb-files", addrs("always", "192.0.2.80/24")), "api.invariant", first, "this SMB public address is already used by CephSMBResource smb-other", "choose an address no other SMB cluster uses")
	resource, changed := smbWith(t, catalog, "smb-files", addrs("always", "192.0.2.80/24"))
	requireAdmits(t, resource, changed)
}

func TestSMBNativeIdentitiesAndShareNamesAreUnique(t *testing.T) {
	catalog := smbCatalog(t)
	owned, ownedFix := "already owns this native SMB resource", "give this resource an ID no other CephSMBResource of this cluster uses"
	twin := smbAdd(catalog, smbClusterObject("smb-other", "files", m("hosts", smbHosts("node-2"), "count", 1)))
	smbOnly(t, smbValidate(t, twin, "smb-files", smbKeep), "api.invariant", "$.spec.native.cluster_id", "CephSMBResource smb-other "+owned, ownedFix)
	same := smbAdd(catalog, smbShareObject("smb-again", "files", "smb-files", "docs", "subvolumeRef", "media"))
	smbOnly(t, smbValidate(t, same, "smb-docs", smbKeep), "api.invariant", "$.spec.native.share_id", "CephSMBResource smb-again "+owned, ownedFix)
	named := smbShareObject("smb-media", "files", "smb-files", "media", "subvolumeRef", "media")
	named = named.WithSpec(named.Spec().WithPath(api.StringValue("docs"), "native", "name"))
	collide := smbAdd(catalog, named)
	served, servedFix := "already serves this share name on the same SMB cluster", "set spec.native.name to a name no other share of this SMB cluster uses"
	smbOnly(t, smbValidate(t, collide, "smb-docs", smbKeep), "api.invariant", "$.spec.native.share_id", "CephSMBResource smb-media "+served, servedFix)
	smbOnly(t, Validate(named, collide), "api.invariant", "$.spec.native.name", "CephSMBResource smb-docs "+served, servedFix)
	apart := smbAdd(catalog, smbClusterObject("smb-other", "other", m("hosts", smbHosts("node-2"), "count", 1)), smbShareObject("smb-other-media", "other", "smb-other", "docs", "subvolumeRef", "media"))
	resource, changed := smbWith(t, apart, "smb-docs", smbKeep)
	requireAdmits(t, resource, changed)
}

func TestSMBShareNamesItsOwnSMBCluster(t *testing.T) {
	catalog := smbCatalog(t)
	smbOnly(t, smbValidate(t, catalog, "smb-docs", smbSpec("smbClusterRef", "smb-users")), "api.invariant", "$.spec.smbClusterRef", "spec.smbClusterRef must name a ceph.smb.cluster CephSMBResource", "set spec.smbClusterRef to the CephSMBResource that declares the SMB cluster")
	smbOnly(t, smbValidate(t, smbAdd(catalog, smbClusterObject("smb-other", "other", m("hosts", smbHosts("node-2"), "count", 1))), "smb-docs", smbSpec("smbClusterRef", "smb-other")), "api.invariant", "$.spec.native.cluster_id", "cluster_id disagrees with the cluster_id of the CephSMBResource spec.smbClusterRef names", "set spec.native.cluster_id to the cluster_id of CephSMBResource smb-other")
}

func TestSMBDiagnosticsNeverEchoAuthoredValues(t *testing.T) {
	const sentinel = "S3NTINEL"
	catalog := smbCatalog(t)
	cluster := smbClusterObject("smb-other", sentinel+"-1", m("hosts", smbHosts("node-2"), "count", 1), "custom_dns", api.StringList(sentinel+"-dns"), "public_addrs", []api.Value{m("address", sentinel+"-addr")}, "client_compat", sentinel, "custom_smb_global_options", m("k", sentinel))
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(m("ref", sentinel+"-2")), "native", "user_group_settings"))
	twin := smbClusterObject("smb-twin", sentinel+"-1", m("hosts", smbHosts("node-3"), "count", 1))
	share := smbShareObject("smb-share", sentinel+"-1", "smb-files", sentinel+"-3", "subvolumeRef", "media")
	share = share.WithSpec(share.Spec().With("native", share.Spec().Get("native").With("name", api.StringValue(sentinel+" name!")).With("comment", api.StringValue(sentinel)).With("cephfs", m("volume", sentinel+"-v", "subvolume", sentinel+"-s", "subvolumegroup", sentinel+"-g", "path", sentinel+"/../x")).With("login_control", api.ListValue(m("name", sentinel+" login"), m("name", sentinel+" login")))))
	join := object(api.CephSMBResource, "smb-join2", m("clusterRef", "storage", "native", m("resource_type", api.SMBJoinAuth, "auth_id", sentinel+"-join-too-long-id", "auth", m("username", sentinel+"-user", "password", sentinel+"-pass"))))
	users := object(api.CephSMBResource, "smb-users2", m("clusterRef", "storage", "native", m("resource_type", api.SMBUsersGroups, "users_groups_id", sentinel+" ug", "values", m("users", []api.Value{m("name", sentinel+" user", "password", sentinel)}, "groups", []api.Value{m("name", sentinel+" g")}))))
	unknown := object(api.CephSMBResource, "smb-unknown", m("clusterRef", "storage", "native", m("resource_type", sentinel+"-type")))
	planted := smbAdd(catalog, cluster, twin, share, join, users, unknown)
	identity := false
	for _, resource := range []api.Object{cluster, twin, share, join, users, unknown} {
		for _, issues := range [][]api.Issue{Validate(resource, planted), ValidatePartial(resource, planted)} {
			for _, issue := range issues {
				if strings.Contains(issue.Message+issue.Remediation, sentinel) {
					t.Fatalf("%s echoed an authored value: %#v", resource.Name(), issue)
				}
				identity = identity || strings.Contains(issue.Message, "already owns this native SMB resource")
			}
		}
	}
	if !identity {
		t.Fatal("the planted identity collision raised no uniqueness issue")
	}
}

func TestSMBPartialAndCompleteValidationAgree(t *testing.T) {
	catalog := smbCatalog(t)
	filesystem := smbChain(smbSpec("subvolumeRef", nil), smbSpec("filesystemRef", "files"), smbNative("cephfs", m("subvolume", "docs")))
	for _, row := range []struct {
		name   string
		change func(api.Value) api.Value
	}{
		{"smb-users", smbNative("intent", "removed")},
		{"smb-join", smbNative("linked_to_cluster", "files")},
		{"smb-join", smbNative("auth", m("username", "svc-join", "password", "s3cr3t"))},
		{"smb-docs", smbNative("comment", "text")},
		{"smb-users", smbNative("users_groups_id", "bad_id")},
		{"smb-files", smbNative("domain_settings", m("realm", "example.com", "join_sources", []api.Value{m("ref", "join")}))},
		{"smb-files", smbNative("clustering", "never", "placement", m("label", "storage", "count", 2))},
		{"smb-files", smbNative("public_addrs", []api.Value{m("address", "192.0.2.80/24")})},
		{"smb-docs", filesystem},
		{"smb-join", smbSpec("secrets", []api.Value{m("path", "/auth/username", "secretRef", "smb-pass")})},
		{"smb-files", smbSpec("secrets", []api.Value{m("path", "/auth/password", "secretRef", "smb-pass")})},
		{"smb-files", smbNative("placement", m("label", "storage", "count_per_host", 2))},
	} {
		full, partial := smbBoth(t, catalog, row.name, row.change)
		if len(partial) == 0 {
			t.Fatalf("%s: the fragment edit raised nothing in partial validation", row.name)
		}
		for _, issue := range partial {
			found := false
			for _, complete := range full {
				found = found || (complete.Code == issue.Code && complete.Field == issue.Field && complete.Message == issue.Message)
			}
			if !found {
				t.Fatalf("%s: partial issue %#v is missing from complete validation %#v", row.name, issue, full)
			}
		}
	}
}
