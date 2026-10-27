package cephcluster

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func smbCatalog(t *testing.T) api.Catalog {
	t.Helper()
	pass := func(path string) []api.Value { return []api.Value{m("path", path, "secretRef", "smb-pass")} }
	objects := append(withoutObject(fixtureCatalog(), api.CephNFSExport, "nfs").Objects(),
		object(api.CephSubvolume, "docs", m("clusterRef", "storage", "filesystemRef", "files")),
		object(api.CephSubvolume, "media", m("clusterRef", "storage", "filesystemRef", "files")),
		object(api.Secret, "smb-pass", m("type", "opaque")),
		object(api.CephSMBResource, "smb-users", m("clusterRef", "storage", "secrets", pass("/values/users/0/password"), "native", m("resource_type", api.SMBUsersGroups, "users_groups_id", "users", "values", m("users", []api.Value{m("name", "alice")}, "groups", []api.Value{})))),
		object(api.CephSMBResource, "smb-join", m("clusterRef", "storage", "secrets", pass("/auth/password"), "native", m("resource_type", api.SMBJoinAuth, "auth_id", "join", "auth", m("username", "svc-join")))),
		object(api.CephSMBResource, "smb-files", m("clusterRef", "storage", "resourceRefs", api.StringList("smb-users"), "native", m("resource_type", api.SMBCluster, "cluster_id", "files", "auth_mode", "user", "user_group_settings", []api.Value{m("source_type", "resource", "ref", "users")}, "placement", m("hosts", api.StringList("node-1.storage.example.test"), "count", 1)))),
		object(api.CephSMBResource, "smb-docs", m("clusterRef", "storage", "smbClusterRef", "smb-files", "subvolumeRef", "docs", "native", m("resource_type", api.SMBShare, "cluster_id", "files", "share_id", "docs"))),
	)
	catalog := normalizeCatalog(api.NewCatalog(objects))
	for _, name := range []string{"smb-users", "smb-join", "smb-files", "smb-docs"} {
		requireAdmits(t, find(t, catalog, api.CephSMBResource, name), catalog)
	}
	return catalog
}

func smbWith(t *testing.T, catalog api.Catalog, name string, change func(api.Value) api.Value) (api.Object, api.Catalog) {
	t.Helper()
	resource := find(t, catalog, api.CephSMBResource, name)
	resource = resource.WithSpec(change(resource.Spec()))
	return resource, replace(catalog, resource)
}

func smbNative(changes ...any) func(api.Value) api.Value {
	return func(spec api.Value) api.Value {
		native := spec.Get("native")
		for i := 0; i < len(changes); i += 2 {
			if changes[i+1] == nil {
				native = native.Without(changes[i].(string))
			} else {
				native = native.With(changes[i].(string), v(changes[i+1]))
			}
		}
		return spec.With("native", native)
	}
}

func smbSpec(key string, value any) func(api.Value) api.Value {
	return func(spec api.Value) api.Value {
		if value == nil {
			return spec.Without(key)
		}
		return spec.With(key, v(value))
	}
}

func smbBoth(t *testing.T, catalog api.Catalog, name string, change func(api.Value) api.Value) ([]api.Issue, []api.Issue) {
	t.Helper()
	resource, changed := smbWith(t, catalog, name, change)
	return Validate(resource, changed), ValidatePartial(resource, changed)
}

func TestSMBResourceSchemaIsClosedPerResourceType(t *testing.T) {
	catalog := smbCatalog(t)
	for _, name := range []string{"smb-users", "smb-join", "smb-files", "smb-docs"} {
		full, partial := smbBoth(t, catalog, name, smbNative("bogus_member", "x"))
		if len(full) != 1 || len(partial) != 1 {
			t.Fatalf("%s: unknown member produced %#v / %#v", name, full, partial)
		}
		exactIssue(t, full, "api.field", "$.spec.native", "native field is not in the supported schema", "remove the unsupported field at $.spec.native")
		exactIssue(t, partial, "api.field", "$.spec.native", "native field is not in the supported schema", "remove the unsupported field at $.spec.native")
	}
	full, _ := smbBoth(t, catalog, "smb-docs", smbNative("cephfs", m("bogus", "x")))
	exactIssue(t, full, "api.field", "$.spec.native.cephfs", "native field is not in the supported schema", "remove the unsupported field at $.spec.native.cephfs")
	types := "ceph.smb.cluster, ceph.smb.share, ceph.smb.join.auth and ceph.smb.usersgroups"
	for rt, texts := range map[string][2]string{
		"ceph.smb.tls.credential": {"Bootwright does not realize ceph.smb.tls.credential resources", "remove this CephSMBResource"},
		"ceph.smb.other":          {"native SMB resource type is not one of " + types, "set spec.native.resource_type to one of " + types},
	} {
		full, partial := smbBoth(t, catalog, "smb-users", smbNative("resource_type", rt))
		if len(full) != 1 || len(partial) != 1 {
			t.Fatalf("%s produced %#v / %#v", rt, full, partial)
		}
		exactIssue(t, full, "api.invariant", "$.spec.native.resource_type", texts[0], texts[1])
		exactIssue(t, partial, "api.invariant", "$.spec.native.resource_type", texts[0], texts[1])
	}
	full, partial := smbBoth(t, catalog, "smb-users", smbNative("resource_type", nil))
	exactIssue(t, full, "api.required", "$.spec.native.resource_type", "this declaration requires the named field", "")
	if len(partial) != 0 {
		t.Fatalf("a fragment without resource_type was refused: %#v", partial)
	}
}

func TestSMBFieldsOutsideTheIntersectionRefuse(t *testing.T) {
	catalog := smbCatalog(t)
	for _, row := range []struct {
		name, member    string
		value           any
		field           string
		message, remedy string
	}{
		{"smb-files", "custom_smb_global_options", m("log level", "s3cr3t"), "custom_smb_global_options", "Bootwright does not realize custom Samba global options", "omit spec.native.custom_smb_global_options"},
		{"smb-files", "client_compat", "s3cr3t", "client_compat", "Bootwright does not realize client_compat", "omit spec.native.client_compat"},
		{"smb-files", "custom_ports", m("smb", 4450), "custom_ports", "Bootwright does not realize custom SMB ports", "omit spec.native.custom_ports"},
		{"smb-files", "bind_addrs", []api.Value{m("address", "s3cr3t")}, "bind_addrs", "Bootwright does not realize SMB bind addresses", "omit spec.native.bind_addrs"},
		{"smb-files", "remote_control", m("enabled", true), "remote_control", "Bootwright does not realize the SMB remote control service", "omit spec.native.remote_control"},
		{"smb-files", "public_addrs", []api.Value{m("address", "192.0.2.80/24"), m("address", "192.0.2.81/24", "destination", "s3cr3t")}, "public_addrs[1].destination", "Bootwright does not realize public address destinations", "omit destination from spec.native.public_addrs[1]"},
		{"smb-docs", "comment", "s3cr3t", "comment", "Bootwright does not realize SMB share comments", "omit spec.native.comment"},
		{"smb-docs", "max_connections", 10, "max_connections", "Bootwright does not realize SMB share connection limits", "omit spec.native.max_connections"},
		{"smb-docs", "custom_smb_share_options", m("veto files", "s3cr3t"), "custom_smb_share_options", "Bootwright does not realize custom Samba share options", "omit spec.native.custom_smb_share_options"},
	} {
		changes := []any{row.member, row.value}
		if row.member == "public_addrs" {
			changes = append(changes, "clustering", "always")
		}
		full, partial := smbBoth(t, catalog, row.name, smbNative(changes...))
		if len(full) != 1 || len(partial) != 1 {
			t.Fatalf("%s produced %#v / %#v", row.field, full, partial)
		}
		exactIssue(t, full, "api.invariant", "$.spec.native."+row.field, row.message, row.remedy)
		exactIssue(t, partial, "api.invariant", "$.spec.native."+row.field, row.message, row.remedy)
		text := full[0].Message + " " + full[0].Remediation
		if !strings.HasPrefix(full[0].Message, "Bootwright does not realize") || strings.Contains(text, "s3cr3t") {
			t.Fatalf("%s: %q", row.field, text)
		}
		for _, token := range []string{"IBM", "9.", "B47", "OD", "release"} {
			if strings.Contains(text, token) {
				t.Fatalf("%s names %q: %q", row.field, token, text)
			}
		}
	}
}

func TestSMBIntentRemovedIsNotApplyTimeAuthorization(t *testing.T) {
	catalog := smbCatalog(t)
	for _, name := range []string{"smb-users", "smb-join", "smb-files", "smb-docs"} {
		full, partial := smbBoth(t, catalog, name, smbNative("intent", "removed"))
		for _, issues := range [][]api.Issue{full, partial} {
			if len(issues) != 1 {
				t.Fatalf("%s: %#v", name, issues)
			}
			exactIssue(t, issues, "api.invariant", "$.spec.native.intent", "native intent removed is not apply-time removal authorization; Bootwright removes an SMB resource only through its removal lifecycle", "omit spec.native.intent or set it to present")
		}
		present, changed := smbWith(t, catalog, name, smbNative("intent", "present"))
		requireAdmits(t, present, changed)
	}
}

func TestSMBLinkedToClusterIsDeferredPendingOD14(t *testing.T) {
	catalog := smbCatalog(t)
	for _, name := range []string{"smb-users", "smb-join"} {
		full, partial := smbBoth(t, catalog, name, smbNative("linked_to_cluster", "files"))
		for _, issues := range [][]api.Issue{full, partial} {
			if len(issues) != 1 {
				t.Fatalf("%s: %#v", name, issues)
			}
			exactIssue(t, issues, "api.invariant", "$.spec.native.linked_to_cluster", "Bootwright does not realize linked SMB auth resources; each CephSMBResource applies and removes its own native resource", "omit spec.native.linked_to_cluster; the SMB cluster names this resource in its spec.resourceRefs")
		}
	}
	full, _ := smbBoth(t, catalog, "smb-files", smbNative("linked_to_cluster", "files"))
	exactIssue(t, full, "api.field", "$.spec.native", "native field is not in the supported schema", "")
}

func TestSMBAuthModeRequiresItsSettings(t *testing.T) {
	catalog := smbCatalog(t)
	join := []api.Value{m("source_type", "resource", "ref", "join")}
	domain := m("realm", "EXAMPLE.COM", "join_sources", join)
	both := smbSpec("resourceRefs", api.StringList("smb-users", "smb-join"))
	none := smbSpec("resourceRefs", nil)
	for _, row := range []struct {
		change          func(api.Value) api.Value
		field           string
		partial         bool
		message, remedy string
	}{
		{smbChain(both, smbNative("domain_settings", domain)), "domain_settings", true, "a user-mode SMB cluster takes no domain_settings", "remove domain_settings, or set auth_mode: active-directory"},
		{smbChain(both, smbNative("auth_mode", "active-directory", "domain_settings", domain)), "user_group_settings", true, "an active-directory SMB cluster takes no user_group_settings", "remove user_group_settings, or set auth_mode: user"},
		{smbChain(none, smbNative("user_group_settings", nil)), "user_group_settings", false, "a user-mode SMB cluster needs user_group_settings", "add a user_group_settings entry whose ref is the users_groups_id of a ceph.smb.usersgroups resource named in spec.resourceRefs"},
		{smbChain(none, smbNative("auth_mode", "active-directory", "user_group_settings", nil)), "domain_settings", false, smbADSettings, smbADSettingsFix},
		{smbChain(none, smbNative("auth_mode", "active-directory", "user_group_settings", nil, "domain_settings", m("realm", "example.com", "join_sources", []api.Value{}))), "domain_settings.join_sources", false, smbADSettings, smbADSettingsFix},
		{smbChain(none, smbNative("user_group_settings", []api.Value{m("source_type", "empty", "ref", "users")})), "user_group_settings[0].source_type", true, "Bootwright does not realize user-mode SMB clusters without a users-and-groups resource", "set source_type: resource and ref to the users_groups_id of a ceph.smb.usersgroups resource in spec.resourceRefs"},
	} {
		full, partial := smbBoth(t, catalog, "smb-files", row.change)
		if len(full) != 1 {
			t.Fatalf("%s: %#v", row.field, full)
		}
		exactIssue(t, full, "api.invariant", "$.spec.native."+row.field, row.message, row.remedy)
		if row.partial {
			exactIssue(t, partial, "api.invariant", "$.spec.native."+row.field, row.message, row.remedy)
		} else if len(partial) != 0 {
			t.Fatalf("%s refused a fragment: %#v", row.field, partial)
		}
	}
	dns := api.StringList("192.0.2.53")
	for _, change := range []func(api.Value) api.Value{
		smbChain(smbSpec("resourceRefs", api.StringList("smb-join")), smbNative("auth_mode", "active-directory", "user_group_settings", nil, "domain_settings", domain, "custom_dns", dns)),
		smbNative("custom_dns", dns),
	} {
		admitted, changed := smbWith(t, catalog, "smb-files", change)
		requireAdmits(t, admitted, changed)
	}
}

func TestSMBClusteringNeverRefusesMultipleDaemons(t *testing.T) {
	catalog := smbCatalog(t)
	label := func(extra ...any) api.Value { return m(append([]any{"label", "storage"}, extra...)...) }
	for _, row := range []struct {
		clustering string
		placement  api.Value
		field      string
		partial    bool
	}{
		{"never", label("count", 2), "clustering", true},
		{"never", label(), "clustering", false},
		{"default", label("count_per_host", 2), "placement.count_per_host", true},
	} {
		full, partial := smbBoth(t, catalog, "smb-files", smbNative("clustering", row.clustering, "placement", row.placement))
		if len(full) != 1 {
			t.Fatalf("%s: %#v", row.field, full)
		}
		message, remedy := smbClustering, smbClusteringFix
		if row.field != "clustering" {
			message, remedy = "one SMB daemon per host binds the SMB port", "omit placement.count_per_host or set it to 1"
		}
		exactIssue(t, full, "api.invariant", "$.spec.native."+row.field, message, remedy)
		if got := len(issuesAt(partial, "$.spec.native."+row.field)); got != map[bool]int{true: 1, false: 0}[row.partial] {
			t.Fatalf("%s partial: %#v", row.field, partial)
		}
	}
	for _, row := range []struct {
		clustering string
		placement  api.Value
	}{{"never", label("count", 1)}, {"default", label()}, {"always", label()}, {"never", m("hosts", api.StringList("node-2.storage.example.test"))}} {
		admitted, changed := smbWith(t, catalog, "smb-files", smbNative("clustering", row.clustering, "placement", row.placement))
		requireAdmits(t, admitted, changed)
	}
}

func TestSMBSecretLeavesAcceptOnlyQualifiedPathsAndTypes(t *testing.T) {
	catalog := smbCatalog(t)
	bind := func(paths ...string) func(api.Value) api.Value {
		secrets := []api.Value{}
		for _, path := range paths {
			secrets = append(secrets, m("path", path, "secretRef", "smb-pass"))
		}
		return smbSpec("secrets", secrets)
	}
	qualified := "secret leaf is not qualified for this SMB resource type"
	qualifiedFix := "bind /auth/password on a ceph.smb.join.auth resource or /values/users/<i>/password on a ceph.smb.usersgroups resource"
	for _, row := range []struct {
		name                string
		change              func(api.Value) api.Value
		field, message, fix string
	}{
		{"smb-join", bind("/auth/password", "/auth/username"), "$.spec.secrets[1].path", qualified, qualifiedFix},
		{"smb-join", bind("/auth/password", "/values/users/0/password"), "$.spec.secrets[1].path", qualified, qualifiedFix},
		{"smb-users", bind("/values/users/0/password", "/values/users/-/password"), "$.spec.secrets[1].path", "secret binding path must be a non-root exact JSON Pointer", ""},
		{"smb-users", bind("/values/users/0/password", "/values/users/0/password"), "$.spec.secrets[1].path", "secret binding paths cannot duplicate or overlap", ""},
		{"smb-users", bind("/values/users/0/password", "/values/users/1/password"), "$.spec.secrets[1].path", "this binding addresses no user of spec.native.values.users; a binding cannot add an array element", "bind the password of an existing user entry"},
		{"smb-join", smbNative("auth", m("username", "svc-join", "password", "s3cr3t")), "$.spec.native.auth.password", "an SMB password is never written in spec.native", "omit spec.native.auth.password and bind /auth/password through spec.secrets"},
		{"smb-users", smbNative("values", m("users", []api.Value{m("name", "alice", "password", "s3cr3t")}, "groups", []api.Value{})), "$.spec.native.values.users[0].password", "an SMB password is never written in spec.native", "omit spec.native.values.users[0].password and bind /values/users/0/password through spec.secrets"},
		{"smb-join", smbSpec("secrets", nil), "$.spec.secrets", "a ceph.smb.join.auth resource needs its /auth/password binding", "add a spec.secrets entry with path /auth/password and an opaque or token Secret"},
		{"smb-users", smbSpec("secrets", nil), "$.spec.native.values.users[0]", "each SMB user needs its password binding", "add a spec.secrets entry with path /values/users/0/password and an opaque or token Secret"},
		{"smb-files", bind("/auth/password"), "$.spec.secrets", "only ceph.smb.join.auth and ceph.smb.usersgroups resources take secret bindings", "remove spec.secrets"},
	} {
		full, _ := smbBoth(t, catalog, row.name, row.change)
		if len(full) != 1 {
			t.Fatalf("%s %s: %#v", row.name, row.field, full)
		}
		exactIssue(t, full, "api.invariant", row.field, row.message, row.fix)
		if strings.Contains(full[0].Message+full[0].Remediation, "s3cr3t") {
			t.Fatalf("an authored password escaped into %#v", full[0])
		}
	}
}

func TestSMBManagerModuleIsDerived(t *testing.T) {
	catalog := smbCatalog(t)
	cluster := find(t, catalog, api.CephCluster, "storage")
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.StringList("cephadm", "smb"), "mgrModules"))
	issues := Validate(cluster, replace(catalog, cluster))
	exactIssue(t, issues, "api.invariant", "$.spec.mgrModules[1]", "manager module smb is derived from the declared CephSMBResource clusters", "remove smb from spec.mgrModules; Bootwright enables it for a declared CephSMBResource cluster")
	if len(issuesAt(issues, "$.spec.mgrModules")) != 0 {
		t.Fatalf("smb fell to the generic module refusal: %#v", issues)
	}
	if !slices.Equal(DerivableManagerModules(), []string{"nfs"}) {
		t.Fatalf("derivable modules = %v", DerivableManagerModules())
	}
}

// A share's cluster_id and a cluster's resourceRefs have one derivation each,
// from the references the resource already authors, so omitting them is
// admitted and normalizes to the derived value, and an authored value that
// disagrees still refuses at its field (D183, Q83 b).
func TestSMBClusterIDAndResourceRefsAreDerived(t *testing.T) {
	catalog := smbCatalog(t)
	share, _ := smbWith(t, catalog, "smb-docs", smbNative("cluster_id", nil))
	derived, _ := Normalize(share, catalog)
	if got := derived.Spec().Get("native", "cluster_id").Text(); got != "files" {
		t.Fatalf("derived share cluster_id = %q, want files", got)
	}
	requireAdmits(t, derived, replace(catalog, derived))
	cluster, _ := smbWith(t, catalog, "smb-files", smbSpec("resourceRefs", nil))
	derived, _ = Normalize(cluster, catalog)
	if got := derived.Spec().Get("resourceRefs").Strings(); !slices.Equal(got, []string{"smb-users"}) {
		t.Fatalf("derived resourceRefs = %q, want [smb-users]", got)
	}
	requireAdmits(t, derived, replace(catalog, derived))
	disagreeing, changed := smbWith(t, catalog, "smb-docs", smbNative("cluster_id", "other"))
	requireIssue(t, Validate(disagreeing, changed), "$.spec.native.cluster_id")
	extra, changed := smbWith(t, catalog, "smb-files", smbSpec("resourceRefs", api.StringList("smb-users", "smb-join")))
	requireIssue(t, Validate(extra, changed), "$.spec.resourceRefs[1]")
}
