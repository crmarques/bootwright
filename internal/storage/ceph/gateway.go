package ceph

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
)

type Gateway struct {
	Authentication *GatewayAuthentication `json:"authentication,omitempty"`
	Bindings       []GatewayBinding       `json:"bindings"`
	DataExtraPool  string                 `json:"dataExtraPool"`
	DataPool       string                 `json:"dataPool"`
	Documents      []json.RawMessage      `json:"documents"`
	Endpoint       string                 `json:"endpoint"`
	IndexPool      string                 `json:"indexPool"`
	Name           string                 `json:"name"`
	Realm          string                 `json:"realm"`
	Zone           string                 `json:"zone"`
	ZoneGroup      string                 `json:"zoneGroup"`
	ZonePools      []string               `json:"zonePools"`
}

type GatewayAuthentication struct {
	LDAP *GatewayLDAP `json:"ldap,omitempty"`
	STS  *GatewaySTS  `json:"sts,omitempty"`
}

type GatewaySTS struct {
	KeySecretRef string                `json:"keySecretRef"`
	Providers    []GatewayOIDCProvider `json:"providers,omitempty"`
}

type GatewayOIDCProvider struct {
	IssuerURL string `json:"issuerURL"`
	Name      string `json:"name"`
}

type GatewayLDAP struct {
	BindSecretRef  string `json:"bindSecretRef"`
	Server         string `json:"server"`
	TrustSecretRef string `json:"trustSecretRef,omitempty"`
}

const (
	rgwLDAPBindPath   = "/etc/bootwright/rgw-ldap-bind"
	rgwLDAPConfPath   = "/etc/openldap/ldap.conf"
	rgwLDAPCAPath     = "/etc/bootwright/rgw-ldap-ca.pem"
	rgwSystemBundle   = "/etc/pki/tls/certs/ca-bundle.crt"
	maxLDAPBindSecret = 1023
	maxLDAPCABundle   = 65536
	rgwSTSKeyLength   = 16
)

func rgwLDAPConf(trusted bool) string {
	ca := rgwSystemBundle
	if trusted {
		ca = rgwLDAPCAPath
	}
	return "TLS_CACERT " + ca + "\nTLS_REQCERT demand\nNETWORK_TIMEOUT 3\nTIMEOUT 3\n"
}

func rgwLDAPOptions(server api.Value) map[string]string {
	users := server.Get("users")
	options := map[string]string{"rgw_ldap_binddn": server.Get("bindDN").Text(), "rgw_ldap_dnattr": users.Get("nameAttribute").Text(),
		"rgw_ldap_searchdn": users.Get("baseDN").Text(), "rgw_ldap_secret": rgwLDAPBindPath, "rgw_ldap_uri": strings.Join(server.Get("urls").Strings(), " "),
		"rgw_s3_auth_use_ldap": "true"}
	if filter := users.Get("filter").Text(); filter != "" {
		options["rgw_ldap_searchfilter"] = "(&" + filter + "(" + users.Get("nameAttribute").Text() + "=" + api.LDAPUsernameToken + "))"
	}
	return options
}

func gatewayLDAP(c api.Catalog, gateway api.Object) (*GatewayLDAP, map[string]string, []customConfig, error) {
	ref := gateway.Spec().Get("authentication", "ldap", "ldapServerRef").Text()
	if ref == "" {
		return nil, nil, nil, nil
	}
	server, found := c.Find(api.LDAPServer, ref)
	if !found {
		return nil, nil, nil, failure("api.invariant", "CephObjectGateway "+gateway.Name()+" names LDAPServer "+ref+", which is not declared", "declare LDAPServer "+ref+", or set spec.authentication.ldap.ldapServerRef to a declared LDAPServer")
	}
	trust := server.Spec().Get("trustBundleRef").Text()
	mounts := []customConfig{{MountPath: rgwLDAPBindPath}, {Content: rgwLDAPConf(trust != ""), MountPath: rgwLDAPConfPath}}
	if trust != "" {
		mounts = append(mounts, customConfig{MountPath: rgwLDAPCAPath})
	}
	return &GatewayLDAP{BindSecretRef: server.Spec().Get("bindPasswordRef").Text(), Server: server.Name(), TrustSecretRef: trust}, rgwLDAPOptions(server.Spec()), mounts, nil
}

func gatewaySTS(c api.Catalog, gateway api.Object) (*GatewaySTS, error) {
	sts := gateway.Spec().Get("authentication", "sts")
	if !sts.Present() {
		return nil, nil
	}
	out := &GatewaySTS{KeySecretRef: sts.Get("keyRef").Text()}
	for _, ref := range sts.Get("oidcProviderRefs").Strings() {
		provider, found := c.Find(api.OIDCProvider, ref)
		if !found {
			return nil, failure("api.invariant", "CephObjectGateway "+gateway.Name()+" names OIDCProvider "+ref+", which is not declared", "declare OIDCProvider "+ref+", or remove it from spec.authentication.sts.oidcProviderRefs")
		}
		out.Providers = append(out.Providers, GatewayOIDCProvider{IssuerURL: provider.Spec().Get("issuerURL").Text(), Name: provider.Name()})
	}
	slices.SortFunc(out.Providers, func(a, b GatewayOIDCProvider) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (g *Gateway) authSecrets() []string {
	if g == nil || g.Authentication == nil {
		return nil
	}
	out := []string{}
	if ldap := g.Authentication.LDAP; ldap != nil {
		out = append(out, ldap.BindSecretRef)
		if ldap.TrustSecretRef != "" {
			out = append(out, ldap.TrustSecretRef)
		}
	}
	if sts := g.Authentication.STS; sts != nil {
		out = append(out, sts.KeySecretRef)
	}
	return out
}

const (
	ldapOutageImpact    = "while the directory is unreachable, each LDAP-token request waits out its 3-second LDAP timeouts and can delay other requests, local object users' included"
	ldapCollisionImpact = "an LDAP user named like any RGW user ID acts as that RGW user (CephObjectUser names, the dashboard's RGW system user dashboard and multisite system users included): keep directory names disjoint"
)

func objectGatewayImpacts(g Gateway) []string {
	if g.Authentication == nil {
		return nil
	}
	out := []string{}
	if ldap := g.Authentication.LDAP; ldap != nil {
		out = append(out,
			"RGW of object gateway "+g.Name+" authenticates S3 users against LDAPServer "+ldap.Server+"; a request signed with a local object user's key never contacts the directory",
			ldapOutageImpact, ldapCollisionImpact,
			"the directory bind password is stored in cephadm's service specification, which ceph orch ls --export and the dashboard's service API show to Ceph administrators; use a least-privilege bind account",
			"rotating the directory bind password or trust bundle of the completed gateway needs bootwright destroy and a fresh apply")
		if len(g.Documents) > 1 {
			out = append(out, "the ingress of object gateway "+g.Name+" forwards each S3 request, whose LDAP access key carries the user's directory password, to RGW without TLS on the cluster network")
		}
	}
	if sts := g.Authentication.STS; sts != nil {
		out = append(out,
			"RGW of object gateway "+g.Name+" issues temporary S3 credentials to web identities through STS; Bootwright registers no identity provider or role",
			"register each identity provider and its roles through RGW's IAM API under an account after handoff",
			"the STS key is stored in cephadm's service specification and the configuration database, which every Ceph administrator can read",
			"credentials STS already issued outlive an identity provider outage until they expire; local object users never contact it",
			"each RGW host reaches the identity provider's issuer directly over HTTPS verified by the image's system trust store",
			"rotating the STS key of the completed gateway needs bootwright destroy and a fresh apply")
		for _, provider := range sts.Providers {
			out = append(out, "object gateway "+g.Name+" lists OIDCProvider "+provider.Name+": register it and its roles through RGW's IAM API after handoff")
		}
		if len(g.Documents) > 1 {
			out = append(out, "the ingress of object gateway "+g.Name+" forwards each STS request and its temporary credentials to RGW without TLS on the cluster network")
		}
	}
	return out
}

type GatewayBinding struct {
	Path       string `json:"path"`
	SecretRef  string `json:"secretRef"`
	ServiceRef string `json:"serviceRef"`
}

type rgwSpec struct {
	CertificateSource      string            `json:"certificate_source,omitempty"`
	Config                 map[string]string `json:"config,omitempty"`
	OnlyBindPortOnNetworks bool              `json:"only_bind_port_on_networks,omitempty"`
	RGWExitTimeoutSecs     *int64            `json:"rgw_exit_timeout_secs,omitempty"`
	RGWFrontendPort        *int64            `json:"rgw_frontend_port,omitempty"`
	RGWFrontendType        string            `json:"rgw_frontend_type,omitempty"`
	RGWRealm               string            `json:"rgw_realm"`
	RGWZone                string            `json:"rgw_zone"`
	RGWZoneGroup           string            `json:"rgw_zonegroup"`
	SSL                    bool              `json:"ssl,omitempty"`
}

type rgwIngressSpec struct {
	BackendService           string   `json:"backend_service"`
	CertificateSource        string   `json:"certificate_source,omitempty"`
	FirstVirtualRouterID     *int64   `json:"first_virtual_router_id,omitempty"`
	FrontendPort             *int64   `json:"frontend_port,omitempty"`
	HealthCheckInterval      string   `json:"health_check_interval,omitempty"`
	MonitorPort              *int64   `json:"monitor_port,omitempty"`
	SSL                      bool     `json:"ssl,omitempty"`
	VirtualInterfaceNetworks []string `json:"virtual_interface_networks,omitempty"`
	VirtualIP                string   `json:"virtual_ip"`
}

func GatewayBlockID(gateway string) string {
	return reconciliation.BlockIdentity("ceph-gateway-", gateway)
}

func GatewayPoolBlockID(gateway, suffix string) string {
	return reconciliation.BlockIdentity("ceph-gateway-", gateway+"-pool-"+strings.ReplaceAll(suffix, ".", "-"))
}

func derivedZonePools(gateway api.Object) []string {
	declared := map[string]bool{}
	for key, suffix := range zonePlacementSuffixes {
		if gateway.Spec().Get("pools", key).Text() != "" {
			declared[suffix] = true
		}
	}
	out := []string{}
	for _, suffix := range ZonePoolSuffixes {
		if !declared[suffix] {
			out = append(out, suffix)
		}
	}
	return out
}

func zonePoolReplicas(r Request) (size, minSize int) {
	if r.Stretch != nil {
		return 4, 2
	}
	if r.SingleHostDefaults || r.Config["global"]["osd_pool_default_size"] == "2" {
		return 2, 2
	}
	return 3, 2
}

func zonePoolSuffix(name string) (string, bool) {
	zone, suffix, ok := strings.Cut(name, ".rgw.")
	if !ok || zone == "" || strings.Contains(zone, ".") || !slices.Contains(ZonePoolSuffixes, suffix) {
		return "", false
	}
	return suffix, true
}

func gatewayPoolObject(block reconciliation.Block, r Request) string {
	if r.Pool == nil || r.Pool.Role != "rgw" {
		return ""
	}
	suffix, ok := zonePoolSuffix(r.Pool.Name)
	if !ok || block.ID != GatewayPoolBlockID(block.Object, suffix) {
		return ""
	}
	return block.Object
}

func rgwSpecOf(gateway api.Value, config map[string]string) func(api.Value) any {
	return func(spec api.Value) any {
		return rgwSpec{CertificateSource: spec.Get("certificate_source").Text(), Config: config, OnlyBindPortOnNetworks: spec.Get("only_bind_port_on_networks").Bool(),
			RGWExitTimeoutSecs: integerOf(spec.Get("rgw_exit_timeout_secs")), RGWFrontendPort: integerOf(spec.Get("rgw_frontend_port")), RGWFrontendType: spec.Get("rgw_frontend_type").Text(),
			RGWRealm: gateway.Get("realm").Text(), RGWZone: gateway.Get("zone").Text(), RGWZoneGroup: gateway.Get("zoneGroup").Text(), SSL: spec.Get("ssl").Bool()}
	}
}

func rgwIngressSpecOf(spec api.Value) any {
	return rgwIngressSpec{BackendService: spec.Get("backend_service").Text(), CertificateSource: spec.Get("certificate_source").Text(),
		FirstVirtualRouterID: integerOf(spec.Get("first_virtual_router_id")), FrontendPort: integerOf(spec.Get("frontend_port")),
		HealthCheckInterval: spec.Get("health_check_interval").Text(), MonitorPort: integerOf(spec.Get("monitor_port")), SSL: spec.Get("ssl").Bool(),
		VirtualIP: spec.Get("virtual_ip").Text(), VirtualInterfaceNetworks: spec.Get("virtual_interface_networks").Strings()}
}

func gatewayTLSSecret(g *Gateway) string {
	if g == nil || len(g.Bindings) == 0 {
		return ""
	}
	return g.Bindings[0].SecretRef
}

func (c Capability) ProveBinding(ctx context.Context, contextName string, block reconciliation.Block, material map[string]secrets.Material) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := DecodeRequest(block.Request)
	if err != nil {
		return err
	}
	if err := scopeOf(r).proveProvidedPins(contextName, material); err != nil {
		return err
	}
	if c.kind == api.CephCluster {
		return proveGatewayUnit(contextName, block, material)
	}
	if c.kind != api.CephObjectGateway {
		return nil
	}
	if err := proveGatewayTLS(contextName, r, material); err != nil {
		return err
	}
	if err := proveGatewayLDAP(contextName, r, material); err != nil {
		return err
	}
	return proveGatewaySTS(contextName, r, material)
}

func proveGatewaySTS(contextName string, r Request, material map[string]secrets.Material) error {
	if r.Gateway == nil || r.Gateway.Authentication == nil || r.Gateway.Authentication.STS == nil {
		return nil
	}
	ref := r.Gateway.Authentication.STS.KeySecretRef
	value, found := material[ref]
	if !found {
		return cephUnconfigured.Fail("the STS key Secret a CephObjectGateway names is not bound to this apply", "")
	}
	key, _ := value.Part(secrets.ValuePart)
	defer clear(key)
	if !rgwSTSKey(key) {
		return cephSecretInvalid.Fail("the Secret "+ref+" that CephObjectGateway "+r.Gateway.Name+" names as its STS key is not exactly 16 letters and digits, the AES-128 key RGW's STS needs",
			"store exactly 16 letters and digits, without a final newline, in Secret "+ref+" with "+secrets.Command(contextName, "set")+" --name "+ref+" --value-stdin, then apply again")
	}
	return nil
}

func rgwSTSKey(value []byte) bool {
	if len(value) != rgwSTSKeyLength {
		return false
	}
	for _, b := range value {
		if !('a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' || '0' <= b && b <= '9') {
			return false
		}
	}
	return true
}

func proveGatewayTLS(contextName string, r Request, material map[string]secrets.Material) error {
	secret := gatewayTLSSecret(r.Gateway)
	if secret == "" {
		return nil
	}
	value, found := material[secret]
	if !found {
		return cephUnconfigured.Fail("the TLS Secret a CephObjectGateway binds is not bound to this apply", "")
	}
	name, fqdn := r.Gateway.Name, r.Gateway.Endpoint
	set := secrets.Command(contextName, "set") + " --name " + secret + " --certificate-file <path> --private-key-file <path>"
	leaf, ok := boundLeaf(value)
	if !ok {
		return secrets.Refusal("part", "the certificate of Secret "+secret+" that CephObjectGateway "+name+" binds cannot be read", contextName, secret, "store a readable certificate and its private key with "+set+", then apply again")
	}
	if leaf.VerifyHostname(fqdn) != nil {
		return secrets.Refusal("part", "the certificate of Secret "+secret+" does not name "+fqdn+", the endpoint of CephObjectGateway "+name+" that clients and its readiness proof connect to", contextName, secret,
			"store a certificate whose subject alternative names include "+fqdn+" (a DNS name or a matching wildcard) with "+set+", or set spec.endpoint.fqdn of CephObjectGateway "+name+" to a name the certificate holds, then apply again")
	}
	return nil
}

func proveGatewayLDAP(contextName string, r Request, material map[string]secrets.Material) error {
	if r.Gateway == nil || r.Gateway.Authentication == nil || r.Gateway.Authentication.LDAP == nil {
		return nil
	}
	ldap, name := r.Gateway.Authentication.LDAP, r.Gateway.Name
	value, found := material[ldap.BindSecretRef]
	if !found {
		return cephUnconfigured.Fail("the bind password Secret a CephObjectGateway's LDAPServer names is not bound to this apply", "")
	}
	password, _ := value.Part(secrets.ValuePart)
	defer clear(password)
	if !rgwBindPassword(password) {
		return cephSecretInvalid.Fail("the Secret "+ldap.BindSecretRef+" that LDAPServer "+ldap.Server+" names as the bind password of CephObjectGateway "+name+" is not one line of 1 to 1023 bytes of UTF-8 text without leading or trailing whitespace, which RGW would trim or cut",
			"store the bind password in Secret "+ldap.BindSecretRef+" as one line of 1 to 1023 bytes without leading or trailing whitespace with "+secrets.Command(contextName, "set")+" --name "+ldap.BindSecretRef+" --value-stdin, then apply again")
	}
	if ldap.TrustSecretRef == "" {
		return nil
	}
	bundle, found := material[ldap.TrustSecretRef]
	if !found {
		return cephUnconfigured.Fail("the trust bundle Secret a CephObjectGateway's LDAPServer names is not bound to this apply", "")
	}
	certificates, _ := bundle.Part(secrets.CertificatePart)
	defer clear(certificates)
	if !parsedBundle(certificates) {
		return cephSecretInvalid.Fail("the Secret "+ldap.TrustSecretRef+" that LDAPServer "+ldap.Server+" names as the trust bundle of CephObjectGateway "+name+" holds no PEM certificate block that parses, or a block that does not, or more than 64 KiB",
			"store the directory's CA certificates as PEM in Secret "+ldap.TrustSecretRef+" with "+secrets.Command(contextName, "set")+" --name "+ldap.TrustSecretRef+", then apply again")
	}
	return nil
}

func rgwBindPassword(value []byte) bool {
	const space = " \t\n\v\f\r"
	return len(value) != 0 && len(value) <= maxLDAPBindSecret && utf8.Valid(value) && !bytes.ContainsAny(value, "\x00\r\n") &&
		!strings.ContainsRune(space, rune(value[0])) && !strings.ContainsRune(space, rune(value[len(value)-1]))
}

func parsedBundle(data []byte) bool {
	if len(data) == 0 || len(data) > maxLDAPCABundle {
		return false
	}
	parsed := 0
	for rest := data; ; {
		block, remaining := pem.Decode(rest)
		if block == nil {
			return parsed > 0 && len(bytes.TrimSpace(rest)) == 0
		}
		if _, err := x509.ParseCertificate(block.Bytes); block.Type != "CERTIFICATE" || err != nil {
			return false
		}
		parsed, rest = parsed+1, remaining
	}
}

func proveGatewayUnit(contextName string, block reconciliation.Block, material map[string]secrets.Material) error {
	if block.Implementation != Implementation {
		return nil
	}
	r, err := DecodeRequest(block.Request)
	if err != nil {
		return err
	}
	if r.Action != "service" || r.Service == nil || r.Service.Type != "mgmt-gateway" {
		return nil
	}
	at := slices.IndexFunc(r.Service.Bindings, func(b ServiceBinding) bool { return b.ServiceRef == "mgmt-gateway" && b.Path == "/spec/ssl_cert" })
	if at < 0 {
		return nil
	}
	secret := r.Service.Bindings[at].SecretRef
	value, found := material[secret]
	if !found {
		return cephUnconfigured.Fail("the TLS Secret the management gateway of a CephCluster binds is not bound to this apply", "")
	}
	set := secrets.Command(contextName, "set") + " --name " + secret + " --certificate-file <path> --private-key-file <path>"
	leaf, ok := boundLeaf(value)
	if !ok {
		return secrets.Refusal("part", "the certificate of Secret "+secret+" that the management gateway of CephCluster "+r.Cluster+" binds cannot be read", contextName, secret, "store a readable certificate and its private key with "+set+", then apply again")
	}
	for _, document := range unitDocuments(r) {
		if document.ServiceType != "mgmt-gateway" {
			continue
		}
		spec, _ := document.Spec.(map[string]any)
		if vip, _ := spec["virtual_ip"].(string); vip != "" {
			if leaf.VerifyHostname(vip) != nil {
				return secrets.Refusal("part", "the certificate of Secret "+secret+" does not name "+vip+", the virtual IP of the management gateway of CephCluster "+r.Cluster+" that clients and its readiness proof connect to", contextName, secret,
					"store a certificate whose subject alternative names include the IP address "+vip+" with "+set+", or set the management gateway's spec.virtual_ip and its ingress's spec.virtual_ip to an address the certificate holds, then apply again")
			}
			continue
		}
		for _, host := range document.Placement.Hosts {
			if leaf.VerifyHostname(host) != nil {
				return secrets.Refusal("part", "the certificate of Secret "+secret+" does not name "+host+", a host of the management gateway of CephCluster "+r.Cluster+" that clients and its readiness proof connect to", contextName, secret,
					"store a certificate whose subject alternative names include "+host+" (a DNS name or a matching wildcard) with "+set+", or place the management gateway only on hosts the certificate names, then apply again")
			}
		}
	}
	if fqdn := r.Service.Endpoint; fqdn != "" && leaf.VerifyHostname(fqdn) != nil {
		return secrets.Refusal("part", "the certificate of Secret "+secret+" does not name "+fqdn+", the name clients use for the management gateway of CephCluster "+r.Cluster, contextName, secret,
			"reissue the certificate with "+fqdn+" as a DNS name (or a matching wildcard) in its subject alternative names and store it with "+set+", or set spec.managementGateway.fqdn of CephCluster "+r.Cluster+" to a DNS name the certificate already holds, then apply again")
	}
	return nil
}

func boundLeaf(value secrets.Material) (*x509.Certificate, bool) {
	certificate, _ := value.Part(secrets.CertificatePart)
	defer clear(certificate)
	block, _ := pem.Decode(certificate)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, false
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	return leaf, err == nil
}

func gatewayBindings(cluster api.Object, services ...string) []GatewayBinding {
	out := []GatewayBinding{}
	for _, binding := range cluster.Spec().Get("serviceBindings").Items() {
		ref := binding.Get("serviceRef").Text()
		if !slices.Contains(services, ref) {
			continue
		}
		for _, secret := range binding.Get("secrets").Items() {
			out = append(out, GatewayBinding{Path: secret.Get("path").Text(), SecretRef: secret.Get("secretRef").Text(), ServiceRef: ref})
		}
	}
	slices.SortStableFunc(out, func(a, b GatewayBinding) int {
		if c := strings.Compare(a.ServiceRef, b.ServiceRef); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	return out
}

func frontingIngresses(cluster api.Object, backend string) []api.Value {
	out := []api.Value{}
	for _, svc := range cluster.Spec().Get("services").Items() {
		if svc.Get("service_type").Text() == "ingress" && svc.Get("spec", "backend_service").Text() == backend {
			out = append(out, svc)
		}
	}
	slices.SortFunc(out, func(a, b api.Value) int {
		return strings.Compare(cephcluster.NativeServiceName(a), cephcluster.NativeServiceName(b))
	})
	return out
}

func placementPool(gateway api.Value, suffix string) string {
	for key, replaced := range zonePlacementSuffixes {
		if replaced == suffix {
			if name := gateway.Get("pools", key).Text(); name != "" {
				return name
			}
		}
	}
	return gateway.Get("zone").Text() + ".rgw." + suffix
}

func declaredPlacementPools(gateway api.Object) []string {
	out := []string{}
	for key := range zonePlacementSuffixes {
		if name := gateway.Spec().Get("pools", key).Text(); name != "" {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func gatewayRequest(c api.Catalog, cluster, gateway api.Object, base Request) (Request, error) {
	spec := gateway.Spec()
	svc, found := cephcluster.NativeService(cluster, spec.Get("serviceRef").Text())
	if !found {
		return Request{}, failure("api.invariant", "CephObjectGateway "+gateway.Name()+" binds no declared rgw service", "set spec.serviceRef to rgw.<service_id> of a declared rgw service")
	}
	ldap, options, mounts, err := gatewayLDAP(c, gateway)
	if err != nil {
		return Request{}, err
	}
	sts, err := gatewaySTS(c, gateway)
	if err != nil {
		return Request{}, err
	}
	if sts != nil {
		if options == nil {
			options = map[string]string{}
		}
		options["rgw_s3_auth_use_sts"] = "true"
	}
	value, err := unitDocumentValue(cluster, svc, rgwSpecOf(spec, options))
	if err != nil {
		return Request{}, err
	}
	if len(mounts) != 0 {
		value.CustomConfigs = mounts
	}
	document, err := json.Marshal(value)
	if err != nil {
		return Request{}, err
	}
	documents, bound := []json.RawMessage{document}, []string{cephcluster.NativeServiceName(svc)}
	for _, ingress := range frontingIngresses(cluster, cephcluster.NativeServiceName(svc)) {
		fronting, err := unitDocumentOf(cluster, ingress, rgwIngressSpecOf)
		if err != nil {
			return Request{}, err
		}
		documents, bound = append(documents, fronting), append(bound, cephcluster.NativeServiceName(ingress))
	}
	bindings, endpoint := gatewayBindings(cluster, bound...), spec.Get("endpoint", "fqdn").Text()
	if len(bindings) != 0 && endpoint == "" {
		name := gateway.Name()
		return Request{}, failure("api.invariant", "CephObjectGateway "+name+" terminates TLS at "+bindings[0].ServiceRef+" but declares no spec.endpoint, whose FQDN its certificate must name",
			"declare spec.endpoint on CephObjectGateway "+name+": spec.endpoint.fqdn with the name its clients use, or spec.endpoint: {} to derive it from the gateway, cluster and Environment storage domain")
	}
	zonePools := []string{}
	for _, suffix := range ZonePoolSuffixes {
		zonePools = append(zonePools, nativePoolRef(c, placementPool(spec, suffix)))
	}
	slices.Sort(zonePools)
	r := base
	r.Action = "gateway"
	r.Gateway = &Gateway{Bindings: bindings, DataExtraPool: nativePoolRef(c, placementPool(spec, "buckets.non-ec")), DataPool: nativePoolRef(c, placementPool(spec, "buckets.data")), Documents: documents,
		Endpoint: endpoint, IndexPool: nativePoolRef(c, placementPool(spec, "buckets.index")), Name: gateway.Name(), Realm: spec.Get("realm").Text(),
		Zone: spec.Get("zone").Text(), ZoneGroup: spec.Get("zoneGroup").Text(), ZonePools: slices.Compact(zonePools)}
	if ldap != nil || sts != nil {
		r.Gateway.Authentication = &GatewayAuthentication{LDAP: ldap, STS: sts}
	}
	return r, nil
}

func gatewayDeadline(r Request, operation string) (time.Duration, error) {
	budget := map[string]unitBudget{"apply": {calls: 5, waits: 1}, "observe": {calls: 1}, "destroy": {calls: 2}}[operation]
	if r.Gateway != nil && r.Gateway.Authentication != nil && operation != "destroy" {
		budget.calls += map[string]int{"apply": 2, "observe": 1}[operation]
	}
	return time.Duration(budget.calls*nativeCall+budget.waits*readinessWait+60) * time.Second, nil
}

func gatewayRequests(c api.Catalog, gateway api.Object, d derivation) ([]Request, error) {
	cluster, _ := c.Find(api.CephCluster, gateway.Spec().Get("clusterRef").Text())
	base, err := d.base(c, cluster)
	if err != nil {
		return nil, err
	}
	name, suffixes := gateway.Name(), derivedZonePools(gateway)
	size, minSize := zonePoolReplicas(base)
	osdHosts := 0
	for _, host := range base.Hosts {
		if len(host.Devices) > 0 {
			osdHosts++
		}
	}
	if base.SingleHostDefaults {
		osdHosts = len(base.Hosts[0].Devices)
	}
	if size > osdHosts || !base.SingleHostDefaults && base.Stretch == nil && size == osdHosts {
		remedy := "add OSD-bearing hosts to CephCluster " + cluster.Name()
		if base.Config["global"]["osd_pool_default_size"] != "2" {
			remedy += ", or set osd_pool_default_size and osd_pool_default_min_size to \"2\" under its spec.config.global"
		}
		return nil, failure("api.invariant", fmt.Sprintf("CephObjectGateway %s derives its zone pools with the cluster's default replica size %d, which needs more than %d OSD-bearing hosts; %d are declared", name, size, size, osdHosts), remedy)
	}
	if err := pgBudget(c, cluster, base, poolRule(cluster)); err != nil {
		return nil, err
	}
	zone := gateway.Spec().Get("zone").Text()
	requests := make([]Request, 0, len(suffixes)+1)
	for _, suffix := range suffixes {
		r := base
		r.Action = "pool"
		r.Pool = &Pool{Name: zone + ".rgw." + suffix, Type: "replicated", Role: "rgw", Application: cephcluster.RoleApplication("rgw"), CrushRule: poolRule(cluster),
			Autoscale: PoolAutoscale{Mode: "on"}, PGNum: int(clampPGs(0, 0)), Size: size, MinSize: minSize}
		requests = append(requests, r)
	}
	r, err := gatewayRequest(c, cluster, gateway, base)
	if err != nil {
		return nil, err
	}
	return append(requests, r), nil
}
