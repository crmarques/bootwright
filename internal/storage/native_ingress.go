package storage

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
