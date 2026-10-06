package secrets

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
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
