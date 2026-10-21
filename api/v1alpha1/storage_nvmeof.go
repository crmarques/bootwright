package v1alpha1

func storageNVMeoFSubsystemSchema() *Shape {
	listeners := nonemptyArray(array(record(required("host", nonempty()), field("trsvcid", port()))))
	listeners.NameKey = "host"
	namespaces := nonemptyArray(array(record(required("nsid", integer("1", "4294967294")), required("imageRef", ref(StorageRBDImage)))))
	namespaces.NameKey = "nsid"
	return record(required("clusterRef", ref(StorageCluster)), required("serviceRef", nonempty()), required("nqn", nonempty()), required("listeners", listeners), required("hosts", nonemptyArray(set(nonempty()))), required("namespaces", namespaces))
}

func storageNVMeoFServiceSpec() *Shape {
	return record(required("pool", nonempty()), required("group", nonempty()), field("port", port()), field("discovery_port", port()), field("transports", enumeration("tcp")), field("enable_auth", boolean()))
}
