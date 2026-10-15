package v1alpha1

func storageNativePlacement() *Shape {
	host := &Shape{Alternatives: []*Shape{nonempty(), record(required("hostname", nonempty()), field("network", nonempty()), field("name", nonempty()))}}
	pattern := &Shape{Alternatives: []*Shape{nonempty(), record(required("pattern", nonempty()), field("pattern_type", enumeration("fnmatch", "regex")))}}
	return record(field("hosts", set(host)), field("label", nonempty()), field("host_pattern", pattern), field("count", integer("1", "")), field("count_per_host", integer("1", "")))
}

func storageNativeService() *Shape {
	return record(required("service_type", nonempty()), field("service_id", nonempty()), field("service_name", nonempty()), required("placement", storageNativePlacement()), field("networks", set(cidr())), field("config", stringsMap()), field("spec", native()), field("unmanaged", boolean()), field("preview_only", boolean()), field("extra_container_args", array(text())), field("extra_entrypoint_args", array(text())), field("custom_configs", array(record(required("mount_path", nonempty()), required("content", nonempty())))))
}

func storageServiceBindings() *Shape {
	secrets := nonemptyArray(array(record(required("path", nonempty()), required("secretRef", secret("opaque", "token", "tlsCertificate", "sshKeyPair")))))
	secrets.NameKey = "path"
	bindings := array(record(required("serviceRef", nonempty()), required("secrets", secrets)))
	bindings.NameKey = "serviceRef"
	return bindings
}

func storageNativeDeviceSelector() *Shape {
	device := &Shape{Alternatives: []*Shape{lexical("device-path"), record(required("path", lexical("device-path")), field("crush_device_class", nonempty()))}}
	return record(field("paths", set(device)), field("all", boolean()), field("model", nonempty()), field("vendor", nonempty()), field("rotational", integer("0", "1")), field("size", nonempty()), field("limit", integer("0", "")))
}

func StorageNativeServiceSpecSchema(serviceType string) *Shape {
	switch serviceType {
	case "mon":
		locations := &Shape{Type: Mapping, Open: true, Element: array(nonempty()), Atomic: true}
		return record(field("crush_locations", locations))
	case "mgr", "mds":
		return record()
	case "osd":
		return record(required("data_devices", storageNativeDeviceSelector()), field("db_devices", storageNativeDeviceSelector()), field("wal_devices", storageNativeDeviceSelector()), field("encrypted", boolean()), field("tpm2", boolean()), field("osds_per_device", integer("1", "")), field("crush_device_class", nonempty()), field("filter_logic", enumeration("AND", "OR")), field("block_db_size", integer("1", "")), field("block_wal_size", integer("1", "")), field("db_slots", integer("1", "")), field("wal_slots", integer("1", "")), field("data_allocate_fraction", number("0", "1")), field("objectstore", enumeration("bluestore")), field("method", enumeration("lvm", "raw")))
	case "rgw":
		return record(field("rgw_realm", nonempty()), field("rgw_zonegroup", nonempty()), field("rgw_zone", nonempty()), field("rgw_frontend_port", integer("1", "65535")), field("rgw_frontend_type", enumeration("beast", "civetweb")), field("ssl", boolean()), field("certificate_source", enumeration("inline", "reference", "cephadm-signed")), field("only_bind_port_on_networks", boolean()), field("rgw_exit_timeout_secs", integer("1", "")))
	case "ingress":
		return record(required("backend_service", nonempty()), required("virtual_ip", nonempty()), field("frontend_port", integer("1", "65535")), field("monitor_port", integer("1", "65535")), field("first_virtual_router_id", integer("1", "255")), field("virtual_interface_networks", set(nonempty())), field("health_check_interval", nonempty()), field("keepalive_only", boolean()), field("enable_haproxy_protocol", boolean()), field("haproxy_peer_communication_port", integer("1", "65535")), field("ssl", boolean()), field("certificate_source", enumeration("inline", "reference", "cephadm-signed")))
	case "nfs":
		return record(field("port", integer("1", "65535")), field("monitoring_port", integer("1", "65535")), field("virtual_ip", nonempty()), field("enable_haproxy_protocol", boolean()))
	default:
		return nil
	}
}
