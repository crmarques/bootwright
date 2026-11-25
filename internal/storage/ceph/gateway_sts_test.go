package ceph

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const stsKey = "Sts0Synthetic1Ky"

func oidcProvider(name, issuer string) api.Object {
	return api.NewObject(api.OIDCProvider, name, api.Value{}, requestMap("management", "external", "issuerURL", issuer))
}

func stsGateway(pairs ...any) api.Object {
	return objectGateway("objects", "endpoint", requestMap("fqdn", "s3.example.test"), "authentication", requestMap(pairs...))
}

func stsArm(providers ...string) api.Value {
	if len(providers) == 0 {
		return requestMap("keyRef", "gateway-sts-key")
	}
	return requestMap("keyRef", "gateway-sts-key", "oidcProviderRefs", api.StringList(providers...))
}

func stsCatalog(gateway api.Object, extra ...api.Object) api.Catalog {
	objects := append([]api.Object{gateway, oidcProvider("public-sso", "https://sso.example.test/realms/ceph"), oidcProvider("another-sso", "https://login.example.test")}, extra...)
	return planCatalog(3, 3, func(spec api.Value) api.Value {
		spec = withPairAndRGW(spec)
		return spec.WithPath(api.ListValue(tlsBinding("rgw.objects", "gateway-tls")), "serviceBindings")
	}, objects...)
}

func stsMaterial(t *testing.T, key []byte) map[string]secrets.Material {
	t.Helper()
	material := tlsMaterial(leafPEM(t, []string{"s3.example.test"}))
	material["gateway-sts-key"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: key})
	return material
}

func TestAGatewayWithSTSFreezesItsSwitchAndProvidersWithoutTheKey(t *testing.T) {
	d := gatewayBlock(t, stsCatalog(stsGateway("sts", stsArm("public-sso", "another-sso"))))
	r := gatewayOf(t, d)
	config, mounts := ldapDocument(t, r)
	if !reflect.DeepEqual(config, map[string]any{"rgw_s3_auth_use_sts": "true"}) || mounts != nil {
		t.Fatalf("the rgw document's config %v and mounts %v", config, mounts)
	}
	want := &GatewaySTS{KeySecretRef: "gateway-sts-key", Providers: []GatewayOIDCProvider{{IssuerURL: "https://login.example.test", Name: "another-sso"},
		{IssuerURL: "https://sso.example.test/realms/ceph", Name: "public-sso"}}}
	if r.Gateway.Authentication == nil || r.Gateway.Authentication.LDAP != nil || !reflect.DeepEqual(r.Gateway.Authentication.STS, want) {
		t.Fatalf("the gateway froze %+v", r.Gateway.Authentication)
	}
	if strings.Contains(string(d.Request), stsKey) || strings.Contains(string(d.Request), "rgw_sts_key") {
		t.Fatalf("the request %s", d.Request)
	}
	plain := gatewayOf(t, gatewayBlock(t, stsCatalog(stsGateway("sts", stsArm()))))
	if plain.Gateway.Authentication.STS.Providers != nil {
		t.Fatalf("a gateway without providers froze %+v", plain.Gateway.Authentication.STS)
	}
}

func TestAGatewayWithLDAPAndSTSFreezesBothArms(t *testing.T) {
	gateway := stsGateway("ldap", requestMap("ldapServerRef", "directory"), "sts", stsArm())
	r := gatewayOf(t, gatewayBlock(t, stsCatalog(gateway, ldapServer())))
	config, mounts := ldapDocument(t, r)
	if config["rgw_s3_auth_use_sts"] != "true" || config["rgw_s3_auth_use_ldap"] != "true" || len(mounts) != 2 {
		t.Fatalf("the rgw document's config %v and mounts %v", config, mounts)
	}
	if refs := r.SecretReferences(); !slices.Contains(refs, "directory-bind") || !slices.Contains(refs, "gateway-sts-key") {
		t.Fatalf("the gateway references %v", refs)
	}
}

func TestBindingRefusesAnSTSKeyOtherThanSixteenLettersAndDigits(t *testing.T) {
	ctx, capability := context.Background(), New(api.CephObjectGateway, nil)
	block := reconciliation.Block{BlockDefinition: gatewayBlock(t, stsCatalog(stsGateway("sts", stsArm())))}
	if err := capability.ProveBinding(ctx, "example", block, stsMaterial(t, []byte(stsKey))); err != nil {
		t.Fatalf("a valid STS key refuses: %v", err)
	}
	const message = "the Secret gateway-sts-key that CephObjectGateway objects names as its STS key is not exactly 16 letters and digits, the AES-128 key RGW's STS needs"
	const remedy = "store exactly 16 letters and digits, without a final newline, in Secret gateway-sts-key with bootwright secret set --context example --name gateway-sts-key --value-stdin, then apply again"
	for name, key := range map[string]string{"empty": "", "15 characters": stsKey[:15], "17 characters": stsKey + "a", "a final newline": stsKey + "\n",
		"a dash": stsKey[:15] + "-", "an underscore": "_" + stsKey[1:], "a space": stsKey[:8] + " " + stsKey[9:], "a non-ASCII letter": stsKey[:14] + "é",
		"a generated token": "q3MmX0YbHnUeV6rjQyZy-A"} {
		t.Run(name, func(t *testing.T) {
			err := capability.ProveBinding(ctx, "example", block, stsMaterial(t, []byte(key)))
			refusedStop(t, err, cephSecretInvalid, message, remedy)
			if key != "" && strings.Contains(err.Error(), strings.TrimSpace(key)) {
				t.Fatal("the refusal echoes the value")
			}
		})
	}
	material := stsMaterial(t, []byte(stsKey))
	delete(material, "gateway-sts-key")
	refusedStop(t, capability.ProveBinding(ctx, "example", block, material), cephUnconfigured, "the STS key Secret a CephObjectGateway names is not bound to this apply", "")
}

func TestGatewayMaterialLendsTheSTSKeyOnlyToApply(t *testing.T) {
	catalog := stsCatalog(stsGateway("sts", stsArm()))
	r := gatewayOf(t, gatewayBlock(t, catalog))
	e := lifecycle.Execution{Material: stsMaterial(t, []byte(stsKey))}
	files, err := gatewayMaterial(e, r, "apply")
	if err != nil {
		t.Fatal(err)
	}
	if last := files[len(files)-1]; len(files) != 3 || last != (lifecycle.MaterialFile{Name: "rgw-sts-key", Secret: "gateway-sts-key", Part: secrets.ValuePart, Variable: "rgwSTSKey"}) {
		t.Fatalf("apply lends %+v", files)
	}
	for _, operation := range []string{"observe", "destroy"} {
		files, err := gatewayMaterial(e, r, operation)
		if err != nil || len(files) != 1 || files[0].Variable != "gatewayCertificate" {
			t.Fatalf("%s lends %+v (%v)", operation, files, err)
		}
	}
	classes, err := New(api.CephObjectGateway, nil).SecretClasses(reconciliation.Block{BlockDefinition: gatewayBlock(t, catalog)})
	if err != nil || classes.Of("gateway-sts-key") != secrets.EffectMaterial {
		t.Fatalf("the STS key is classified %v (%v)", classes, err)
	}
	plan, err := kindPlan(api.CephObjectGateway, catalog)
	if err != nil || !slices.Contains(plan.Secrets, "gateway-sts-key") {
		t.Fatalf("the plan binds %v (%v)", plan.Secrets, err)
	}
}

func TestGatewaySTSPlanImpacts(t *testing.T) {
	d := gatewayBlock(t, stsCatalog(stsGateway("sts", stsArm("public-sso"))))
	for _, line := range []string{
		"RGW of object gateway objects issues temporary S3 credentials to web identities through STS; Bootwright registers no identity provider or role",
		"register each identity provider and its roles through RGW's IAM API under an account after handoff",
		"the STS key is stored in cephadm's service specification and the configuration database, which every Ceph administrator can read",
		"credentials STS already issued outlive an identity provider outage until they expire; local object users never contact it",
		"each RGW host reaches the identity provider's issuer directly over HTTPS verified by the image's system trust store",
		"rotating the STS key of the completed gateway needs bootwright destroy and a fresh apply",
		"object gateway objects lists OIDCProvider public-sso: register it and its roles through RGW's IAM API after handoff",
	} {
		if !slices.Contains(d.Impacts, line) {
			t.Fatalf("the gateway's impacts %q lack %q", d.Impacts, line)
		}
	}
	for _, line := range d.Impacts {
		if strings.Contains(line, "LDAP") || strings.Contains(line, stsKey) || len(line)+strings.Count(line, "objects")*(63-len("objects")) > 200 {
			t.Fatalf("an STS-only gateway's impact %q", line)
		}
	}
	fronted := gatewayOf(t, d).Gateway
	fronted.Documents = append(fronted.Documents, []byte(`{}`))
	if impacts := objectGatewayImpacts(*fronted); !strings.Contains(impacts[len(impacts)-1], "STS request") {
		t.Fatalf("an ingress gateway's impacts %q", impacts)
	}
}

func TestGatewaySTSRequestGolden(t *testing.T) {
	matchesGolden(t, "request-gateway-sts", gatewayBlock(t, stsCatalog(stsGateway("ldap", requestMap("ldapServerRef", "directory"), "sts", stsArm("public-sso")), ldapServer())).Request, false)
}
