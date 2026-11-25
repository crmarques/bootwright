package ceph

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const ldapBindPassword = "Synthetic bind pass phrase 2026"

func ldapServer(pairs ...any) api.Object {
	users := requestMap("baseDN", "OU=People,DC=example,DC=test", "nameAttribute", "sAMAccountName", "filter", "(memberOf=CN=ceph,DC=example,DC=test)")
	spec := append([]any{"management", "external", "urls", api.StringList("ldaps://dc-01.example.test:636", "ldaps://dc-02.example.test"),
		"bindDN", "CN=svc ceph,OU=Service Accounts,DC=example,DC=test", "bindPasswordRef", "directory-bind", "users", users}, pairs...)
	return api.NewObject(api.LDAPServer, "directory", api.Value{}, requestMap(spec...))
}

func ldapGateway() api.Object {
	return objectGateway("objects", "endpoint", requestMap("fqdn", "s3.example.test"), "authentication", requestMap("ldap", requestMap("ldapServerRef", "directory")))
}

func ldapCatalog(server api.Object) api.Catalog {
	return planCatalog(3, 3, func(spec api.Value) api.Value {
		spec = withPairAndRGW(spec)
		return spec.WithPath(api.ListValue(tlsBinding("rgw.objects", "gateway-tls")), "serviceBindings")
	}, ldapGateway(), server)
}

func ldapDocument(t *testing.T, r Request) (map[string]any, []customConfig) {
	t.Helper()
	var document struct {
		CustomConfigs []customConfig `json:"custom_configs"`
		Spec          struct {
			Config map[string]any `json:"config"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(r.Gateway.Documents[0], &document); err != nil {
		t.Fatal(err)
	}
	return document.Spec.Config, document.CustomConfigs
}

func TestAGatewayWithLDAPFreezesItsOptionsAndMountsWithoutSecretBytes(t *testing.T) {
	for name, test := range map[string]struct {
		server api.Object
		ca     string
		mounts int
	}{"with a trust bundle": {ldapServer("trustBundleRef", "directory-ca"), rgwLDAPCAPath, 3}, "on the system store": {ldapServer(), rgwSystemBundle, 2}} {
		t.Run(name, func(t *testing.T) {
			d := gatewayBlock(t, ldapCatalog(test.server))
			r := gatewayOf(t, d)
			config, mounts := ldapDocument(t, r)
			want := map[string]any{"rgw_ldap_binddn": "CN=svc ceph,OU=Service Accounts,DC=example,DC=test", "rgw_ldap_dnattr": "sAMAccountName",
				"rgw_ldap_searchdn": "OU=People,DC=example,DC=test", "rgw_ldap_secret": "/etc/bootwright/rgw-ldap-bind",
				"rgw_ldap_uri": "ldaps://dc-01.example.test:636 ldaps://dc-02.example.test", "rgw_s3_auth_use_ldap": "true",
				"rgw_ldap_searchfilter": "(&(memberOf=CN=ceph,DC=example,DC=test)(sAMAccountName=@USERNAME@))"}
			if !reflect.DeepEqual(config, want) {
				t.Fatalf("the rgw document's config %v, want %v", config, want)
			}
			conf := "TLS_CACERT " + test.ca + "\nTLS_REQCERT demand\nNETWORK_TIMEOUT 3\nTIMEOUT 3\n"
			if len(mounts) != test.mounts || mounts[0] != (customConfig{MountPath: rgwLDAPBindPath}) || mounts[1] != (customConfig{Content: conf, MountPath: rgwLDAPConfPath}) ||
				test.mounts == 3 && mounts[2] != (customConfig{MountPath: rgwLDAPCAPath}) {
				t.Fatalf("the rgw document mounts %+v", mounts)
			}
			ldap := r.Gateway.Authentication.LDAP
			if ldap.Server != "directory" || ldap.BindSecretRef != "directory-bind" || (ldap.TrustSecretRef != "") != (test.mounts == 3) {
				t.Fatalf("the gateway froze %+v", ldap)
			}
			if strings.Contains(string(d.Request), ldapBindPassword) || !strings.Contains(string(d.Request), `"authentication":{"ldap":`) {
				t.Fatalf("the request %s", d.Request)
			}
		})
	}
}

func TestAGatewayLDAPFilterIsRenderedWithTheUsernamePlaceholder(t *testing.T) {
	server := ldapServer()
	unfiltered := server.WithSpec(server.Spec().WithPath(requestMap("baseDN", "DC=example", "nameAttribute", "uid"), "users"))
	config, _ := ldapDocument(t, gatewayOf(t, gatewayBlock(t, ldapCatalog(unfiltered))))
	if _, found := config["rgw_ldap_searchfilter"]; found || config["rgw_ldap_dnattr"] != "uid" {
		t.Fatalf("a directory without a filter froze %v", config)
	}
}

func TestAGatewayWithoutAuthenticationCarriesNoLDAPMember(t *testing.T) {
	d := gatewayBlock(t, directTLSCatalog())
	if r := gatewayOf(t, d); r.Gateway.Authentication != nil || strings.Contains(string(d.Request), "custom_configs") || strings.Contains(string(d.Request), "rgw_ldap") {
		t.Fatalf("a gateway without authentication froze %s", d.Request)
	}
	if objectGatewayImpacts(*gatewayOf(t, d).Gateway) != nil {
		t.Fatal("a gateway without authentication has LDAP impacts")
	}
}

func ldapMaterial(t *testing.T, password, bundle []byte) map[string]secrets.Material {
	t.Helper()
	material := tlsMaterial(leafPEM(t, []string{"s3.example.test"}))
	material["directory-bind"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: password})
	if bundle != nil {
		material["directory-ca"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: bundle})
	}
	return material
}

func TestBindingRefusesALDAPBindPasswordRGWWouldTrimOrCut(t *testing.T) {
	ctx, capability := context.Background(), New(api.CephObjectGateway, nil)
	block := reconciliation.Block{BlockDefinition: gatewayBlock(t, ldapCatalog(ldapServer("trustBundleRef", "directory-ca")))}
	bundle := leafPEM(t, []string{"directory.example.test"})
	if err := capability.ProveBinding(ctx, "example", block, ldapMaterial(t, []byte(ldapBindPassword), append(bundle, leafPEM(t, nil)...))); err != nil {
		t.Fatalf("a valid bind password and bundle refuse: %v", err)
	}
	const message = "the Secret directory-bind that LDAPServer directory names as the bind password of CephObjectGateway objects is not one line of 1 to 1023 bytes of UTF-8 text without leading or trailing whitespace, which RGW would trim or cut"
	const remedy = "store the bind password in Secret directory-bind as one line of 1 to 1023 bytes without leading or trailing whitespace with bootwright secret set --context example --name directory-bind --value-stdin, then apply again"
	for name, password := range map[string]string{"empty": "", "a leading space": " " + ldapBindPassword, "a trailing newline": ldapBindPassword + "\n",
		"a tab": ldapBindPassword + "\t", "two lines": "first\nsecond", "1024 bytes": strings.Repeat("a", 1024), "a NUL": "a\x00b", "invalid UTF-8": "a\xffb"} {
		t.Run(name, func(t *testing.T) {
			err := capability.ProveBinding(ctx, "example", block, ldapMaterial(t, []byte(password), bundle))
			refusedStop(t, err, cephSecretInvalid, message, remedy)
			if password != "" && strings.Contains(err.Error(), password) {
				t.Fatal("the refusal echoes the value")
			}
		})
	}
	const caMessage = "the Secret directory-ca that LDAPServer directory names as the trust bundle of CephObjectGateway objects holds no PEM certificate block that parses, or a block that does not, or more than 64 KiB"
	for name, value := range map[string][]byte{"no block": []byte("not a certificate"), "an unparsable block": []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"),
		"a key block": append(slices.Clone(bundle), []byte("-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n")...), "trailing text": append(slices.Clone(bundle), []byte("junk")...)} {
		t.Run(name, func(t *testing.T) {
			refusedStop(t, capability.ProveBinding(ctx, "example", block, ldapMaterial(t, []byte(ldapBindPassword), value)), cephSecretInvalid, caMessage,
				"store the directory's CA certificates as PEM in Secret directory-ca with bootwright secret set --context example --name directory-ca, then apply again")
		})
	}
	material := ldapMaterial(t, []byte(ldapBindPassword), bundle)
	delete(material, "directory-bind")
	refusedStop(t, capability.ProveBinding(ctx, "example", block, material), cephUnconfigured, "the bind password Secret a CephObjectGateway's LDAPServer names is not bound to this apply", "")
}

func TestGatewayMaterialLendsLDAPLeavesOnlyToApply(t *testing.T) {
	r := gatewayOf(t, gatewayBlock(t, ldapCatalog(ldapServer("trustBundleRef", "directory-ca"))))
	e := lifecycle.Execution{Material: ldapMaterial(t, []byte(ldapBindPassword), leafPEM(t, nil))}
	files, err := gatewayMaterial(e, r, "apply")
	if err != nil {
		t.Fatal(err)
	}
	want := []lifecycle.MaterialFile{{Name: "gateway-certificate", Secret: "gateway-tls", Part: secrets.CertificatePart, Variable: "gatewayCertificate"},
		{Name: "gateway-key", Secret: "gateway-tls", Part: secrets.PrivateKeyPart, Variable: "gatewayKey"},
		{Name: "rgw-ldap-bind-password", Secret: "directory-bind", Part: secrets.ValuePart, Variable: "rgwLDAPBindPassword"},
		{Name: "rgw-ldap-ca-bundle", Secret: "directory-ca", Part: secrets.CertificatePart, Variable: "rgwLDAPCABundle"}}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("apply lends %+v", files)
	}
	for _, operation := range []string{"observe", "destroy"} {
		files, err := gatewayMaterial(e, r, operation)
		if err != nil || len(files) != 1 || files[0].Variable != "gatewayCertificate" {
			t.Fatalf("%s lends %+v (%v)", operation, files, err)
		}
	}
	classes, err := New(api.CephObjectGateway, nil).SecretClasses(reconciliation.Block{BlockDefinition: gatewayBlock(t, ldapCatalog(ldapServer("trustBundleRef", "directory-ca")))})
	if err != nil || classes.Of("directory-bind") != secrets.EffectMaterial || classes.Of("directory-ca") != secrets.EffectMaterial {
		t.Fatalf("the LDAP Secrets are classified %v (%v)", classes, err)
	}
	if refs := r.SecretReferences(); !slices.Contains(refs, "directory-bind") || !slices.Contains(refs, "directory-ca") {
		t.Fatalf("the gateway references %v", refs)
	}
	plan, err := kindPlan(api.CephObjectGateway, ldapCatalog(ldapServer("trustBundleRef", "directory-ca")))
	if err != nil || !slices.Contains(plan.Secrets, "directory-bind") || !slices.Contains(plan.Secrets, "directory-ca") {
		t.Fatalf("the plan binds %v (%v)", plan.Secrets, err)
	}
}

func TestGatewayLDAPPlanImpacts(t *testing.T) {
	d := gatewayBlock(t, ldapCatalog(ldapServer()))
	for _, line := range []string{
		"RGW of object gateway objects authenticates S3 users against LDAPServer directory; a request signed with a local object user's key never contacts the directory",
		ldapOutageImpact, ldapCollisionImpact,
		"the directory bind password is stored in cephadm's service specification, which ceph orch ls --export and the dashboard's service API show to Ceph administrators; use a least-privilege bind account",
		"rotating the directory bind password or trust bundle of the completed gateway needs bootwright destroy and a fresh apply",
	} {
		if !slices.Contains(d.Impacts, line) {
			t.Fatalf("the gateway's impacts %q lack %q", d.Impacts, line)
		}
	}
	if slices.ContainsFunc(d.Impacts, func(impact string) bool { return len(impact) > 200 }) {
		t.Fatalf("an impact of %q exceeds 200 bytes", d.Impacts)
	}
	fronted := gatewayOf(t, d).Gateway
	fronted.Documents = append(fronted.Documents, []byte(`{}`))
	if impacts := objectGatewayImpacts(*fronted); !strings.Contains(impacts[len(impacts)-1], "without TLS on the cluster network") {
		t.Fatalf("an ingress gateway's impacts %q", impacts)
	}
}

func TestGatewayLDAPDeadlineAddsTheMountRoundAndReadback(t *testing.T) {
	plain, ldap := gatewayOf(t, gatewayBlock(t, directTLSCatalog())), gatewayOf(t, gatewayBlock(t, ldapCatalog(ldapServer())))
	for operation, calls := range map[string]int{"apply": 2, "observe": 1, "destroy": 0} {
		before, _ := gatewayDeadline(plain, operation)
		after, _ := gatewayDeadline(ldap, operation)
		if int((after - before).Seconds()) != calls*nativeCall {
			t.Fatalf("%s grows by %s", operation, after-before)
		}
	}
}

func TestGatewayLDAPRequestGolden(t *testing.T) {
	matchesGolden(t, "request-gateway-ldap", gatewayBlock(t, ldapCatalog(ldapServer("trustBundleRef", "directory-ca"))).Request, false)
}
