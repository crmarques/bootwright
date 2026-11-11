package secrets

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestEveryTrustReferenceAcceptsPublicTrustMaterialAndHasAnExplicitClientPolicy(t *testing.T) {
	count := 0
	var walk func(*api.Shape, string)
	walk = func(shape *api.Shape, path string) {
		if shape == nil {
			return
		}
		walk(shape.Element, path+"[]")
		for _, field := range shape.Fields {
			if strings.HasSuffix(field.Name, "TrustBundleRef") || field.Name == "trustBundleRef" || field.Name == "additionalTrustBundleRefs" {
				count++
				ref := field.Shape
				if ref.Element != nil {
					ref = ref.Element
				}
				if !slices.Equal(ref.SecretTypes, []string{"caBundle", "trustBundle"}) {
					t.Errorf("%s.%s does not accept both public trust types", path, field.Name)
				}
				if field.Name != "additionalTrustBundleRefs" {
					policy := "tlsVerification"
					if field.Name == "packageRepositoryTrustBundleRef" {
						policy = "packageRepositoryTLSVerification"
					} else if strings.HasSuffix(path, ".bmc.tls") {
						policy = "verification"
					}
					f, ok := shape.Field(policy)
					if !ok || !slices.Equal(f.Shape.Enums, []string{"full", "certificate", "none"}) {
						t.Errorf("%s.%s has no client verification policy", path, field.Name)
					}
				}
			}
			walk(field.Shape, path+"."+field.Name)
		}
	}
	for _, kind := range api.Kinds() {
		walk(api.Schema(kind), string(kind))
	}
	if count != 9 {
		t.Fatalf("review trust consumer coverage: found %d references, expected 9", count)
	}
}

func TestTLSVerificationPolicyCannotBeIgnoredOrContradictItsTrust(t *testing.T) {
	for _, kind := range []api.Kind{api.Machine, api.InfraProvider, api.Registry, api.Proxy, api.Environment, api.Entitlement, api.CephCluster} {
		for _, mode := range []string{"full", "certificate", "none"} {
			spec := api.MapValue()
			path := map[api.Kind][]string{
				api.Machine: {"substrate", "baremetal", "bmc", "tls"}, api.InfraProvider: {"baremetal", "defaults", "bmc", "tls"},
				api.Registry: {}, api.Proxy: {"connection"}, api.Environment: {"mirror", "artifacts"},
				api.Entitlement: {"registry"}, api.CephCluster: {},
			}[kind]
			policy, ref := "tlsVerification", "trustBundleRef"
			if kind == api.Machine || kind == api.InfraProvider {
				policy = "verification"
			} else if kind == api.CephCluster {
				policy, ref = "packageRepositoryTLSVerification", "packageRepositoryTrustBundleRef"
			}
			spec = spec.WithPath(api.StringValue(mode), append(slices.Clone(path), policy)...)
			if mode == "certificate" {
				spec = spec.WithPath(api.StringValue("server-trust"), append(slices.Clone(path), ref)...)
			}
			object := api.NewObject(kind, "test", api.MapValue(), spec)
			issues := ValidateTrust(object, api.Catalog{}, func(object api.Object, _ string) bool { return object.Kind() == api.Machine })
			wantRefused := kind != api.Machine && mode != "full"
			if (len(issues) > 0) != wantRefused {
				t.Errorf("%s %s: issues=%+v, refused=%v", kind, mode, issues, wantRefused)
			}
		}
	}
}

func TestCertificateIdentityRefusesCAOnlySecretsAndContradictoryPolicies(t *testing.T) {
	field := func(name, value string) api.FieldValue {
		return api.FieldValue{Name: name, Value: api.StringValue(value)}
	}
	for _, test := range []struct {
		tls     api.Value
		kind    string
		refused bool
	}{
		{api.MapValue(field("verification", "certificate"), field("trustBundleRef", "trust")), "trustBundle", false},
		{api.MapValue(field("verification", "certificate"), field("trustBundleRef", "trust")), "caBundle", true},
		{api.MapValue(field("verification", "certificate")), "trustBundle", true},
		{api.MapValue(field("verification", "none"), field("trustBundleRef", "trust")), "trustBundle", true},
		{api.MapValue(field("verification", "full"), api.FieldValue{Name: "verify", Value: api.BoolValue(false)}), "trustBundle", true},
	} {
		machine := api.NewObject(api.Machine, "test", api.MapValue(), api.MapValue().WithPath(test.tls, "substrate", "baremetal", "bmc", "tls"))
		secret := api.NewObject(api.Secret, "trust", api.MapValue(), api.MapValue(field("type", test.kind)))
		issues := ValidateTrust(machine, api.NewCatalog([]api.Object{secret}), func(api.Object, string) bool { return true })
		if (len(issues) > 0) != test.refused {
			t.Fatalf("%s: issues=%v, refused=%v", test.kind, issues, test.refused)
		}
	}
	if issues := Validate(generatedSecret("trustBundle", "commonName", "test"), api.Catalog{}); len(issues) == 0 {
		t.Fatal("generated trustBundle admitted")
	}
}
