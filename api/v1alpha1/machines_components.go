package v1alpha1

func machineComponentSchema() *Shape {
	endpoint := named(record(required("name", nonempty()), required("addressRef", nonempty())))
	service := func(implementation, defaultPort string, extras ...Field) *Shape {
		fields := []Field{required("implementation", enumeration(implementation)), required("machineRef", ref(Machine)), defaulted("bindAddress", ip(), StringValue("0.0.0.0")), defaulted("port", port(), IntegerValue(defaultPort)), field("endpoints", endpoint)}
		return record(append(fields, extras...)...)
	}
	listener := named(record(required("name", nonempty()), required("protocol", enumeration("http", "https")), required("port", port())))
	defaultListeners := ListValue(MapValue(FieldValue{Name: "name", Value: StringValue("https")}, FieldValue{Name: "protocol", Value: StringValue("https")}, FieldValue{Name: "port", Value: IntegerValue("8443")}))
	artifact := record(required("machineRef", ref(Machine)), defaulted("bindAddress", ip(), StringValue("0.0.0.0")), defaulted("retention", enumeration("persistent", "install-only"), StringValue("persistent")), field("tls", record(required("secretRef", secret("tlsCertificate")), defaulted("minVersion", enumeration("TLSv1.2", "TLSv1.3"), StringValue("TLSv1.2")))), defaulted("listeners", listener, defaultListeners), field("endpoints", named(record(required("name", nonempty()), required("listenerRef", nonempty()), required("addressRef", nonempty())))))
	loadBalancer := record(required("implementation", enumeration("haproxy")), required("machineRef", ref(Machine)), required("bindAddresses", nonemptyArray(array(record(required("address", ip()), field("name", nonempty()))))))
	return choice(field("artifactServer", artifact), field("loadBalancer", loadBalancer), field("proxy", service("squid", "3128")), field("nameResolution", service("dnsmasq", "53", field("additionalIngressHosts", set(dns())), field("forwarders", set(ip())))), field("ntp", service("chrony", "123", field("upstreamSources", set(lexical("host"))))), field("registry", service("mirror-registry", "5000")))
}
