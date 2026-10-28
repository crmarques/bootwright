package v1alpha1

import "slices"

func init() {
	register(CephCluster, storageClusterSchema)
	register(CephPlacementPolicy, storagePlacementPolicySchema)
	register(CephPool, storagePoolSchema)
	register(CephReplication, storageReplicationSchema)
	register(CephRBDImage, storageRBDImageSchema)
	register(CephFilesystem, storageFilesystemSchema)
	register(CephSubvolume, storageSubvolumeSchema)
	register(CephObjectGateway, storageObjectGatewaySchema)
	register(CephObjectUser, storageObjectUserSchema)
	register(CephObjectBucket, storageObjectBucketSchema)
	register(CephNFSExport, storageNFSExportSchema)
	register(CephSMBResource, storageSMBResourceSchema)
	register(CephNVMeoFSubsystem, storageNVMeoFSubsystemSchema)
	register(CephConsumer, cephConsumerSchema)
	register(CephClient, storageClientSchema)
}

func storagePlacement() *Shape {
	return record(field("hosts", set(nonempty())), field("sites", set(name())), field("countPerHost", integer("0", "")))
}

func storageReplicated() *Shape {
	return record(field("size", integer("0", "")), field("minSize", integer("0", "")))
}

func storageDeviceSelector() *Shape {
	return record(field("paths", set(lexical("device-path"))), field("pathSpecs", array(record(required("path", lexical("device-path")), field("crushDeviceClass", nonempty())))), field("all", boolean()), field("model", nonempty()), field("vendor", nonempty()), field("rotational", boolean()), field("size", nonempty()), field("limit", integer("0", "")))
}

func storageServiceSpec(custom bool) *Shape {
	s := record(field("unmanaged", boolean()), field("extraContainerArgs", array(text())), field("extraEntrypointArgs", array(text())), field("networks", set(cidr())))
	if custom {
		s.Fields = s.Fields[1:]
		s.Fields = append(s.Fields, field("customConfigs", array(record(required("mountPath", lexical("absolute-path")), required("content", nonempty())))))
	}
	return s
}

func storageOSD() *Shape {
	return record(required("dataDevices", storageDeviceSelector()), field("dbDevices", storageDeviceSelector()), field("walDevices", storageDeviceSelector()), field("encrypted", boolean()), field("osdsPerDevice", integer("0", "")), field("crushDeviceClass", nonempty()), field("filterLogic", enumeration("AND", "OR")), field("blockDBSize", nonempty()), field("blockWALSize", nonempty()), field("dbSlots", integer("0", "")), field("walSlots", integer("0", "")), field("dataAllocateFraction", number("0", "1")), field("tpm2", boolean()), field("unmanaged", boolean()), field("serviceOverrides", storageServiceSpec(true)))
}

func storageMonitoringService(kind string) *Shape {
	s := record(field("placement", storagePlacement()), field("port", integer("0", "65535")))
	if kind == "prometheus" {
		s.Fields = append(s.Fields, field("retentionTime", nonempty()), field("retentionSize", nonempty()))
	}
	s.Fields = append(s.Fields, field("networks", set(cidr())))
	if kind == "grafana" {
		s.Fields = append(s.Fields, field("initialAdminPasswordRef", secret("opaque", "token")))
	}
	return s
}

var cephAuxiliaryImageNames = []string{"alertmanager", "alloy", "grafana", "haproxy", "keepalived", "loki", "nginx", "node_exporter", "nvmeof",
	"oauth2_proxy", "prometheus", "promtail", "samba", "samba_metrics", "snmp_gateway"}

func cephAuxiliaryImages() *Shape {
	s := record()
	for _, name := range cephAuxiliaryImageNames {
		s.Fields = append(s.Fields, field(name, record(field("base", lexical("registry-base")), field("version", lexical("native-version")))))
	}
	return s
}

var cephManagedFields = []string{"distribution", "release", "packageVersion", "packageRepository", "packageRepositoryTrustBundleRef", "packageRepositoryKey", "nfsGanesha", "image", "images",
	"entitlementRef", "cephadm", "networks", "security", "config", "mgrModules", "services", "serviceBindings", "topology"}

func CephManagedFields() []string { return slices.Clone(cephManagedFields) }

var cephDistributions = []string{"oss", "redhat", "ibm"}

func CephDistribution(spec Value) string {
	for _, arm := range cephDistributions {
		if spec.Has("distribution", arm) {
			return arm
		}
	}
	return ""
}

func cephDistributionSchema() *Shape {
	return choice(field("oss", record(field("mirror", lexical("https-url")), field("checksum", lexical("checksum")))), field("redhat", record()),
		field("ibm", record(required("callHome", enumeration("enabled", "disabled")), field("packages", record(required("source", enumeration("vendor")))))))
}

func storageClusterSchema() *Shape {
	configValues := stringsMap()
	configValues.Element = nonempty()
	config := &Shape{Type: Mapping, Open: true, Element: configValues, Atomic: true}
	s := record(
		defaulted("management", enumeration("managed", "external"), StringValue("managed")),
		field("distribution", cephDistributionSchema()), field("release", lexical("native-version")), field("packageVersion", lexical("native-version")), field("packageRepository", lexical("package-repository")), field("packageRepositoryTrustBundleRef", secret("caBundle")), field("packageRepositoryKey", lexical("package-repository-key")), field("nfsGanesha", record(required("packageVersion", lexical("native-version")))), field("image", record(field("base", lexical("registry-base")), field("version", lexical("native-version")))), field("images", cephAuxiliaryImages()),
		field("entitlementRef", ref(Entitlement)),
		field("cephadm", record(field("addressRef", nestedIn(Scope{Noun: "address", Each: []string{"topology", "nodes"}, Owner: []string{"machineRef"}, Kind: Machine, Entries: []string{"network", "addresses"}})), field("ansible", record(field("packageVersion", lexical("native-version")))), field("clusterSSH", record(field("user", lexical("posix-user")), field("keyRef", secret("sshKeyPair")), field("transport", enumeration("native", "verifiedTunnel")))), required("bootstrap", record(required("nodeRef", nestedIn(Scope{Noun: "topology node", Entries: []string{"topology", "nodes"}})), field("addressRef", nestedIn(Scope{Noun: "address", Each: []string{"topology", "nodes"}, Select: []string{"cephadm", "bootstrap", "nodeRef"}, Owner: []string{"machineRef"}, Kind: Machine, Entries: []string{"network", "addresses"}})), field("singleHostDefaults", boolean()))))),
		field("networks", record(field("publicCIDRs", set(cidr())), field("clusterCIDRs", set(cidr())))), field("security", record(field("fips", record(field("enabled", boolean()))), field("cephx", record(required("keyType", enumeration("aes", "aes256k")))))), field("config", config), field("mgrModules", set(nonempty())), field("services", nonemptyArray(array(storageNativeService()))), field("serviceBindings", storageServiceBindings()),
		field("topology", record(required("nodes", nonemptyArray(named(record(required("name", name()), field("fqdn", dns()), required("machineRef", ref(Machine)), field("site", name()), field("labels", set(nonempty())), field("location", stringsMap()))))), field("stretch", record(required("failureDomain", nonempty()), field("dataSites", set(name())), field("tiebreaker", record(field("node", text()), field("site", text()))), defaulted("ruleName", nonempty(), StringValue("stretch-rule")))))),
	)
	s.Suppress = []Suppression{{Field: "distribution.oss", Fields: []string{"entitlementRef", "packageVersion"}},
		{Field: "management", Value: StringValue("external"), Fields: CephManagedFields()}}
	return s
}

func storageClientSchema() *Shape {
	return record(required("clusterRef", ref(CephCluster)), field("poolRef", ref(CephPool)), field("filesystemRef", ref(CephFilesystem)),
		field("subvolumeRef", ref(CephSubvolume)), required("access", enumeration("readOnly", "readWrite")))
}

func storageRBDImageSchema() *Shape {
	return record(required("clusterRef", ref(CephCluster)), required("poolRef", ref(CephPool)), required("sizeBytes", integer("1", "9223372036854775807")), field("dataPoolRef", ref(CephPool)), field("features", set(enumeration("layering", "exclusive-lock", "object-map", "fast-diff", "deep-flatten", "journaling"))))
}

func storagePlacementPolicySchema() *Shape {
	return record(required("clusterRef", ref(CephCluster)), field("failureDomain", nonempty()), required("ruleName", nonempty()), field("crushDeviceClass", nonempty()), field("replicated", storageReplicated()))
}

func storagePoolSchema() *Shape {
	erasure := record(required("dataChunks", integer("1", "")), required("codingChunks", integer("1", "")), field("plugin", enumeration("", "jerasure", "isa", "clay", "lrc", "shec")), field("technique", nonempty()), field("crushDeviceClass", nonempty()), field("crushRoot", nonempty()), field("stripeUnit", integer("1", "")), field("parameters", stringsMap()))
	s := record(required("clusterRef", ref(CephCluster)), field("nativeName", name()), field("placementPolicyRef", ref(CephPlacementPolicy)), field("role", enumeration("rbd", "cephfsMetadata", "cephfsData", "rgw")), field("application", nonempty()), defaulted("replicated", storageReplicated(), MapValue()), field("erasure", erasure), field("autoscale", record(field("mode", enumeration("on", "off", "warn")), field("targetSizeRatio", number("0", "")), field("targetSizeBytes", nonempty()), field("pgNumMin", integer("0", "")), field("pgNumMax", integer("0", "")), field("bulk", boolean()))), field("quota", record(field("maxBytes", integer("0", "9223372036854775807")), field("maxObjects", integer("0", "9223372036854775807")))), field("compression", record(field("mode", enumeration("none", "passive", "aggressive", "force")), field("algorithm", enumeration("", "lz4", "snappy", "zlib", "zstd")), field("requiredRatio", number("0", "1")), field("minBlobSize", integer("0", "18446744073709551615")), field("maxBlobSize", integer("0", "18446744073709551615")))), field("mirroring", record(required("mode", enumeration("image", "pool")))))
	s.Arms = []string{"replicated", "erasure"}
	return s
}

func storageFilesystemSchema() *Shape {
	dataPool := &Shape{Alternatives: []*Shape{ref(CephPool), record(required("name", ref(CephPool)), field("default", boolean()))}}
	mds := record(field("activeCount", integer("1", "256")), field("standbyReplay", boolean()), field("standbyCountWanted", integer("0", "2147483647")))
	group := record(required("name", nonempty()), field("poolLayoutRef", ref(CephPool)), field("mode", nonempty()), field("uid", integer("0", "2147483647")), field("gid", integer("0", "2147483647")), field("sizeBytes", integer("1", "9223372036854775807")))
	return record(required("clusterRef", ref(CephCluster)), field("mdsServiceRef", nativeServiceOf("clusterRef")), required("metadataPoolRef", ref(CephPool)), required("dataPoolRefs", nonemptyArray(array(dataPool))), field("mds", mds), field("subvolumeGroups", named(group)))
}

func storageSubvolumeSchema() *Shape {
	return record(required("clusterRef", ref(CephCluster)), required("filesystemRef", ref(CephFilesystem)), field("group", nonempty()), field("sizeBytes", integer("1", "9223372036854775807")), field("poolLayoutRef", ref(CephPool)), field("uid", integer("0", "2147483647")), field("gid", integer("0", "2147483647")), field("mode", nonempty()))
}

func storageObjectGatewaySchema() *Shape {
	return record(required("clusterRef", ref(CephCluster)), required("serviceRef", nativeServiceOf("clusterRef")), required("realm", name()), required("zoneGroup", name()), required("zone", name()), field("pools", record(field("indexPoolRef", ref(CephPool)), field("dataPoolRef", ref(CephPool)), field("dataExtraPoolRef", ref(CephPool)))), field("endpoint", record(field("fqdn", dns()))))
}

func storageObjectUserSchema() *Shape {
	return record(required("clusterRef", ref(CephCluster)), required("objectGatewayRef", ref(CephObjectGateway)), field("displayName", nonempty()))
}

func storageObjectBucketSchema() *Shape {
	return record(required("clusterRef", ref(CephCluster)), required("objectGatewayRef", ref(CephObjectGateway)), required("ownerRef", ref(CephObjectUser)), field("policy", native()))
}

func storageNFSExportSchema() *Shape {
	return record(required("clusterRef", ref(CephCluster)), required("serviceID", nonempty()), field("filesystemRef", ref(CephFilesystem)), field("subvolumeRef", ref(CephSubvolume)), field("objectUserRef", ref(CephObjectUser)), required("native", native()))
}

func StorageNFSExportNativeSchema() *Shape {
	access := func() *Shape { return enumeration("RW", "RO", "NONE") }
	squash := func() *Shape {
		return enumeration("root", "root_squash", "rootsquash", "rootid", "root_id_squash", "rootidsquash", "all", "all_squash", "allsquash", "all_anomnymous", "allanonymous", "no_root_squash", "none", "noidsquash")
	}
	fsal := record(required("name", enumeration("CEPH", "RGW")), field("fs_name", nonempty()), field("cmount_path", nonempty()))
	client := record(required("addresses", set(nonempty())), required("access_type", access()), required("squash", squash()))
	return record(required("pseudo", nonempty()), field("path", nonempty()), field("access_type", access()), field("squash", squash()), field("security_label", boolean()), field("protocols", set(integer("3", "4"))), field("transports", set(enumeration("TCP", "UDP"))), field("sectype", set(enumeration("sys", "krb5", "krb5i", "krb5p"))), required("fsal", fsal), field("clients", array(client)))
}

func cephConsumerSchema() *Shape {
	return record(required("clusterRef", ref(CephCluster)), field("dataFoundation", record(required("rbdPoolRef", ref(CephPool)), required("filesystemRef", ref(CephFilesystem)), field("objectGatewayRef", ref(CephObjectGateway)), field("cephxKeyType", enumeration("aes", "aes256k")))), field("externalDetails", record(required("fromSecretRef", secret("opaque")))))
}

func storageReplicationSchema() *Shape {
	return record(required("clusterRef", ref(CephCluster)), required("type", enumeration("rbd")), required("poolRef", ref(CephPool)), required("peerClusterRef", ref(CephCluster)), required("peerPoolRef", ref(CephPool)), field("imageRefs", set(ref(CephRBDImage))))
}
