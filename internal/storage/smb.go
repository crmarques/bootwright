package storage

import (
	"math/big"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type smbRefusal struct {
	resourceTypes   []string
	path            []string
	message, remedy string
}

const (
	smbPassword        = "an SMB password is never written in spec.native"
	smbLinked          = "Bootwright does not realize linked SMB auth resources; each StorageSMBResource applies and removes its own native resource"
	smbLinkedRemedy    = "omit spec.native.linked_to_cluster; the SMB cluster names this resource in its spec.resourceRefs"
	smbTypes           = "ceph.smb.cluster, ceph.smb.share, ceph.smb.join.auth and ceph.smb.usersgroups"
	smbShareBacking    = "an SMB share requires exactly one of spec.filesystemRef and spec.subvolumeRef"
	smbClustering      = "SMB daemons on more than one host without clustering keep separate lock and session state"
	smbClusteringFix   = "set clustering to default or always, or set placement.count: 1"
	smbIDMessage       = "an SMB resource ID is 1 to 18 letters, digits or inner hyphens"
	smbIDRemedy        = "use letters, digits and hyphens, starting and ending with a letter or digit, at most 18 characters"
	smbLoginMessage    = "an SMB login name holds no space, tab or newline and at most 128 characters"
	smbLoginRemedy     = "write the name without whitespace and within 128 characters"
	smbUniqueMessage   = "SMB user, group and login names must be unique in their list"
	smbADSettings      = "an active-directory SMB cluster needs domain_settings with its realm and join sources"
	smbADSettingsFix   = "add domain_settings.realm and a join_sources entry whose ref is the auth_id of a ceph.smb.join.auth resource named in spec.resourceRefs"
	smbJoinPassword    = "/auth/password"
	smbSecretQualified = "bind /auth/password on a ceph.smb.join.auth resource or /values/users/<i>/password on a ceph.smb.usersgroups resource"
)

var (
	smbResourceTypes  = []string{api.SMBCluster, api.SMBShare, api.SMBJoinAuth, api.SMBUsersGroups}
	smbRefusedMembers = []smbRefusal{
		{[]string{api.SMBCluster}, []string{"custom_smb_global_options"}, "Bootwright does not realize custom Samba global options", "omit spec.native.custom_smb_global_options"},
		{[]string{api.SMBCluster}, []string{"client_compat"}, "Bootwright does not realize client_compat", "omit spec.native.client_compat"},
		{[]string{api.SMBCluster}, []string{"custom_ports"}, "Bootwright does not realize custom SMB ports", "omit spec.native.custom_ports"},
		{[]string{api.SMBCluster}, []string{"bind_addrs"}, "Bootwright does not realize SMB bind addresses", "omit spec.native.bind_addrs"},
		{[]string{api.SMBCluster}, []string{"remote_control"}, "Bootwright does not realize the SMB remote control service", "omit spec.native.remote_control"},
		{[]string{api.SMBShare}, []string{"comment"}, "Bootwright does not realize SMB share comments", "omit spec.native.comment"},
		{[]string{api.SMBShare}, []string{"max_connections"}, "Bootwright does not realize SMB share connection limits", "omit spec.native.max_connections"},
		{[]string{api.SMBShare}, []string{"custom_smb_share_options"}, "Bootwright does not realize custom Samba share options", "omit spec.native.custom_smb_share_options"},
		{[]string{api.SMBJoinAuth, api.SMBUsersGroups}, []string{"linked_to_cluster"}, smbLinked, smbLinkedRemedy},
		{[]string{api.SMBJoinAuth}, []string{"auth", "password"}, smbPassword, "omit spec.native.auth.password and bind /auth/password through spec.secrets"},
	}
)

func smbType(object api.Object) string { return textOf(object.Spec(), "native", "resource_type") }

func (a *admission) smbResourceType(native api.Value) (string, bool) {
	value := native.Get("resource_type")
	switch {
	case !value.Present():
		a.required(native, "resource_type", "$.spec.native")
	case value.Type() != api.String:
		a.issue("api.type", "$.spec.native.resource_type", "native value has the wrong type", "use the qualified native field type at $.spec.native.resource_type")
	case value.Text() == "ceph.smb.tls.credential":
		a.issue("api.invariant", "$.spec.native.resource_type", "Bootwright does not realize ceph.smb.tls.credential resources", "remove this StorageSMBResource")
	case !slices.Contains(smbResourceTypes, value.Text()):
		a.issue("api.invariant", "$.spec.native.resource_type", "native SMB resource type is not one of "+smbTypes, "set spec.native.resource_type to one of "+smbTypes)
	default:
		return value.Text(), true
	}
	return "", false
}

func (a *admission) withoutRefusedSMBMembers(native api.Value, rt string) api.Value {
	for _, row := range smbRefusedMembers {
		parent := native.Get(row.path[:len(row.path)-1]...)
		leaf := row.path[len(row.path)-1]
		if !slices.Contains(row.resourceTypes, rt) || parent.Type() != api.Mapping || !parent.Has(leaf) {
			continue
		}
		a.issue("api.invariant", "$.spec.native."+strings.Join(row.path, "."), row.message, row.remedy)
		native = native.WithPath(parent.Without(leaf), row.path[:len(row.path)-1]...)
	}
	switch rt {
	case api.SMBCluster:
		native = a.withoutIndexedMember(native, []string{"public_addrs"}, "destination", func(i int) (string, string) {
			return "Bootwright does not realize public address destinations", "omit destination from spec.native.public_addrs[" + strconv.Itoa(i) + "]"
		})
	case api.SMBUsersGroups:
		native = a.withoutIndexedMember(native, []string{"values", "users"}, "password", func(i int) (string, string) {
			index := strconv.Itoa(i)
			return smbPassword, "omit spec.native.values.users[" + index + "].password and bind /values/users/" + index + "/password through spec.secrets"
		})
	}
	return native
}

func (a *admission) withoutIndexedMember(native api.Value, path []string, member string, texts func(int) (string, string)) api.Value {
	list := native.Get(path...)
	if list.Type() != api.Sequence {
		return native
	}
	items := list.Items()
	for i, item := range items {
		if item.Type() == api.Mapping && item.Has(member) {
			message, remedy := texts(i)
			a.issue("api.invariant", indexed("$.spec.native."+strings.Join(path, "."), i)+"."+member, message, remedy)
			items[i] = item.Without(member)
		}
	}
	return native.WithPath(api.ListValue(items...), path...)
}

func (a *admission) smbIntrinsic() {
	spec := a.object.Spec()
	native := spec.Get("native")
	if native.Type() != api.Mapping {
		return
	}
	rt, ok := a.smbResourceType(native)
	if !ok {
		return
	}
	native = a.withoutRefusedSMBMembers(native, rt)
	a.nativeShape(native, api.StorageSMBResourceNativeSchema(rt), "$.spec.native", a.partial)
	a.smbEnvelope(spec, rt)
	if native.Get("intent").Text() == "removed" {
		a.issue("api.invariant", "$.spec.native.intent", "native intent removed is not apply-time removal authorization; Bootwright removes an SMB resource only through its removal lifecycle", "omit spec.native.intent or set it to present")
	}
	switch rt {
	case api.SMBCluster:
		a.smbClusterValues(native)
		if placement := native.Get("placement"); a.partial && placement.Type() == api.Mapping {
			a.nativePlacement(api.Object{}, placement, "$.spec.native.placement", true)
		}
	case api.SMBShare:
		a.smbShareValues(native)
	case api.SMBJoinAuth:
		a.smbID(native.Get("auth_id"), "$.spec.native.auth_id")
	case api.SMBUsersGroups:
		a.smbID(native.Get("users_groups_id"), "$.spec.native.users_groups_id")
		users := native.Get("values", "users")
		if users.Type() == api.Sequence && users.Len() == 0 {
			a.issue("api.invariant", "$.spec.native.values.users", "an SMB usersgroups resource declares at least one user", "add a users entry with a name, and bind its password")
		}
		a.smbNames(users, "$.spec.native.values.users", false)
		a.smbNames(native.Get("values", "groups"), "$.spec.native.values.groups", false)
	}
	if rt == api.SMBJoinAuth || rt == api.SMBUsersGroups {
		a.smbSecretLeaves(spec.Get("secrets"), rt)
	}
}

func (a *admission) smbEnvelope(spec api.Value, rt string) {
	if rt != api.SMBShare {
		if spec.Has("smbClusterRef") {
			a.issue("api.invariant", "$.spec.smbClusterRef", "only a ceph.smb.share resource names an SMB cluster", "remove spec.smbClusterRef")
		}
		for _, key := range []string{"filesystemRef", "subvolumeRef"} {
			if spec.Has(key) {
				a.issue("api.invariant", "$.spec."+key, "only a ceph.smb.share resource binds CephFS backing", "remove spec.filesystemRef and spec.subvolumeRef")
			}
		}
	} else if spec.Has("filesystemRef") && spec.Has("subvolumeRef") {
		a.issue("api.invariant", "$.spec", smbShareBacking, "keep only one of spec.filesystemRef and spec.subvolumeRef")
	}
	if rt != api.SMBCluster && spec.Has("resourceRefs") {
		a.issue("api.invariant", "$.spec.resourceRefs", "only a ceph.smb.cluster resource names auth resources", "remove spec.resourceRefs")
	}
	if rt != api.SMBJoinAuth && rt != api.SMBUsersGroups && spec.Has("secrets") {
		a.issue("api.invariant", "$.spec.secrets", "only ceph.smb.join.auth and ceph.smb.usersgroups resources take secret bindings", "remove spec.secrets")
	}
}

func (a *admission) smbID(value api.Value, field string) {
	if value.Type() == api.String && !smbID.MatchString(value.Text()) {
		a.issue("api.value", field, smbIDMessage, smbIDRemedy)
	}
}

func (a *admission) smbClusterValues(native api.Value) {
	a.smbID(native.Get("cluster_id"), "$.spec.native.cluster_id")
	mode := textOf(native, "auth_mode")
	if mode == "user" && native.Has("domain_settings") {
		a.issue("api.invariant", "$.spec.native.domain_settings", "a user-mode SMB cluster takes no domain_settings", "remove domain_settings, or set auth_mode: active-directory")
	}
	if mode == "active-directory" && native.Has("user_group_settings") {
		a.issue("api.invariant", "$.spec.native.user_group_settings", "an active-directory SMB cluster takes no user_group_settings", "remove user_group_settings, or set auth_mode: user")
	}
	if realm := native.Get("domain_settings", "realm"); realm.Type() == api.String && !api.ValidLexical("dns", strings.ToLower(realm.Text())) {
		a.issue("api.value", "$.spec.native.domain_settings.realm", "an SMB realm is a DNS domain name", "write the Active Directory realm as its DNS domain name")
	}
	for i, source := range native.Get("domain_settings", "join_sources").Items() {
		a.smbID(source.Get("ref"), indexed("$.spec.native.domain_settings.join_sources", i)+".ref")
	}
	for i, source := range native.Get("user_group_settings").Items() {
		field := indexed("$.spec.native.user_group_settings", i)
		a.smbID(source.Get("ref"), field+".ref")
		if textOf(source, "source_type") == "empty" {
			a.issue("api.invariant", field+".source_type", "Bootwright does not realize user-mode SMB clusters without a users-and-groups resource", "set source_type: resource and ref to the users_groups_id of a ceph.smb.usersgroups resource in spec.resourceRefs")
		}
	}
	for i, server := range native.Get("custom_dns").Items() {
		if address, err := netip.ParseAddr(server.Text()); server.Type() == api.String && (err != nil || address.Zone() != "") {
			a.issue("api.value", indexed("$.spec.native.custom_dns", i), "a custom DNS server is a literal IP address", "write each custom_dns entry as an IP address")
		}
	}
	placement := native.Get("placement")
	if perHost := placement.Get("count_per_host"); perHost.Type() == api.Integer && numeric(perHost).Cmp(big.NewInt(1)) > 0 {
		a.issue("api.invariant", "$.spec.native.placement.count_per_host", "one SMB daemon per host binds the SMB port", "omit placement.count_per_host or set it to 1")
	}
	if count := placement.Get("count"); textOf(native, "clustering") == "never" && count.Type() == api.Integer && numeric(count).Cmp(big.NewInt(1)) > 0 {
		a.issue("api.invariant", "$.spec.native.clustering", smbClustering, smbClusteringFix)
	}
}

func (a *admission) smbShareValues(native api.Value) {
	a.smbID(native.Get("cluster_id"), "$.spec.native.cluster_id")
	a.smbID(native.Get("share_id"), "$.spec.native.share_id")
	if name := native.Get("name"); name.Type() == api.String && !smbShareName.MatchString(name.Text()) {
		a.issue("api.value", "$.spec.native.name", "an SMB share name starts with a letter, digit or underscore and holds at most 64 letters, digits, spaces, dots, underscores or hyphens", "rename the share within that grammar")
	}
	if path := native.Get("cephfs", "path"); path.Type() == api.String && !cleanAbsolute(path.Text()) {
		a.issue("api.value", "$.spec.native.cephfs.path", "an SMB share path must be clean and absolute", "use one clean absolute path, / for the root of the backing")
	}
	if provider := textOf(native, "cephfs", "provider"); slices.Contains([]string{"samba-vfs/classic", "samba-vfs/new", "samba-vfs/proxied", "kcephfs"}, provider) {
		a.issue("api.invariant", "$.spec.native.cephfs.provider", "Bootwright does not realize SMB shares through this CephFS provider", "set spec.native.cephfs.provider to samba-vfs or omit it")
	}
	logins := native.Get("login_control")
	for i, entry := range logins.Items() {
		if access := textOf(entry, "access"); access == "r" || access == "rw" {
			a.issue("api.invariant", indexed("$.spec.native.login_control", i)+".access", "the SMB manager stores this access in its long spelling, which observation compares", "write read for r and read-write for rw")
		}
	}
	a.smbNames(logins, "$.spec.native.login_control", true)
}

func (a *admission) smbNames(list api.Value, field string, categorized bool) {
	seen := map[string]bool{}
	for i, entry := range list.Items() {
		name := entry.Get("name")
		if name.Type() != api.String {
			continue
		}
		if !validSMBLoginName(name.Text()) {
			a.issue("api.value", indexed(field, i)+".name", smbLoginMessage, smbLoginRemedy)
		}
		key := name.Text()
		if categorized {
			key = fallback(entry.Get("category"), "user") + "\x00" + key
		}
		if seen[key] {
			a.issue("api.invariant", indexed(field, i), smbUniqueMessage, "remove the repeated entry")
		}
		seen[key] = true
	}
}

func (a *admission) smbSecretLeaves(secrets api.Value, rt string) {
	paths := []string{}
	for j, binding := range secrets.Items() {
		pointer := textOf(binding, "path")
		leaf := indexed("$.spec.secrets", j) + ".path"
		valid := validSecretPointer(pointer)
		if !valid {
			a.invalid(leaf, "secret binding path must be a non-root exact JSON Pointer")
		}
		for _, earlier := range paths {
			if earlier == pointer || strings.HasPrefix(earlier, pointer+"/") || strings.HasPrefix(pointer, earlier+"/") {
				a.invalid(leaf, "secret binding paths cannot duplicate or overlap")
			}
		}
		paths = append(paths, pointer)
		if valid && (rt == api.SMBJoinAuth && pointer != smbJoinPassword || rt == api.SMBUsersGroups && !smbUserPassword.MatchString(pointer)) {
			a.issue("api.invariant", leaf, "secret leaf is not qualified for this SMB resource type", smbSecretQualified)
		}
	}
}

func (a *admission) smbResource(cluster api.Object) {
	spec := a.object.Spec()
	native := spec.Get("native")
	rt := smbType(a.object)
	switch rt {
	case api.SMBShare:
		a.required(spec, "smbClusterRef", "$.spec")
		if !spec.Has("filesystemRef") && !spec.Has("subvolumeRef") {
			a.issue("api.required", "$.spec", smbShareBacking, "set spec.filesystemRef or spec.subvolumeRef")
		}
		if native.Get("restrict_access").Bool() && native.Get("login_control").Len() == 0 {
			a.issue("api.invariant", "$.spec.native.login_control", "a share with restrict_access needs at least one login_control entry", "add login_control entries, or set restrict_access: false")
		}
	case api.SMBCluster:
		a.smbClusterSettings(native)
		a.smbClusterPlacement(cluster, native)
	case api.SMBJoinAuth:
		if !slices.Contains(smbBoundPaths(spec), smbJoinPassword) {
			a.issue("api.invariant", "$.spec.secrets", "a ceph.smb.join.auth resource needs its /auth/password binding", "add a spec.secrets entry with path /auth/password and an opaque or token Secret")
		}
	case api.SMBUsersGroups:
		a.smbUserBindings(spec, native.Get("values", "users"))
	}
}

func (a *admission) smbClusterSettings(native api.Value) {
	switch textOf(native, "auth_mode") {
	case "user":
		if settings := native.Get("user_group_settings"); !settings.Present() || (settings.Type() == api.Sequence && settings.Len() == 0) {
			a.issue("api.invariant", "$.spec.native.user_group_settings", "a user-mode SMB cluster needs user_group_settings", "add a user_group_settings entry whose ref is the users_groups_id of a ceph.smb.usersgroups resource named in spec.resourceRefs")
		}
	case "active-directory":
		if !native.Has("domain_settings") {
			a.issue("api.invariant", "$.spec.native.domain_settings", smbADSettings, smbADSettingsFix)
		} else if sources := native.Get("domain_settings", "join_sources"); sources.Type() == api.Sequence && sources.Len() == 0 {
			a.issue("api.invariant", "$.spec.native.domain_settings.join_sources", smbADSettings, smbADSettingsFix)
		}
	}
}

func (a *admission) smbClusterPlacement(cluster api.Object, native api.Value) {
	placement := native.Get("placement")
	if placement.Type() != api.Mapping {
		return
	}
	eligible := a.nativePlacement(cluster, placement, "$.spec.native.placement", false)
	if textOf(native, "clustering") == "never" && !placement.Has("count") && len(eligible) > 1 {
		a.issue("api.invariant", "$.spec.native.clustering", smbClustering, smbClusteringFix)
	}
}

func smbBoundPaths(spec api.Value) []string {
	paths := []string{}
	for _, binding := range spec.Get("secrets").Items() {
		paths = append(paths, textOf(binding, "path"))
	}
	return paths
}

func (a *admission) smbUserBindings(spec api.Value, users api.Value) {
	if users.Type() != api.Sequence {
		return
	}
	bound := smbBoundPaths(spec)
	for i := range users.Items() {
		index := strconv.Itoa(i)
		if !slices.Contains(bound, "/values/users/"+index+"/password") {
			a.issue("api.invariant", indexed("$.spec.native.values.users", i), "each SMB user needs its password binding", "add a spec.secrets entry with path /values/users/"+index+"/password and an opaque or token Secret")
		}
	}
	for j, path := range bound {
		match := smbUserPassword.FindStringSubmatch(path)
		if match == nil {
			continue
		}
		if index, err := strconv.Atoi(match[1]); err != nil || index >= users.Len() {
			a.issue("api.invariant", indexed("$.spec.secrets", j)+".path", "this binding addresses no user of spec.native.values.users; a binding cannot add an array element", "bind the password of an existing user entry")
		}
	}
}
