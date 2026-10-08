package material

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
)

// Admission cannot import this package, so it repeats what generation refuses.
// Over every class of value either side treats specially, admission refuses a
// generated username, common name or comment exactly when generation would.
func TestAdmissionAgreesWithGenerationOnGeneratedParameters(t *testing.T) {
	values := []string{
		"admin", "bootwright-machine-key", "operator key", "a\x00b", "a\rb", "a\nb", "a:b", "a b", " a", "a ",
		"a\u00a0b", "a\u2028b", "a\u2029b", "a\u0085b", "a\x0bb", "a\tb", `a"b`, `a\b`, "a\xffb", "\u00e9", strings.Repeat("a", secrets.MaxPartBytes),
		strings.Repeat("a", secrets.MaxPartBytes+1),
	}
	for _, value := range values {
		_, generationErr := generatedUsername(value)
		if refused := admissionRefuses(t, "usernamePassword", "username", value); refused != (generationErr != nil) {
			t.Errorf("username %.16q: admission refused=%t, generation error=%v", value, refused, generationErr)
		}
		generationErr = validateGeneratedSSHComment(value)
		if refused := admissionRefuses(t, "sshKeyPair", "comment", value); refused != (generationErr != nil) {
			t.Errorf("comment %.16q: admission refused=%t, generation error=%v", value, refused, generationErr)
		}
	}
	for _, value := range append(values, "") {
		generationErr := validGeneratedCommonName(value)
		for _, kind := range []string{"tlsCertificate", "caBundle"} {
			if refused := admissionRefuses(t, kind, "commonName", value); refused != (generationErr != nil) {
				t.Errorf("%s commonName %.16q: admission refused=%t, generation error=%v", kind, value, refused, generationErr)
			}
		}
	}
}

func admissionRefuses(t *testing.T, kind, parameter, value string) bool {
	t.Helper()
	generated := api.MapValue(api.FieldValue{Name: parameter, Value: api.StringValue(value)})
	object := api.NewObject(api.Secret, "generated", api.MapValue(), api.MapValue(
		api.FieldValue{Name: "type", Value: api.StringValue(kind)},
		api.FieldValue{Name: "source", Value: api.MapValue(api.FieldValue{Name: "generated", Value: generated})},
	))
	for _, issue := range secrets.Validate(object, api.Catalog{}) {
		if issue.Field == "$.spec.source.generated."+parameter {
			return true
		}
		t.Fatalf("unexpected issue %#v", issue)
	}
	return false
}
