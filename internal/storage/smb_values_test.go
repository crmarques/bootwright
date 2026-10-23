package storage

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type smbRow struct {
	name                         string
	change                       func(api.Value) api.Value
	code, field, message, remedy string
}

func smbChain(changes ...func(api.Value) api.Value) func(api.Value) api.Value {
	return func(spec api.Value) api.Value {
		for _, change := range changes {
			spec = change(spec)
		}
		return spec
	}
}

func smbPasswords(count int) func(api.Value) api.Value {
	secrets := []api.Value{}
	for i := range count {
		secrets = append(secrets, m("path", "/values/users/"+string(rune('0'+i))+"/password", "secretRef", "smb-pass"))
	}
	return smbSpec("secrets", secrets)
}

func smbUsers(users, groups []api.Value) func(api.Value) api.Value {
	return smbNative("values", m("users", users, "groups", groups))
}

func names(values ...string) []api.Value {
	list := []api.Value{}
	for _, value := range values {
		list = append(list, m("name", value))
	}
	return list
}

func requireSMBRow(t *testing.T, catalog api.Catalog, row smbRow, partial bool) {
	t.Helper()
	full, fragment := smbBoth(t, catalog, row.name, row.change)
	if len(full) != 1 {
		t.Fatalf("%s %s: %#v", row.name, row.field, full)
	}
	exactIssue(t, full, row.code, row.field, row.message, row.remedy)
	if !partial {
		if len(fragment) != 0 {
			t.Fatalf("%s %s refused a fragment: %#v", row.name, row.field, fragment)
		}
		return
	}
	if len(fragment) != 1 {
		t.Fatalf("%s %s partial: %#v", row.name, row.field, fragment)
	}
	exactIssue(t, fragment, row.code, row.field, row.message, row.remedy)
}

func requireSMBAdmits(t *testing.T, catalog api.Catalog, name string, changes ...func(api.Value) api.Value) {
	t.Helper()
	for _, change := range changes {
		admitted, changed := smbWith(t, catalog, name, change)
		requireAdmits(t, admitted, changed)
		if issues := ValidatePartial(admitted, changed); len(issues) != 0 {
			t.Fatalf("admissible fragment %s refused: %#v", name, issues)
		}
	}
}

func TestSMBShareValuesFollowTheManagerGrammar(t *testing.T) {
	catalog := smbCatalog(t)
	shareName := []string{"an SMB share name starts with a letter, digit or underscore and holds at most 64 letters, digits, spaces, dots, underscores or hyphens", "rename the share within that grammar"}
	path := []string{"an SMB share path must be clean and absolute", "use one clean absolute path, / for the root of the backing"}
	provider := []string{"Bootwright does not realize SMB shares through this CephFS provider", "set spec.native.cephfs.provider to samba-vfs or omit it"}
	access := []string{"the SMB manager stores this access in its long spelling, which observation compares", "write read for r and read-write for rw"}
	login := func(entries ...api.Value) func(api.Value) api.Value {
		return smbNative("restrict_access", true, "login_control", entries)
	}
	rows := []smbRow{
		{"smb-docs", smbNative("name", ".x"), "api.value", "$.spec.native.name", shareName[0], shareName[1]},
		{"smb-docs", smbNative("name", strings.Repeat("a", 65)), "api.value", "$.spec.native.name", shareName[0], shareName[1]},
		{"smb-docs", smbNative("cephfs", m("path", "a/../b")), "api.value", "$.spec.native.cephfs.path", path[0], path[1]},
		{"smb-docs", smbNative("cephfs", m("path", "/a/../b")), "api.value", "$.spec.native.cephfs.path", path[0], path[1]},
		{"smb-docs", smbNative("cephfs", m("provider", "kcephfs")), "api.invariant", "$.spec.native.cephfs.provider", provider[0], provider[1]},
		{"smb-docs", smbNative("cephfs", m("provider", "samba-vfs/new")), "api.invariant", "$.spec.native.cephfs.provider", provider[0], provider[1]},
		{"smb-docs", login(m("name", "alice", "access", "rw")), "api.invariant", "$.spec.native.login_control[0].access", access[0], access[1]},
		{"smb-docs", login(m("name", "alice", "access", "r")), "api.invariant", "$.spec.native.login_control[0].access", access[0], access[1]},
		{"smb-docs", login(m("name", "al ice")), "api.value", "$.spec.native.login_control[0].name", smbLoginMessage, smbLoginRemedy},
		{"smb-docs", login(m("name", "alice"), m("name", "alice", "category", "user")), "api.invariant", "$.spec.native.login_control[1]", smbUniqueMessage, "remove the repeated entry"},
	}
	for _, row := range rows {
		requireSMBRow(t, catalog, row, true)
	}
	requireSMBAdmits(t, catalog, "smb-docs",
		smbNative("name", "Team docs.v2"),
		smbNative("name", "_"+strings.Repeat("a", 63)),
		smbNative("cephfs", m("path", "/", "provider", "samba-vfs")),
		login(m("name", "alice", "category", "user", "access", "read-write"), m("name", "alice", "category", "group", "access", "read")),
	)
}

func TestSMBUsersAndGroupsAreNamedOnce(t *testing.T) {
	catalog := smbCatalog(t)
	long := strings.Repeat("é", 129)
	rows := []smbRow{
		{"smb-users", smbChain(smbUsers([]api.Value{}, []api.Value{}), smbSpec("secrets", nil)), "api.invariant", "$.spec.native.values.users", "an SMB usersgroups resource declares at least one user", "add a users entry with a name, and bind its password"},
		{"smb-users", smbChain(smbUsers(names("alice", "alice"), []api.Value{}), smbPasswords(2)), "api.invariant", "$.spec.native.values.users[1]", smbUniqueMessage, "remove the repeated entry"},
		{"smb-users", smbUsers(names("alice"), names("staff", "staff")), "api.invariant", "$.spec.native.values.groups[1]", smbUniqueMessage, "remove the repeated entry"},
		{"smb-users", smbUsers(names("al\tice"), []api.Value{}), "api.value", "$.spec.native.values.users[0].name", smbLoginMessage, smbLoginRemedy},
		{"smb-users", smbUsers(names(long), []api.Value{}), "api.value", "$.spec.native.values.users[0].name", smbLoginMessage, smbLoginRemedy},
		{"smb-users", smbUsers(names("alice"), names("my staff")), "api.value", "$.spec.native.values.groups[0].name", smbLoginMessage, smbLoginRemedy},
	}
	for _, row := range rows {
		requireSMBRow(t, catalog, row, true)
	}
	requireSMBAdmits(t, catalog, "smb-users",
		smbUsers(names(strings.Repeat("é", 128)), names("staff", "alice")),
		smbChain(smbUsers(names("alice", "bob"), names("staff")), smbPasswords(2)),
	)
}

func TestSMBResourceIDsFollowTheManagerGrammar(t *testing.T) {
	catalog := smbCatalog(t)
	join := func(ref string) func(api.Value) api.Value {
		return smbChain(smbSpec("resourceRefs", api.StringList("smb-join")), smbNative("auth_mode", "active-directory", "user_group_settings", nil, "domain_settings", m("realm", "example.com", "join_sources", []api.Value{m("source_type", "resource", "ref", ref)})))
	}
	peer := func(name, key string) func(api.Catalog, string) api.Catalog {
		return func(catalog api.Catalog, id string) api.Catalog {
			_, changed := smbWith(t, catalog, name, smbNative(key, id))
			return changed
		}
	}
	settings := func(ref string) func(api.Value) api.Value {
		return smbNative("user_group_settings", []api.Value{m("source_type", "resource", "ref", ref)})
	}
	sites := []struct {
		name, field string
		change      func(string) func(api.Value) api.Value
		prepare     func(api.Catalog, string) api.Catalog
	}{
		{"smb-files", "$.spec.native.cluster_id", func(id string) func(api.Value) api.Value { return smbNative("cluster_id", id) }, nil},
		{"smb-docs", "$.spec.native.cluster_id", func(id string) func(api.Value) api.Value { return smbNative("cluster_id", id) }, peer("smb-files", "cluster_id")},
		{"smb-docs", "$.spec.native.share_id", func(id string) func(api.Value) api.Value { return smbNative("share_id", id) }, nil},
		{"smb-join", "$.spec.native.auth_id", func(id string) func(api.Value) api.Value { return smbNative("auth_id", id) }, nil},
		{"smb-users", "$.spec.native.users_groups_id", func(id string) func(api.Value) api.Value { return smbNative("users_groups_id", id) }, nil},
		{"smb-files", "$.spec.native.domain_settings.join_sources[0].ref", join, peer("smb-join", "auth_id")},
		{"smb-files", "$.spec.native.user_group_settings[0].ref", settings, peer("smb-users", "users_groups_id")},
	}
	for _, site := range sites {
		prepared := func(id string) api.Catalog {
			if site.prepare == nil {
				return catalog
			}
			return site.prepare(catalog, id)
		}
		for _, id := range []string{"x_y", strings.Repeat("a", 19), "-ab", "ab-"} {
			requireSMBRow(t, prepared(id), smbRow{site.name, site.change(id), "api.value", site.field, smbIDMessage, smbIDRemedy}, true)
		}
		for _, id := range []string{"a", "a-b", strings.Repeat("a", 18)} {
			requireSMBAdmits(t, prepared(id), site.name, site.change(id))
		}
	}
}

func TestSMBClusterRealmAndDNSAreLiteral(t *testing.T) {
	catalog := smbCatalog(t)
	ad := func(realm string, dns ...string) func(api.Value) api.Value {
		return smbChain(smbSpec("resourceRefs", api.StringList("smb-join")), smbNative("auth_mode", "active-directory", "user_group_settings", nil, "custom_dns", api.StringList(dns...), "domain_settings", m("realm", realm, "join_sources", []api.Value{m("source_type", "resource", "ref", "join")})))
	}
	dnsMessage, dnsRemedy := "a custom DNS server is a literal IP address", "write each custom_dns entry as an IP address"
	rows := []smbRow{
		{"smb-files", ad("ad realm", "192.0.2.53"), "api.value", "$.spec.native.domain_settings.realm", "an SMB realm is a DNS domain name", "write the Active Directory realm as its DNS domain name"},
		{"smb-files", smbNative("custom_dns", api.StringList("192.0.2.53", "dns.example")), "api.value", "$.spec.native.custom_dns[1]", dnsMessage, dnsRemedy},
		{"smb-files", ad("example.com", "fe80::1%eth0"), "api.value", "$.spec.native.custom_dns[0]", dnsMessage, dnsRemedy},
	}
	for _, row := range rows {
		requireSMBRow(t, catalog, row, true)
	}
	requireSMBAdmits(t, catalog, "smb-files", ad("AD.EXAMPLE.COM", "2001:db8::53"), smbNative("custom_dns", api.StringList("2001:db8::53", "192.0.2.53")))
}

func TestSMBEnvelopeFieldsBelongToTheirResourceType(t *testing.T) {
	catalog := smbCatalog(t)
	rows := []smbRow{
		{"smb-files", smbSpec("smbClusterRef", "smb-files"), "api.invariant", "$.spec.smbClusterRef", "only a ceph.smb.share resource names an SMB cluster", "remove spec.smbClusterRef"},
		{"smb-join", smbSpec("filesystemRef", "files"), "api.invariant", "$.spec.filesystemRef", "only a ceph.smb.share resource binds CephFS backing", "remove spec.filesystemRef and spec.subvolumeRef"},
		{"smb-users", smbSpec("subvolumeRef", "docs"), "api.invariant", "$.spec.subvolumeRef", "only a ceph.smb.share resource binds CephFS backing", "remove spec.filesystemRef and spec.subvolumeRef"},
		{"smb-docs", smbSpec("resourceRefs", api.StringList("smb-users")), "api.invariant", "$.spec.resourceRefs", "only a ceph.smb.cluster resource names auth resources", "remove spec.resourceRefs"},
		{"smb-docs", smbSpec("filesystemRef", "files"), "api.invariant", "$.spec", smbShareBacking, "keep only one of spec.filesystemRef and spec.subvolumeRef"},
	}
	for _, row := range rows {
		requireSMBRow(t, catalog, row, true)
	}
	fullOnly := []smbRow{
		{"smb-docs", smbSpec("smbClusterRef", nil), "api.required", "$.spec.smbClusterRef", "this declaration requires the named field", "supply $.spec.smbClusterRef using its documented type"},
		{"smb-docs", smbSpec("subvolumeRef", nil), "api.required", "$.spec", smbShareBacking, "set spec.filesystemRef or spec.subvolumeRef"},
		{"smb-docs", smbNative("restrict_access", true), "api.invariant", "$.spec.native.login_control", "a share with restrict_access needs at least one login_control entry", "add login_control entries, or set restrict_access: false"},
	}
	for _, row := range fullOnly {
		requireSMBRow(t, catalog, row, false)
	}
	requireSMBAdmits(t, catalog, "smb-docs", smbChain(smbSpec("subvolumeRef", nil), smbSpec("filesystemRef", "files")), smbNative("restrict_access", false))
}

func TestSMBClusterPlacementGrammarRefusesAFragment(t *testing.T) {
	catalog := smbCatalog(t)
	field := "$.spec.native.placement.host_pattern"
	message := "native host pattern must be a valid hostname glob"
	full, partial := smbBoth(t, catalog, "smb-files", smbNative("placement", m("host_pattern", "[", "count", 1)))
	exactIssue(t, full, "api.invariant", field, message, "set host_pattern to a valid hostname glob, such as node-*")
	if len(partial) != 1 {
		t.Fatalf("partial: %#v", partial)
	}
	exactIssue(t, partial, "api.invariant", field, message, "set host_pattern to a valid hostname glob, such as node-*")
	exclusive := "native hosts, label and host_pattern selectors are mutually exclusive"
	full, partial = smbBoth(t, catalog, "smb-files", smbNative("placement", m("hosts", api.StringList("node-1.storage.example.test"), "label", "storage", "count", 1)))
	exactIssue(t, full, "api.invariant", "$.spec.native.placement", exclusive, "")
	if len(partial) != 1 {
		t.Fatalf("partial: %#v", partial)
	}
	exactIssue(t, partial, "api.invariant", "$.spec.native.placement", exclusive, "")
}
