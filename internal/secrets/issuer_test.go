package secrets

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestGeneratedIssuerRequiresGeneratedCA(t *testing.T) {
	ca := generatedSecret("caBundle", "commonName", "synthetic root")
	leaf := generatedSecret("tlsCertificate", "commonName", "synthetic leaf")
	leaf = api.NewObject(api.Secret, "leaf", api.MapValue(), leaf.Spec().WithPath(api.StringValue(ca.Name()), "source", "generated", "issuerRef"))
	if issues := Validate(leaf, api.NewCatalog([]api.Object{ca, leaf})); len(issues) != 0 {
		t.Fatalf("generated issuer refused: %v", issues)
	}
	for _, issuer := range []api.Object{ca.WithSpec(ca.Spec().With("source", api.MapValue(api.FieldValue{Name: "contextStore", Value: api.MapValue()}))), ca.WithSpec(ca.Spec().With("type", api.StringValue("tlsCertificate")))} {
		if issues := Validate(leaf, api.NewCatalog([]api.Object{issuer, leaf})); len(issues) == 0 {
			t.Fatal("unproved issuer admitted")
		}
	}
	ca = ca.WithSpec(ca.Spec().WithPath(api.StringValue(ca.Name()), "source", "generated", "issuerRef"))
	if issues := Validate(ca, api.NewCatalog([]api.Object{ca})); len(issues) == 0 {
		t.Fatal("CA issuer cycle admitted")
	}
}
