package v1alpha1

const (
	SMBCluster     = "ceph.smb.cluster"
	SMBShare       = "ceph.smb.share"
	SMBJoinAuth    = "ceph.smb.join.auth"
	SMBUsersGroups = "ceph.smb.usersgroups"
)

func storageSMBResourceSchema() *Shape {
	secrets := nonemptyArray(array(record(required("path", nonempty()), required("secretRef", secret("opaque", "token")))))
	secrets.NameKey = "path"
	return record(required("clusterRef", ref(CephCluster)), field("smbClusterRef", ref(CephSMBResource)), field("filesystemRef", ref(CephFilesystem)), field("subvolumeRef", ref(CephSubvolume)), field("resourceRefs", set(ref(CephSMBResource))), field("secrets", secrets), required("native", native()))
}

func StorageSMBResourceNativeSchema(resourceType string) *Shape {
	intent := func() *Shape { return enumeration("present", "removed") }
	member := func() *Shape { return record(required("name", nonempty())) }
	switch resourceType {
	case SMBCluster:
		join := record(field("source_type", enumeration("resource")), required("ref", nonempty()))
		source := record(field("source_type", enumeration("resource", "empty")), required("ref", nonempty()))
		return record(required("resource_type", enumeration(SMBCluster)), required("cluster_id", nonempty()), field("intent", intent()), required("auth_mode", enumeration("user", "active-directory")), field("domain_settings", record(required("realm", nonempty()), required("join_sources", array(join)))), field("user_group_settings", array(source)), field("custom_dns", set(nonempty())), required("placement", storageNativePlacement()), field("clustering", enumeration("default", "always", "never")), field("public_addrs", array(record(required("address", nonempty())))))
	case SMBShare:
		cephfs := record(field("volume", nonempty()), field("path", nonempty()), field("subvolumegroup", nonempty()), field("subvolume", nonempty()), field("provider", enumeration("samba-vfs", "samba-vfs/classic", "samba-vfs/new", "samba-vfs/proxied", "kcephfs")))
		login := record(required("name", nonempty()), field("category", enumeration("user", "group")), field("access", enumeration("read", "read-write", "admin", "none", "r", "rw")))
		return record(required("resource_type", enumeration(SMBShare)), field("cluster_id", nonempty()), required("share_id", nonempty()), field("intent", intent()), field("name", nonempty()), field("readonly", boolean()), field("browseable", boolean()), field("cephfs", cephfs), field("restrict_access", boolean()), field("login_control", array(login)))
	case SMBJoinAuth:
		return record(required("resource_type", enumeration(SMBJoinAuth)), required("auth_id", nonempty()), field("intent", intent()), required("auth", record(required("username", nonempty()))))
	case SMBUsersGroups:
		return record(required("resource_type", enumeration(SMBUsersGroups)), required("users_groups_id", nonempty()), field("intent", intent()), required("values", record(required("users", array(member())), required("groups", array(member())))))
	}
	return nil
}
