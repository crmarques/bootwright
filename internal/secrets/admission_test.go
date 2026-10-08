package secrets

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func generatedSecret(kind, parameter, value string) api.Object {
	generated := api.MapValue(api.FieldValue{Name: parameter, Value: api.StringValue(value)})
	return api.NewObject(api.Secret, "generated", api.MapValue(), api.MapValue(
		api.FieldValue{Name: "type", Value: api.StringValue(kind)},
		api.FieldValue{Name: "source", Value: api.MapValue(api.FieldValue{Name: "generated", Value: generated})},
	))
}

// A generated parameter generation would refuse is refused at validate, so a
// declaration never passes plan only to fail when its material is made. A
// comment that would end the fleet key's quoted Kickstart line, or its quoting,
// is one of them.
func TestGeneratedParametersRefuseWhatGenerationRefuses(t *testing.T) {
	oversize := strings.Repeat("a", MaxPartBytes+1)
	for _, test := range []struct {
		kind, parameter, value string
	}{
		{"usernamePassword", "username", "ad\x00min"},
		{"usernamePassword", "username", "ad\xffmin"},
		{"usernamePassword", "username", oversize},
		{"sshKeyPair", "comment", "a\x00b"},
		{"sshKeyPair", "comment", "a\xffb"},
		{"sshKeyPair", "comment", oversize},
		{"sshKeyPair", "comment", "fleet\" \u2028%post --nochroot #\u2028touch /mnt/sysroot/root/pwned #\u2028%end #\u2028#"},
		{"sshKeyPair", "comment", "a\u2028b"},
		{"sshKeyPair", "comment", "a\u2029b"},
		{"sshKeyPair", "comment", "a\u0085b"},
		{"sshKeyPair", "comment", "a\x0bb"},
		{"sshKeyPair", "comment", "a\x1eb"},
		{"sshKeyPair", "comment", "a\tb"},
		{"sshKeyPair", "comment", `a"b`},
		{"sshKeyPair", "comment", `a\b`},
		{"tlsCertificate", "commonName", "a\x00b"},
		{"tlsCertificate", "commonName", "a\xffb"},
		{"caBundle", "commonName", oversize},
		{"caBundle", "commonName", ""},
	} {
		issues := Validate(generatedSecret(test.kind, test.parameter, test.value), api.Catalog{})
		field := "$.spec.source.generated." + test.parameter
		if len(issues) != 1 || issues[0].Code != "api.invariant" || issues[0].Field != field || !strings.Contains(issues[0].Message, "NUL") {
			t.Errorf("%s %.12q gave %#v, want one api.invariant at %s", test.parameter, test.value, issues, field)
		}
	}
	for _, test := range []struct{ kind, parameter, value string }{
		{"usernamePassword", "username", "admin"},
		{"sshKeyPair", "comment", "bootwright-machine-key"},
		{"sshKeyPair", "comment", "operator key for the lab"},
	} {
		if issues := Validate(generatedSecret(test.kind, test.parameter, test.value), api.Catalog{}); len(issues) != 0 {
			t.Errorf("%s %q was refused: %v", test.parameter, test.value, issues)
		}
	}
}

// The retired file source remedy names the context its secret set writes to,
// as a placeholder, since admission reads no context; validate fills it in.
func TestTheRetiredFileSourceRemedyNamesTheContext(t *testing.T) {
	object := api.NewObject(api.Secret, "material", api.MapValue(), api.MapValue(
		api.FieldValue{Name: "type", Value: api.StringValue("token")},
		api.FieldValue{Name: "source", Value: api.MapValue(api.FieldValue{Name: "file", Value: api.MapValue()})},
	))
	for name, issues := range map[string][]api.Issue{"a Secret": Validate(object, api.Catalog{}), "a kind default": ValidatePartial(object, api.Catalog{})} {
		if len(issues) != 1 || issues[0].Field != "$.spec.source.file" || !strings.Contains(issues[0].Remediation, "--context <context>") {
			t.Errorf("%s gave %#v", name, issues)
		}
	}
}

// Only a retired file-source remedy has its context placeholder filled, in a
// copy; any other diagnostic, and the Secret name placeholder, stay as given.
func TestNameFileSourceContextFillsOnlyTheRetiredSourceRemedy(t *testing.T) {
	failure := &diagnostics.Failure{Usage: true, Diagnostics: []diagnostics.Diagnostic{
		{Code: "api.field", Field: "$.spec.source.file", Remediation: "run bootwright secret set --name token --context <context> --value-file <path>"},
		{Code: "api.field", Field: "$.spec.other", Remediation: "run bootwright secret list --context <context>"},
		{Code: "api.field", Field: "$.spec.defaults.Secret.source.file", Remediation: "run bootwright secret set --name <name> --context <context> with the file flags of its type"},
	}}
	given := slices.Clone(failure.Diagnostics)
	var named *diagnostics.Failure
	if !errors.As(NameFileSourceContext(failure, "lab"), &named) || named == failure || !named.Usage {
		t.Fatal("the filled refusal is not a copy of the failure")
	}
	want := []string{
		"run bootwright secret set --name token --context lab --value-file <path>",
		"run bootwright secret list --context <context>",
		"run bootwright secret set --name <name> --context lab with the file flags of its type",
	}
	for index, d := range named.Diagnostics {
		if d.Remediation != want[index] {
			t.Errorf("%s remedy %q, want %q", d.Field, d.Remediation, want[index])
		}
	}
	if !reflect.DeepEqual(failure.Diagnostics, given) {
		t.Fatal("the input failure was changed", failure.Diagnostics)
	}
	if NameFileSourceContext(failure, "") != error(failure) {
		t.Fatal("an unnamed context changed the refusal")
	}
	plain := errors.New("synthetic")
	if NameFileSourceContext(plain, "lab") != plain {
		t.Fatal("an error with no diagnostics changed")
	}
}
