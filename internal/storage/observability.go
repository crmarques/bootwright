package storage

import (
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	inlineCredentialMessage = "native documents never carry credentials inline"
	tlsNotQualifiedMessage  = "TLS options of this service are not supported by Bootwright yet; the selected release's native default applies"
	dnsLabels               = `[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*`
)

var (
	retentionTimePattern   = regexp.MustCompile(`^\d+(y|w|d|h|m|s)$`)
	retentionSizePattern   = regexp.MustCompile(`^\d+(B|KB|MB|GB|TB|PB|EB)$`)
	engineIDPattern        = regexp.MustCompile(`^[0-9A-Fa-f]{10,64}$`)
	dnsNamePattern         = regexp.MustCompile(`^` + dnsLabels + `$`)
	decimalPortPattern     = regexp.MustCompile(`^[0-9]{1,5}$`)
	allowlistDomainPattern = regexp.MustCompile(`^(\.|\*\.)?(` + dnsLabels + `|\d{1,3}(\.\d{1,3}){3})(:(\*|\d{1,5}))?$`)
)

var (
	scrapeRefused        = refusedMember{names: []string{"targets", "remote_write_url", "remote_write_allowed_metrics"}, message: "scrape targets and remote write send metrics to endpoints with no qualified effect contract", remedy: "omit spec.<member>"}
	monitoringTLSRefused = refusedMember{names: []string{"ssl", "certificate_source"}, message: tlsNotQualifiedMessage, remedy: "omit spec.<member>"}
	inlineTLSRefused     = refusedMember{names: []string{"ssl_cert", "ssl_key"}, message: "native documents never carry TLS material inline", remedy: "remove spec.<member>; " + tlsBindingRemedy}
	signedSANsRefused    = refusedMember{names: []string{"custom_sans"}, message: "cephadm-signed certificates are not supported by Bootwright yet", remedy: "omit spec.custom_sans"}
)

var observabilityRefusedMembers = map[string][]refusedMember{
	"prometheus":    {scrapeRefused, monitoringTLSRefused},
	"node-exporter": {scrapeRefused, monitoringTLSRefused},
	"alertmanager":  {monitoringTLSRefused, {names: []string{"secure"}, message: "this Alertmanager option has no qualified effect contract", remedy: "omit spec.secure"}},
	"grafana":       {monitoringTLSRefused, {names: []string{"initial_admin_password"}, message: inlineCredentialMessage, remedy: "remove spec.initial_admin_password; bind /spec/initial_admin_password to a Secret in spec.ceph.serviceBindings"}},
	"ceph-exporter": {
		monitoringTLSRefused,
		{names: []string{"sock_dir"}, message: "cephadm ignores sock_dir, so the stored service would not match this document", remedy: "omit spec.sock_dir"},
		{names: []string{"addrs"}, message: "this ceph-exporter option has no qualified effect contract", remedy: "omit spec.addrs"},
	},
	"snmp-gateway": {{names: []string{"credentials"}, message: inlineCredentialMessage, remedy: "remove spec.credentials; bind each credential leaf under /spec/credentials to a Secret in spec.ceph.serviceBindings"}},
	"mgmt-gateway": {
		inlineTLSRefused,
		signedSANsRefused,
		{names: []string{"certificate_source"}, message: "certificate sources are not selectable by Bootwright yet", remedy: "omit spec.certificate_source"},
		{names: []string{"ssl_prefer_server_ciphers", "ssl_session_tickets", "ssl_session_timeout", "ssl_session_cache", "server_tokens", "ssl_stapling", "ssl_stapling_verify"}, message: "this management gateway option has no qualified effect contract", remedy: "omit spec.<member>"},
	},
	"oauth2-proxy": {
		monitoringTLSRefused,
		inlineTLSRefused,
		signedSANsRefused,
		{names: []string{"client_secret", "cookie_secret"}, message: inlineCredentialMessage, remedy: "remove spec.<member>; bind /spec/<member> to a Secret in spec.ceph.serviceBindings"},
		{names: []string{"ssl_insecure_skip_verify"}, message: "oauth2-proxy must verify the identity provider's certificate", remedy: "omit spec.ssl_insecure_skip_verify"},
	},
}

var unrealizedServiceTypes = []refusedMember{
	{names: []string{"loki", "alloy", "promtail"}, message: "Bootwright does not realize log collection yet", remedy: "remove this service from spec.ceph.services; B479 tracks log collection"},
	{names: []string{"crash"}, message: "cephadm bootstrap creates the crash service on every host as a derived child of the StorageCluster, so a declared crash service would be its second owner", remedy: "remove the crash service from spec.ceph.services; the cluster keeps its derived crash service"},
	{names: []string{"jaeger-agent", "jaeger-collector", "jaeger-query", "jaeger-tracing", "elasticsearch"}, message: "Bootwright does not realize tracing services", remedy: "remove this service from spec.ceph.services"},
	{names: []string{"node-proxy", "agent", "container"}, message: "Bootwright does not realize this native service type", remedy: "remove this service from spec.ceph.services"},
}

func unrealizedServiceType(name string) (refusedMember, bool) {
	for _, row := range unrealizedServiceTypes {
		if slices.Contains(row.names, name) {
			return row, true
		}
	}
	return refusedMember{}, false
}

func (a *admission) prometheusModule(ceph api.Value, index int) {
	field := indexed("$.spec.ceph.mgrModules", index)
	if slices.ContainsFunc(ceph.Get("services").Items(), func(service api.Value) bool { return textOf(service, "service_type") == "prometheus" }) {
		a.issue("api.invariant", field, "manager module prometheus is derived from the declared prometheus service", "remove prometheus from spec.ceph.mgrModules; cephadm enables it when it deploys the declared prometheus service")
		return
	}
	a.issue("api.invariant", field, "manager module prometheus is derived from a declared prometheus service", "remove prometheus from spec.ceph.mgrModules and declare a prometheus service in spec.ceph.services; cephadm enables the module when it deploys that service")
}

func (a *admission) alertmanagerUserData(spec api.Value, field string) api.Value {
	data := spec.Get("user_data")
	if data.Type() != api.Mapping {
		return spec
	}
	for _, key := range data.Fields() {
		if key.Name != "default_webhook_urls" {
			a.issue("api.invariant", field+".user_data."+key.Name, "cephadm reads only default_webhook_urls from user_data, so this key would be stored but never applied", "move the URLs to spec.user_data.default_webhook_urls and remove this key")
			data = data.Without(key.Name)
		}
	}
	return spec.With("user_data", data)
}

func stringMember(spec api.Value, name string) (string, bool) {
	value := spec.Get(name)
	return value.Text(), value.Type() == api.String
}

func plainURL(text string, schemes ...string) bool {
	parsed, err := url.Parse(text)
	return err == nil && slices.Contains(schemes, parsed.Scheme) && parsed.Hostname() != "" && parsed.User == nil
}

func validPort(text string) bool {
	port, err := strconv.Atoi(text)
	return decimalPortPattern.MatchString(text) && err == nil && port >= 1 && port <= 65535
}

func (a *admission) observabilityRecord(service api.Value, typeName, field string, partial bool) {
	spec := service.Get("spec")
	at := field + ".spec."
	if slices.Contains([]string{"prometheus", "alertmanager", "grafana"}, typeName) && spec.Get("only_bind_port_on_networks").Bool() && service.Get("networks").Len() == 0 {
		a.issue("api.invariant", at+"only_bind_port_on_networks", "only_bind_port_on_networks needs the service networks it binds to", "declare networks on this service or omit spec.only_bind_port_on_networks")
	}
	switch typeName {
	case "prometheus":
		if text, ok := stringMember(spec, "retention_time"); ok && !retentionTimePattern.MatchString(text) {
			a.issue("api.invariant", at+"retention_time", "retention_time is digits followed by y, w, d, h, m or s", "write spec.retention_time as digits followed by one unit, for example 14d")
		}
		if text, ok := stringMember(spec, "retention_size"); ok && !retentionSizePattern.MatchString(text) {
			a.issue("api.invariant", at+"retention_size", "retention_size is digits followed by B, KB, MB, GB, TB, PB or EB", "write spec.retention_size as digits followed by one unit, for example 50GB")
		}
	case "alertmanager":
		a.alertmanagerRecord(spec, at)
	case "snmp-gateway":
		a.snmpGatewayRecord(spec, at, partial)
	case "mgmt-gateway":
		a.mgmtGatewayRecord(spec, at, partial)
	case "oauth2-proxy":
		a.oauth2ProxyRecord(spec, at)
	}
}

func (a *admission) alertmanagerRecord(spec api.Value, at string) {
	if port := spec.Get("port"); port.Type() == api.Integer && port.Text() == "9094" {
		a.issue("api.invariant", at+"port", "cephadm reserves port 9094 for the Alertmanager cluster listener", "choose another spec.port")
	}
	for j, entry := range spec.Get("user_data", "default_webhook_urls").Items() {
		if entry.Type() == api.String && !plainURL(entry.Text(), "http", "https") {
			a.issue("api.invariant", indexed(at+"user_data.default_webhook_urls", j), "an Alertmanager webhook URL is an http or https URL without user information", "write the entry as http(s)://host[:port]/path and keep credentials out of the URL")
		}
	}
}

func splitHostPort(text string) (string, string, bool) {
	if rest, ok := strings.CutPrefix(text, "["); ok {
		host, port, found := strings.Cut(rest, "]:")
		return host, port, found && !strings.ContainsAny(host, "[]") && !strings.ContainsAny(port, "[]:")
	}
	at := strings.LastIndex(text, ":")
	if at < 0 || strings.Contains(text[:at], ":") || strings.ContainsAny(text, "[]") {
		return "", "", false
	}
	return text[:at], text[at+1:], true
}

func snmpDestination(text string) bool {
	host, port, ok := splitHostPort(text)
	if !ok || !validPort(port) {
		return false
	}
	if address, err := netip.ParseAddr(host); err == nil {
		return address.Is4()
	}
	return dnsNamePattern.MatchString(host)
}

func (a *admission) snmpGatewayRecord(spec api.Value, at string, partial bool) {
	if text, ok := stringMember(spec, "snmp_destination"); ok && !snmpDestination(text) {
		a.issue("api.invariant", at+"snmp_destination", "snmp_destination is an IPv4 address or DNS name and a port", "write spec.snmp_destination as host:port")
	}
	if text, ok := stringMember(spec, "engine_id"); ok && (!engineIDPattern.MatchString(text) || len(text)%2 != 0) {
		a.issue("api.invariant", at+"engine_id", "engine_id is 10 to 64 hexadecimal characters of even length", "write spec.engine_id as an even number of 10 to 64 hexadecimal digits")
	}
	if !partial && textOf(spec, "snmp_version") == "V3" && !spec.Has("engine_id") {
		a.issue("api.invariant", at+"engine_id", "SNMP V3 notifications need an engine_id", "set spec.engine_id or use snmp_version V2c")
	}
}

func (a *admission) mgmtGatewayRecord(spec api.Value, at string, partial bool) {
	if ssl := spec.Get("ssl"); ssl.Type() == api.Boolean && !ssl.Bool() {
		a.issue("api.invariant", at+"ssl", "a plaintext management gateway would carry dashboard credentials unencrypted", "omit spec.ssl or set it to true")
	}
	if ciphers := spec.Get("ssl_ciphers"); ciphers.Type() == api.Sequence && ciphers.Len() == 0 {
		a.issue("api.invariant", at+"ssl_ciphers", "ssl_ciphers lists at least one cipher", "list the ciphers or omit spec.ssl_ciphers")
	}
	text, ok := stringMember(spec, "virtual_ip")
	if !ok {
		return
	}
	address, bare := ParseVirtualIP(text, false)
	if !bare {
		a.issue("api.invariant", at+"virtual_ip", "a management gateway virtual_ip is a bare address without a prefix length", "write spec.virtual_ip as an address without a prefix length, inside a declared public network")
		return
	}
	if partial {
		return
	}
	declared := declaredPublicNetworks(a.object, a.catalog)
	if len(declared) != 0 && !slices.ContainsFunc(declared, func(prefix netip.Prefix) bool { return prefix.Contains(address.Addr()) }) {
		a.issue("api.invariant", at+"virtual_ip", "a management gateway virtual_ip lies inside a declared public network, so a host interface carries it", "choose an address inside spec.ceph.networks.publicCIDRs")
	}
}

func (a *admission) oauth2ProxyRecord(spec api.Value, at string) {
	if text, ok := stringMember(spec, "https_address"); ok {
		if _, port, ok := splitHostPort(text); !ok {
			a.issue("api.invariant", at+"https_address", "https_address is host:port", "write spec.https_address as host:4180 or omit it")
		} else if port != "4180" {
			a.issue("api.invariant", at+"https_address", "cephadm reserves port 4180 for oauth2-proxy whatever https_address names", "use port 4180 in spec.https_address or omit it")
		}
	}
	for _, member := range []string{"oidc_issuer_url", "redirect_url"} {
		if text, ok := stringMember(spec, member); ok && !plainURL(text, "https") {
			a.issue("api.invariant", at+member, member+" is an https URL without user information", "write spec."+member+" as https://host[:port]/path")
		}
	}
	for j, entry := range spec.Get("allowlist_domains").Items() {
		if entry.Type() == api.String && !allowlistDomain(entry.Text()) {
			a.issue("api.invariant", indexed(at+"allowlist_domains", j), "allowlist_domains entries are [.|*.]host[:port|:*]", "write the entry as a domain or IPv4 address with an optional leading . or *. and an optional :port or :*")
		}
	}
}

func allowlistDomain(text string) bool {
	if !allowlistDomainPattern.MatchString(text) {
		return false
	}
	at := strings.LastIndex(text, ":")
	return at < 0 || text[at+1:] == "*" || validPort(text[at+1:])
}

func (a *admission) observabilityGraph(ceph api.Value) {
	services := ceph.Get("services").Items()
	declared := map[string]bool{}
	authenticating := false
	for _, service := range services {
		typeName := textOf(service, "service_type")
		declared[typeName] = true
		authenticating = authenticating || typeName == "mgmt-gateway" && service.Get("spec", "enable_auth").Bool()
	}
	for i, service := range services {
		field := indexed("$.spec.ceph.services", i)
		switch textOf(service, "service_type") {
		case "snmp-gateway":
			if !declared["alertmanager"] {
				a.issue("api.invariant", field+".service_type", "the SNMP gateway receives alerts only from Alertmanager", "declare an alertmanager service in spec.ceph.services or remove the snmp-gateway service")
			}
		case "oauth2-proxy":
			if !authenticating {
				a.issue("api.invariant", field+".service_type", "oauth2-proxy authenticates only through a management gateway with enable_auth: true", "declare a mgmt-gateway service with spec.enable_auth: true, or remove the oauth2-proxy service")
			}
		case "mgmt-gateway":
			if service.Get("spec", "enable_auth").Bool() && !declared["oauth2-proxy"] {
				a.issue("api.invariant", field+".spec.enable_auth", "enable_auth needs an oauth2-proxy service as the management gateway's authentication backend", "declare an oauth2-proxy service or set spec.enable_auth: false")
			}
		}
	}
}
