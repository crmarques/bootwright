package storage

import (
	"encoding/binary"
	"math/big"
	"net/netip"
	"regexp"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

var ingressRefusedMembers = []refusedMember{
	{names: []string{"ssl_cert", "ssl_key"}, message: "native documents never carry TLS material inline", remedy: "remove spec.<member>; " + tlsBindingRemedy},
	{names: []string{"monitor_user", "monitor_password", "keepalived_password"}, message: "cephadm generates the ingress credentials and keeps them as derived confidential children", remedy: "omit spec.<member>; Bootwright never authors or reads ingress credentials"},
	{names: []string{"generate_cert", "custom_sans"}, message: "cephadm-signed certificates are not supported by Bootwright yet", remedy: "omit spec.<member>; " + tlsBindingRemedy},
	{names: []string{"virtual_ips_list"}, message: "one ingress serves one virtual IP in this build", remedy: "declare spec.virtual_ip instead of spec.virtual_ips_list"},
	{names: []string{"use_keepalived_multicast", "vrrp_interface_network"}, message: "keepalived transport options have no qualified effect contract", remedy: "omit spec.<member>"},
	{names: []string{"enable_stats", "monitor_ssl", "monitor_ssl_cert", "monitor_ssl_key", "monitor_cert_source", "monitor_networks", "monitor_ip_addrs"}, message: "HAProxy statistics endpoint options have no qualified effect contract", remedy: "omit spec.<member>"},
	{names: []string{"verify_backend_ssl_cert", "verification_ca_cert", "use_tcp_mode_over_rgw"}, message: "TLS terminates at the ingress; backend TLS from the ingress is not qualified", remedy: "omit spec.<member>"},
	{names: []string{"haproxy_qat_support", "ssl_dh_param", "ssl_ciphers", "ssl_options"}, message: "this ingress option has no qualified effect contract", remedy: "omit spec.<member>"},
}

var healthCheckInterval = regexp.MustCompile(`^\d+(s|m|h)$`)

type ingressArm struct {
	backend string
	members []string
}

var ingressArms = []ingressArm{
	{backend: "rgw", members: []string{"ssl", "certificate_source"}},
	{backend: "nfs", members: []string{"keepalive_only", "enable_haproxy_protocol", "haproxy_peer_communication_port"}},
}

func ParseVirtualIP(text string, prefixed bool) (netip.Prefix, bool) {
	var prefix netip.Prefix
	if prefixed {
		parsed, err := netip.ParsePrefix(text)
		if err != nil {
			return netip.Prefix{}, false
		}
		prefix = parsed
	} else {
		address, err := netip.ParseAddr(text)
		if err != nil || address.Zone() != "" {
			return netip.Prefix{}, false
		}
		prefix = netip.PrefixFrom(address, address.BitLen())
	}
	if !prefix.IsValid() || prefix.Addr().Zone() != "" || prefix.Addr().Is4In6() {
		return netip.Prefix{}, false
	}
	return prefix, true
}

func ingressBackendType(cluster api.Object, ingress api.Value) string {
	backend, ok := NativeService(cluster, textOf(ingress, "spec", "backend_service"))
	if !ok {
		return ""
	}
	return textOf(backend, "service_type")
}

func (a *admission) ingressRecord(service api.Value, field string, placed []api.Value, partial bool) {
	spec := service.Get("spec")
	if spec.Has("health_check_interval") && !healthCheckInterval.MatchString(textOf(spec, "health_check_interval")) {
		a.issue("api.invariant", field+".spec.health_check_interval", "health_check_interval must be a whole number of seconds, minutes or hours", "write spec.health_check_interval as digits followed by s, m or h")
	}
	if vip := spec.Get("virtual_ip"); vip.Type() == api.String {
		if _, ok := ParseVirtualIP(vip.Text(), true); !ok {
			a.issue("api.invariant", field+".spec.virtual_ip", "an ingress virtual_ip is an address with its network prefix length", "write spec.virtual_ip as address/prefix, for example 192.0.2.10/24")
		}
	}
	for j, entry := range spec.Get("virtual_interface_networks").Items() {
		if prefix, err := netip.ParsePrefix(entry.Text()); err != nil || prefix != prefix.Masked() {
			a.issue("api.invariant", indexed(field+".spec.virtual_interface_networks", j), "virtual_interface_networks entries are network prefixes without host bits", "write the entry as its network address and prefix length")
		}
	}
	keepalive := spec.Get("keepalive_only").Bool()
	if keepalive && spec.Get("enable_haproxy_protocol").Bool() {
		a.issue("api.invariant", field+".spec.keepalive_only", "keepalive-only and HAProxy protocol modes are exclusive", "set only one of spec.keepalive_only and spec.enable_haproxy_protocol")
	}
	a.certificateSource(spec, field)
	if partial {
		return
	}
	for _, port := range []string{"frontend_port", "monitor_port"} {
		if !keepalive && !spec.Has(port) {
			a.issue("api.required", field+".spec."+port, "an ingress that runs HAProxy needs "+port, "supply spec."+port)
		}
	}
	a.ingressPorts(spec, field, ingressBackendType(a.object, service) == "nfs")
	a.serviceNetworks(a.object, spec.Get("virtual_interface_networks"), field+".spec.virtual_interface_networks")
	if len(placed) < 2 {
		a.issue("api.invariant", field+".placement", "an ingress service needs at least two eligible hosts for failover", "select at least two declared storage hosts in the ingress placement")
	}
}

func (a *admission) ingressPorts(spec api.Value, field string, nfs bool) {
	type port struct {
		name  string
		value *big.Int
	}
	ports := []port{}
	for _, name := range []string{"frontend_port", "monitor_port", "haproxy_peer_communication_port"} {
		if spec.Has(name) {
			ports = append(ports, port{name: name, value: numeric(spec.Get(name))})
		} else if nfs && name == "haproxy_peer_communication_port" {
			ports = append(ports, port{name: name, value: big.NewInt(1024)})
		}
	}
	for i, left := range ports {
		if slices.ContainsFunc(ports[i+1:], func(right port) bool { return left.value.Cmp(right.value) == 0 }) {
			a.issue("api.invariant", field+".spec."+left.name, "ingress frontend, monitor and peer ports must differ", "choose distinct ports; an NFS ingress reserves 1024 for HAProxy peers unless spec.haproxy_peer_communication_port names another")
		}
	}
}

func (a *admission) nativeServiceGraph(ceph api.Value) {
	services := ceph.Get("services").Items()
	fronts := map[string]int{}
	for i, service := range services {
		if textOf(service, "service_type") != "ingress" {
			continue
		}
		field := indexed("$.spec.ceph.services", i)
		if backend, ok := a.ingressBackend(service, field); ok {
			name := NativeServiceName(backend)
			if _, taken := fronts[name]; taken {
				a.issue("api.invariant", field+".spec.backend_service", "one ingress serves each backend service in this build", "remove the second ingress for this backend")
			} else {
				fronts[name] = i
			}
		}
		a.ingressVIP(service, field, services[:i])
	}
	for i, service := range services {
		field := indexed("$.spec.ceph.services", i)
		at, fronted := fronts[NativeServiceName(service)]
		switch textOf(service, "service_type") {
		case "rgw":
			if fronted {
				a.rgwBehindIngress(service, field)
			}
		case "nfs":
			if fronted {
				a.nfsMode(service, field, services[at], indexed("$.spec.ceph.services", at))
			} else {
				a.nfsDirect(service, field)
			}
		}
	}
}

func (a *admission) ingressBackend(service api.Value, field string) (api.Value, bool) {
	spec := service.Get("spec")
	backend, ok := NativeService(a.object, textOf(spec, "backend_service"))
	if !ok {
		a.issue("api.invariant", field+".spec.backend_service", "ingress backend_service must resolve to one declared service of this cluster", "set spec.backend_service to the qualified name of a declared rgw or nfs service")
		return api.Value{}, false
	}
	at := slices.IndexFunc(ingressArms, func(arm ingressArm) bool { return arm.backend == textOf(backend, "service_type") })
	if at < 0 {
		a.issue("api.invariant", field+".spec.backend_service", "ingress backend_service must name a declared rgw or nfs service of this cluster", "set spec.backend_service to a declared rgw or nfs service; other ingress backends wait for their owning item (B479 for the management gateway)")
		return api.Value{}, false
	}
	for _, other := range ingressArms {
		for _, member := range other.members {
			if spec.Has(member) && !slices.Contains(ingressArms[at].members, member) {
				a.issue("api.invariant", field+".spec."+member, "spec."+member+" applies only to an ingress whose backend is a "+other.backend+" service", "omit spec."+member+" for this backend")
			}
		}
	}
	if ingressArms[at].backend == "rgw" {
		a.tlsBindingRules(service, field, true)
	}
	return backend, true
}

func (a *admission) ingressVIP(service api.Value, field string, earlier []api.Value) {
	prefix, ok := ParseVirtualIP(textOf(service, "spec", "virtual_ip"), true)
	if !ok {
		return
	}
	address, at := prefix.Addr(), field+".spec.virtual_ip"
	networks := declaredPublicNetworks(a.object, a.catalog)
	index := slices.IndexFunc(networks, func(network netip.Prefix) bool { return network.Contains(address) && network.Bits() == prefix.Bits() })
	if index < 0 {
		a.issue("api.invariant", at, "an ingress virtual_ip must lie inside a declared public CIDR and carry that CIDR's prefix length", "choose a free address inside one of spec.ceph.networks.publicCIDRs and use its prefix length")
	} else if network := networks[index]; address == network.Addr() || (address.Is4() && network.Bits() < 31 && address == broadcastAddress(network)) {
		a.issue("api.invariant", at, "an ingress virtual_ip cannot be the network or broadcast address of its public CIDR", "choose a host address inside the public CIDR")
	}
	if slices.Contains(machineAddresses(a.catalog), address) {
		a.issue("api.invariant", at, "an ingress virtual_ip equals a declared Machine address", "choose an address that no Machine declares")
	}
	used := slices.ContainsFunc(earlier, ingressUses(address))
	for _, cluster := range a.catalog.OfKind(api.StorageCluster) {
		if cluster.Name() != a.object.Name() && slices.ContainsFunc(cluster.Spec().Get("ceph", "services").Items(), ingressUses(address)) {
			used = true
		}
	}
	if used {
		a.issue("api.invariant", at, "this ingress virtual_ip is already used by another ingress service", "choose a virtual IP no other ingress uses")
	}
}

func frontedByIngress(cluster api.Object, service api.Value) bool {
	name := NativeServiceName(service)
	return slices.ContainsFunc(cluster.Spec().Get("ceph", "services").Items(), func(ingress api.Value) bool {
		return textOf(ingress, "service_type") == "ingress" && textOf(ingress, "spec", "backend_service") == name
	})
}

func ingressUses(address netip.Addr) func(api.Value) bool {
	return func(service api.Value) bool {
		prefix, ok := ParseVirtualIP(textOf(service, "spec", "virtual_ip"), true)
		return textOf(service, "service_type") == "ingress" && ok && prefix.Addr() == address
	}
}

func broadcastAddress(network netip.Prefix) netip.Addr {
	bytes := network.Masked().Addr().As4()
	value := binary.BigEndian.Uint32(bytes[:]) | uint32(uint64(1)<<(32-network.Bits())-1)
	binary.BigEndian.PutUint32(bytes[:], value)
	return netip.AddrFrom4(bytes)
}

func machineAddresses(catalog api.Catalog) []netip.Addr {
	out := []netip.Addr{}
	for _, machine := range catalog.OfKind(api.Machine) {
		for _, entry := range machine.Spec().Get("network", "addresses").Items() {
			text := textOf(entry, "address")
			if prefix, err := netip.ParsePrefix(text); err == nil {
				out = append(out, prefix.Addr())
			} else if address, err := netip.ParseAddr(text); err == nil {
				out = append(out, address)
			}
		}
	}
	return out
}
