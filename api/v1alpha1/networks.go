package v1alpha1

func init() {
	register(Network, networkSchema)
}

func networkSubnetSchema() *Shape {
	return record(required("cidr", cidr()), field("gateway", ip()))
}

func networkSchema() *Shape {
	libvirt := record(required("providerRef", ref(InfraProvider)), required("bridge", lexical("bridge")), defaulted("forward", enumeration("nat", "none"), StringValue("nat")), field("hostAddress", ip()), field("taggedNetworkRefs", set(ref(Network))))
	network := choice(field("libvirt", libvirt))
	network.AllowEmpty = true
	network.Fields = append([]Field{required("provided", boolean()), field("description", text()), field("vlan", integer("1", "4094")), defaulted("mtu", integer("576", "65536"), IntegerValue("1500")), field("sites", nonemptyArray(set(name()))), field("ipv4", networkSubnetSchema()), field("ipv6", networkSubnetSchema())}, network.Fields...)
	return network
}
