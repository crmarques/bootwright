package v1alpha1

func init() {
	register(StorageCluster, storageClusterSchema)
	register(StoragePlacementPolicy, storagePlacementPolicySchema)
	register(StoragePool, storagePoolSchema)
	register(StorageFilesystem, storageFilesystemSchema)
	register(StorageObjectGateway, storageObjectGatewaySchema)
	register(StorageNFSExport, storageNFSExportSchema)
	register(StorageExport, storageExportSchema)
}

func storagePlacement() *Shape {
	return record(field("hosts", set(nonempty())), field("sites", set(name())), field("countPerHost", integer("0", "")))
}

func storageTLS() *Shape { return record(required("secretRef", secret("tlsCertificate"))) }

func storageIngress(withTLS bool) *Shape {
	s := record(required("name", nonempty()), required("address", ip()), required("prefixLength", integer("0", "128")), field("virtualInterfaceNetworks", set(cidr())), field("placement", storagePlacement()), field("firstVirtualRouterID", integer("0", "255")))
	if withTLS {
		s.Fields = append(s.Fields, field("tls", storageTLS()))
	}
	return s
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

func storageClusterSchema() *Shape {
	configValues := stringsMap()
	configValues.Element = nonempty()
	config := &Shape{Type: Mapping, Open: true, Element: configValues, Atomic: true}
	monitoring := record(field("enabled", boolean()))
	for _, key := range []string{"prometheus", "grafana", "alertmanager", "nodeExporter", "loki", "promtail"} {
		monitoring.Fields = append(monitoring.Fields, field(key, storageMonitoringService(key)))
	}
	monitoring.Suppress = []Suppression{{Field: "enabled", Value: BoolValue(false), Fields: []string{"prometheus", "grafana", "alertmanager", "nodeExporter", "loki", "promtail"}}}
	gateway := record(defaulted("dnsLabel", name(), StringValue("mgr")), field("port", integer("0", "65535")), field("exposure", enumeration("https", "http")), field("enableAuth", boolean()), field("tls", storageTLS()), field("oauth2Proxy", record(required("providerDisplayName", nonempty()), required("clientId", nonempty()), required("clientSecretRef", secret("opaque", "token")), required("oidcIssuerUrl", lexical("https-url")), field("redirectUrl", url()), field("httpsAddress", nonempty()), field("allowlistDomains", set(dns())), field("cookieSecretRef", secret("opaque", "token")))), required("ingress", storageIngress(false)))
	gateway.Suppress = []Suppression{{Field: "exposure", Value: StringValue("http"), Fields: []string{"tls", "oauth2Proxy"}}, {Field: "enableAuth", Value: BoolValue(false), Fields: []string{"oauth2Proxy"}}}
	cluster := record(
		defaulted("distribution", enumeration("oss", "redhat", "ibm"), StringValue("oss")), required("release", nonempty()), field("packageVersion", nonempty()), field("image", record(field("base", lexical("registry-base")), field("version", lexical("image-version")))), field("community", record(field("mirror", lexical("https-url")), field("checksum", lexical("checksum")))),
		field("ibm", record(required("callHome", enumeration("enabled", "disabled")), field("packages", record(required("source", enumeration("vendor", "subscription")), field("subscriptionRepos", set(nonempty())))))), field("entitlementRef", ref(Entitlement)), field("osSubscriptionRef", ref(Entitlement)),
		required("cephadm", record(field("addressRef", nonempty()), field("workarounds", set(enumeration("mgmt-gateway-spec-dependency-recording"))), field("ansible", record(field("packageVersion", nonempty()))), field("clusterSSH", record(field("user", lexical("posix-user")), field("keyRef", secret("sshKeyPair")))), required("bootstrap", record(required("node", nonempty()), field("addressRef", nonempty()), field("singleHostDefaults", boolean()))))),
		field("networks", record(field("publicCIDRs", set(cidr())), field("clusterCIDRs", set(cidr())))), field("security", record(field("fips", record(field("enabled", boolean()))), field("cephx", record(required("keyType", enumeration("aes", "aes256k")))))), field("config", config), field("mgrModules", set(nonempty())), field("services", array(record(required("serviceType", nonempty()), field("serviceID", nonempty()), required("placement", storagePlacement()), field("spec", native())))), field("monitoring", monitoring), field("mgmtGateway", gateway),
		required("topology", record(required("nodes", nonemptyArray(named(record(required("name", name()), field("fqdn", dns()), required("machineRef", ref(Machine)), field("site", name()), required("roles", nonemptyArray(set(enumeration("mon", "mgr", "osd", "mds", "rgw", "ingress", "prometheus", "grafana", "alertmanager")))), field("labels", set(nonempty())), field("devices", set(nonempty())), field("osd", storageOSD()))))), field("osdDrivegroups", array(record(required("serviceID", nonempty()), field("placement", storagePlacement()), required("osd", storageOSD())))), field("stretch", record(required("failureDomain", nonempty()), field("dataSites", set(name())), field("tiebreaker", record(field("node", text()), field("site", text()))), defaulted("ruleName", nonempty(), StringValue("stretch-rule")))))),
	)
	cluster.Suppress = []Suppression{{Field: "distribution", Value: StringValue("oss"), Fields: []string{"ibm", "entitlementRef", "packageVersion"}}, {Field: "distribution", Value: StringValue("redhat"), Fields: []string{"community", "ibm"}}, {Field: "distribution", Value: StringValue("ibm"), Fields: []string{"community"}}}
	s := record(required("type", enumeration("ceph")), defaulted("management", enumeration("managed", "external"), StringValue("managed")), field("ceph", cluster))
	s.Suppress = []Suppression{{Field: "management", Value: StringValue("external"), Fields: []string{"ceph"}}}
	return s
}

func storagePlacementPolicySchema() *Shape {
	return record(required("clusterRef", ref(StorageCluster)), field("failureDomain", nonempty()), required("ruleName", nonempty()), field("crushDeviceClass", nonempty()), field("replicated", storageReplicated()))
}

func storagePoolSchema() *Shape {
	erasure := record(required("dataChunks", integer("1", "")), required("codingChunks", integer("1", "")), field("plugin", enumeration("", "jerasure", "isa", "clay", "lrc", "shec")), field("technique", nonempty()), field("crushDeviceClass", nonempty()), field("crushRoot", nonempty()), field("stripeUnit", integer("1", "")), field("parameters", stringsMap()))
	s := record(required("clusterRef", ref(StorageCluster)), field("placementPolicyRef", ref(StoragePlacementPolicy)), defaulted("type", enumeration("replicated", "erasure"), StringValue("replicated")), field("role", enumeration("rbd", "cephfs-metadata", "cephfs-data", "rgw")), field("application", nonempty()), field("replicated", storageReplicated()), field("erasure", erasure), field("autoscale", record(field("mode", enumeration("on", "off", "warn")), field("targetSizeRatio", number("0", "")), field("targetSizeBytes", nonempty()), field("pgNumMin", integer("0", "")), field("pgNumMax", integer("0", "")), field("bulk", boolean()))), field("quota", record(field("maxBytes", integer("0", "9223372036854775807")), field("maxObjects", integer("0", "9223372036854775807")))), field("compression", record(field("mode", enumeration("none", "passive", "aggressive", "force")), field("algorithm", enumeration("", "lz4", "snappy", "zlib", "zstd")), field("requiredRatio", number("0", "1")), field("minBlobSize", integer("0", "18446744073709551615")), field("maxBlobSize", integer("0", "18446744073709551615")))), field("mirroring", record(required("mode", enumeration("image", "pool")))))
	s.Discriminator, s.Arms = "type", []string{"replicated", "erasure"}
	s.ArmValues, s.InertArms = map[string][]string{"replicated": {"replicated"}, "erasure": {"erasure"}}, []string{"replicated"}
	return s
}

func storageFilesystemSchema() *Shape {
	dataPool := &Shape{Alternatives: []*Shape{ref(StoragePool), record(required("name", ref(StoragePool)), field("default", boolean()))}}
	return record(required("clusterRef", ref(StorageCluster)), required("metadataPoolRef", ref(StoragePool)), required("dataPoolRefs", nonemptyArray(array(dataPool))), field("mds", record(field("activeCount", integer("0", "")), field("standbyReplay", boolean()), field("standbyCountWanted", integer("0", "")), field("placement", storagePlacement()), field("serviceSpec", storageServiceSpec(false)))), field("subvolumeGroups", named(record(required("name", nonempty()), field("poolLayoutRef", ref(StoragePool)), field("mode", nonempty()), field("uid", integer("0", "")), field("gid", integer("0", "")), field("sizeBytes", integer("0", ""))))))
}

func storageObjectGatewaySchema() *Shape {
	config := stringsMap()
	config.Element = nonempty()
	return record(required("clusterRef", ref(StorageCluster)), required("serviceID", nonempty()), field("placement", storagePlacement()), field("frontendPort", integer("0", "65535")), field("realm", nonempty()), field("zoneGroup", nonempty()), field("zone", nonempty()), field("config", config), field("endpoint", record(field("dnsLabel", name()), field("scheme", enumeration("https", "http")), field("port", port()), field("tls", storageTLS()), required("ingresses", nonemptyArray(named(storageIngress(false)))))))
}

func storageNFSExportSchema() *Shape {
	exports := array(record(required("pseudo", lexical("absolute-path")), field("filesystemRef", ref(StorageFilesystem)), field("path", lexical("absolute-path")), field("bucket", nonempty()), field("accessType", enumeration("RW", "RO", "NONE")), field("squash", nonempty()), field("clients", set(nonempty()))))
	exports.NameKey = "pseudo"
	return record(required("clusterRef", ref(StorageCluster)), required("serviceID", nonempty()), field("port", integer("0", "65535")), required("placement", storagePlacement()), field("ingresses", named(storageIngress(true))), field("exports", exports))
}

func storageExportSchema() *Shape {
	return record(field("type", enumeration("dataFoundation")), required("clusterRef", ref(StorageCluster)), field("dataFoundation", record(required("rbdPoolRef", ref(StoragePool)), required("filesystemRef", ref(StorageFilesystem)), field("objectGatewayRef", ref(StorageObjectGateway)))), field("externalDetails", record(required("fromSecretRef", secret("opaque")))))
}
