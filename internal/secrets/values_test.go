package secrets

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// fmt cannot call String or GoString on a value held in an unexported field,
// so it walks that value's fields itself.
type materialHolder struct {
	name     string
	material Material
}

func TestMaterialFormattingNeverRevealsBytes(t *testing.T) {
	const secret = "synthetic-confidential-bytes"
	m := NewMaterial(map[Part][]byte{PasswordPart: []byte(secret)})
	values := map[string]any{
		"value":            m,
		"pointer":          &m,
		"unexported field": materialHolder{name: "holder", material: m},
		"pointer holder":   &materialHolder{name: "holder", material: m},
		"slice":            []Material{m},
		"map":              map[string]Material{"k": m},
		"zero":             materialHolder{},
	}
	goBytes := fmt.Sprintf("%#v", []byte(secret))
	forms := []string{secret, fmt.Sprintf("%x", secret), fmt.Sprint([]byte(secret)), goBytes[strings.Index(goBytes, "{"):]}
	for name, value := range values {
		for _, verb := range []string{"%v", "%+v", "%s", "%#v", "%q", "%x"} {
			got := fmt.Sprintf(verb, value)
			for _, form := range forms {
				if strings.Contains(got, form) {
					t.Errorf("%s formatted with %s revealed material: %s", name, verb, got)
				}
			}
		}
	}
	if got := fmt.Sprint(m); got != "[confidential material]" {
		t.Fatalf("material printed %q", got)
	}
	if _, err := json.Marshal(materialHolder{material: m}.material); err == nil {
		t.Fatal("material serialized")
	}
	if value, ok := m.Part(PasswordPart); !ok || string(value) != secret {
		t.Fatalf("material part = %q, %v", value, ok)
	}
	var zero Material
	if parts := zero.Parts(); len(parts) != 0 || zero.Size() != 0 {
		t.Fatalf("zero material holds parts %v", parts)
	}
	if _, ok := zero.Part(PasswordPart); ok {
		t.Fatal("zero material holds a part")
	}
	zero.Clear()
	if lent := zero.Lend(PasswordPart); lent.Size() != 0 {
		t.Fatal("zero material lent a part")
	}
}
