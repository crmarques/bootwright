package cephcluster

import (
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	SignOnClientSecret = "/spec/client_secret"
	SignOnCookieSecret = "/spec/cookie_secret"
	SignOnCertificate  = "/spec/ssl_cert"
	SignOnKey          = "/spec/ssl_key"
	SignOnPort         = 4180
	SignOnMaxGroups    = 64
)

var signOnValueSecretTypes = []string{"opaque", "token"}

var signOnSecretPaths = map[string][]string{
	SignOnClientSecret: signOnValueSecretTypes,
	SignOnCookieSecret: signOnValueSecretTypes,
	SignOnCertificate:  {"tlsCertificate"},
	SignOnKey:          {"tlsCertificate"},
}

var signOnRefusedDocument = []refusedMember{
	{names: []string{"service_id", "service_name"}, message: "oauth2-proxy is a singleton service named by its type", remedy: "remove spec.native.<member>"},
	{names: []string{"custom_configs"}, message: "the identity provider's CA bundle comes from the OIDCProvider's trustBundleRef", remedy: "remove spec.native.custom_configs; set spec.trustBundleRef on the OIDCProvider that spec.oidcProviderRef names"},
	{names: []string{"config", "extra_container_args", "extra_entrypoint_args"}, message: "native effect-bearing options are not qualified by this core consumer", remedy: "remove spec.native.<member>"},
	{names: []string{"unmanaged", "preview_only"}, message: "owned services must remain managed and executable", remedy: "remove spec.native.<member>"},
}

var signOnRefusedSpec = []refusedMember{
	{names: []string{"oidc_issuer_url"}, message: "the issuer URL comes from the OIDCProvider's issuerURL", remedy: "remove spec.native.spec.oidc_issuer_url; spec.oidcProviderRef names the OIDCProvider whose spec.issuerURL oauth2-proxy uses"},
	{names: []string{"client_secret", "cookie_secret"}, message: inlineCredentialMessage, remedy: "remove spec.native.spec.<member>; bind /spec/<member> to a Secret in spec.secrets"},
	{names: []string{"ssl_cert", "ssl_key"}, message: "native documents never carry TLS material inline", remedy: "remove spec.native.spec.<member>; bind /spec/ssl_cert and /spec/ssl_key to one tlsCertificate Secret in spec.secrets"},
	{names: []string{"ssl", "certificate_source"}, message: tlsNotQualifiedMessage, remedy: "omit spec.native.spec.<member>"},
	{names: []string{"custom_sans"}, message: "cephadm-signed certificates are not supported by Bootwright yet", remedy: "omit spec.native.spec.custom_sans"},
	{names: []string{"ssl_insecure_skip_verify"}, message: "oauth2-proxy must verify the identity provider's certificate", remedy: "omit spec.native.spec.ssl_insecure_skip_verify"},
	{names: []string{"scope", "email_domains"}, message: "no oauth2-proxy spec Bootwright realizes has this member", remedy: "omit spec.native.spec.<member>"},
}

func withoutRefused(a *admission, value api.Value, rows []refusedMember, field string) api.Value {
	if value.Type() != api.Mapping {
		return value
	}
	for _, member := range value.Fields() {
		for _, row := range rows {
			if slices.Contains(row.names, member.Name) {
				a.issue("api.invariant", field+"."+member.Name, row.message, strings.ReplaceAll(row.remedy, "<member>", member.Name))
				value = value.Without(member.Name)
			}
		}
	}
	return value
}

var (
	signOnGroup = regexp.MustCompile(`^[A-Za-z0-9_./:@+-]+( [A-Za-z0-9_./:@+-]+)*$`)
	signOnClaim = regexp.MustCompile(`^[A-Za-z0-9_./:@+-]+$`)
)

func ValidSignOnGroup(value string) bool { return len(value) <= 256 && signOnGroup.MatchString(value) }
func ValidSignOnClaim(value string) bool { return len(value) <= 256 && signOnClaim.MatchString(value) }

func (a *admission) signOnAccess(access api.Value) {
	groups := access.Get("allowedGroups").Items()
	if len(groups) > SignOnMaxGroups {
		a.invalid("$.spec.access.allowedGroups", "the sign-on allowlist has more than 64 values", "list at most 64 group or role values")
	}
	for i, group := range groups {
		if group.Type() == api.String && !ValidSignOnGroup(group.Text()) {
			a.invalid(indexed("$.spec.access.allowedGroups", i), "a group or role value is not a supported OAuth2 Proxy argument", "use 1 to 256 ASCII letters, digits, or _./:@+- with optional single internal spaces")
		}
	}
	if claim := access.Get("groupsClaim"); claim.Type() == api.String && !ValidSignOnClaim(claim.Text()) {
		a.invalid("$.spec.access.groupsClaim", "the groups claim is not a supported OIDC claim name", "use 1 to 256 ASCII letters, digits, or _./:@+-")
	}
}

func (a *admission) signOnIntrinsic() {
	spec := a.object.Spec()
	a.signOnAccess(spec.Get("access"))
	native := spec.Get("native")
	if native.Type() != api.Mapping {
		a.signOnSecretLeaves(spec.Get("secrets"))
		return
	}
	native = withoutRefused(a, native, signOnRefusedDocument, "$.spec.native")
	if inner := native.Get("spec"); inner.Type() == api.Mapping {
		native = native.With("spec", withoutRefused(a, inner, signOnRefusedSpec, "$.spec.native.spec"))
	}
	if native.Get("placement").Type() == api.Mapping && native.Get("placement").Has("count_per_host") {
		a.invalid("$.spec.native.placement.count_per_host", "oauth2-proxy places at most one daemon per host", "omit placement.count_per_host")
		native = native.With("placement", native.Get("placement").Without("count_per_host"))
	}
	a.nativeShape(native, api.StorageSignOnNativeSchema(), "$.spec.native", a.partial)
	a.oauth2ProxyRecord(native.Get("spec"), "$.spec.native.spec.")
	if placement := native.Get("placement"); a.partial && placement.Type() == api.Mapping {
		a.nativePlacement(api.Object{}, placement, "$.spec.native.placement", true)
	}
	a.signOnSecretLeaves(spec.Get("secrets"))
}

func (a *admission) signOnSecretLeaves(secrets api.Value) {
	bound := map[string]string{}
	for i, entry := range secrets.Items() {
		field := indexed("$.spec.secrets", i)
		path := textOf(entry, "path")
		types, known := signOnSecretPaths[path]
		if !known {
			if entry.Get("path").Type() == api.String {
				a.issue("api.invariant", field+".path", "a CephSignOn binds only /spec/client_secret, /spec/cookie_secret, /spec/ssl_cert and /spec/ssl_key", "remove this entry or bind one of those paths")
			}
			continue
		}
		ref := textOf(entry, "secretRef")
		bound[path] = ref
		if a.partial {
			continue
		}
		if object, found := a.catalog.Find(api.Secret, ref); found && !slices.Contains(types, textOf(object.Spec(), "type")) {
			a.issue("api.invariant", field+".secretRef", "this binding leaf accepts only "+strings.Join(types, " or ")+" Secrets", "bind a "+strings.Join(types, " or ")+" Secret at this path")
		}
	}
	if a.partial {
		return
	}
	for _, path := range []string{SignOnClientSecret, SignOnCookieSecret} {
		if _, ok := bound[path]; !ok {
			a.issue("api.invariant", "$.spec.secrets", "a CephSignOn needs the "+path+" binding, so continuation never depends on a secret cephadm generated", "add a spec.secrets entry with path "+path+" and an opaque or token Secret")
		}
	}
	certificate, hasCertificate := bound[SignOnCertificate]
	key, hasKey := bound[SignOnKey]
	switch {
	case hasCertificate != hasKey:
		a.issue("api.invariant", "$.spec.secrets", "ssl_cert and ssl_key are bound together", "bind both /spec/ssl_cert and /spec/ssl_key to one tlsCertificate Secret")
	case hasCertificate && certificate != key:
		a.issue("api.invariant", "$.spec.secrets", "ssl_cert and ssl_key must bind the certificate and private key of one tlsCertificate Secret", "bind /spec/ssl_cert and /spec/ssl_key to the same tlsCertificate Secret")
	}
}

func (a *admission) signOn(cluster api.Object) {
	if !slices.ContainsFunc(cluster.Spec().Get("services").Items(), func(service api.Value) bool { return textOf(service, "service_type") == "mgmt-gateway" }) {
		a.issue("api.invariant", "$.spec.clusterRef", "a CephSignOn signs users in to its cluster's management gateway, and CephCluster "+cluster.Name()+" declares no mgmt-gateway service",
			"declare a mgmt-gateway service in spec.services of CephCluster "+cluster.Name()+", or remove this CephSignOn")
		return
	}
	for _, other := range a.catalog.OfKind(api.CephSignOn) {
		if other.Name() < a.object.Name() && textOf(other.Spec(), "clusterRef") == cluster.Name() {
			a.issue("api.invariant", "$.spec.clusterRef", "CephSignOn "+other.Name()+" already signs users in to CephCluster "+cluster.Name()+", and a cluster has one sign-on",
				"remove this CephSignOn, or point it at another CephCluster")
			return
		}
	}
	placement := a.object.Spec().Get("native", "placement")
	if placement.Type() != api.Mapping {
		return
	}
	hosts := []string{}
	for _, node := range a.nativePlacement(cluster, placement, "$.spec.native.placement", false) {
		hosts = append(hosts, textOf(node, "name"))
	}
	slices.Sort(hosts)
	a.signOnListeners(cluster, hosts)
}

func (a *admission) signOnListeners(cluster api.Object, hosts []string) {
	own := listener{label: "oauth2-proxy (4180)", low: 4180}
	own.service, own.high, own.binds = -1, own.low, bindAll(hosts, netip.Addr{})
	for _, other := range listenerRegistry(a.placementsOf(cluster), a.catalog) {
		host, clash := listenerClash(own, other)
		if !clash {
			continue
		}
		name := other.label
		if name == "" {
			name = "CephCluster " + cluster.Name() + " " + other.field
		}
		a.issue("api.invariant", "$.spec.native.placement", "the oauth2-proxy port "+strconv.Itoa(SignOnPort)+" is already bound on host "+host+" by "+name, "place oauth2-proxy on other hosts, or move the other listener to another port")
		return
	}
}

func ClusterSignOns(catalog api.Catalog, cluster string) []api.Object {
	objects := slices.DeleteFunc(catalog.OfKind(api.CephSignOn), func(object api.Object) bool { return textOf(object.Spec(), "clusterRef") != cluster })
	slices.SortFunc(objects, func(a, b api.Object) int { return strings.Compare(a.Name(), b.Name()) })
	return objects
}
