package access

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
)

func TestSessionPrivateKeyReadsOnlyTheDeclaredRepresentation(t *testing.T) {
	for _, kind := range []string{"sshKeyPair", "opaque"} {
		catalog := api.NewCatalog([]api.Object{api.NewObject(api.Secret, "admin", api.MapValue(), api.MapValue().With("type", api.StringValue(kind)))})
		part := secrets.PrivateKeyPart
		if kind == "opaque" {
			part = secrets.ValuePart
		}
		material := secrets.NewMaterial(map[secrets.Part][]byte{part: []byte("private fixture")})
		bytes, err := privateKey("admin", map[string]secrets.Material{"admin": material}, privateKeyPart(catalog, "admin"))
		if err != nil || string(bytes) != "private fixture" {
			t.Fatalf("%s: %q, %v", kind, bytes, err)
		}
		clear(bytes)
		material.Clear()
		wrong := secrets.NewMaterial(map[secrets.Part][]byte{secrets.PublicKeyPart: []byte("public fixture")})
		bytes, err = privateKey("admin", map[string]secrets.Material{"admin": wrong}, privateKeyPart(catalog, "admin"))
		if err == nil || len(bytes) != 0 {
			t.Fatalf("%s accepted a public key", kind)
		}
		wrong.Clear()
	}
}
