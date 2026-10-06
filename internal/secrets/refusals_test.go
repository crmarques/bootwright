package secrets

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// One table owns which flags secret set takes for each type: the fields a
// flag set may supply, the shape it must have and the file flags the retired
// file source's remedy names, whose wording validate pins.
func TestInputTableOwnsAllowedFieldsAndFileRemedies(t *testing.T) {
	every := []InputFields{ValueFileInput, ValueStdinInput, UsernameInput, PasswordFileInput, PasswordStdinInput, CertificateFileInput, PrivateKeyFileInput, PublicKeyFileInput}
	supply := func(input Input, field InputFields) Input {
		switch field {
		case ValueFileInput:
			input.ValueFile = "v"
		case ValueStdinInput:
			input.ValueStdin = true
		case UsernameInput:
			input.Username = "u"
		case PasswordFileInput:
			input.PasswordFile = "p"
		case PasswordStdinInput:
			input.PasswordStdin = true
		case CertificateFileInput:
			input.CertificateFile = "c"
		case PrivateKeyFileInput:
			input.PrivateKeyFile = "k"
		case PublicKeyFileInput:
			input.PublicKeyFile = "p"
		}
		return input
	}
	for _, row := range []struct {
		kind    string
		allowed InputFields
		file    string
		valid   []Input
	}{
		{"opaque", ValueFileInput | ValueStdinInput, "--value-file <path>", []Input{{ValueFile: "v"}, {ValueStdin: true}}},
		{"token", ValueFileInput | ValueStdinInput, "--value-file <path>", []Input{{ValueFile: "v"}, {ValueStdin: true}}},
		{"dockerConfigJson", ValueFileInput | ValueStdinInput, "--value-file <path>", []Input{{ValueFile: "v"}, {ValueStdin: true}}},
		{"usernamePassword", UsernameInput | PasswordFileInput | PasswordStdinInput, "--username <username> --password-file <path>", []Input{{Username: "u", PasswordFile: "p"}, {Username: "u", PasswordStdin: true}}},
		{"caBundle", CertificateFileInput, "--certificate-file <path>", []Input{{CertificateFile: "c"}}},
		{"tlsCertificate", CertificateFileInput | PrivateKeyFileInput, "--certificate-file <path> --private-key-file <path>", []Input{{CertificateFile: "c", PrivateKeyFile: "k"}}},
		{"sshKeyPair", PrivateKeyFileInput | PublicKeyFileInput, "--private-key-file <path> [--public-key-file <path>]", []Input{{PrivateKeyFile: "k"}, {PrivateKeyFile: "k", PublicKeyFile: "p"}}},
		{"future", 0, "", nil},
	} {
		t.Run(row.kind, func(t *testing.T) {
			if got := AllowedInputFields(row.kind); got != row.allowed {
				t.Fatalf("allowed fields = %b, want %b", got, row.allowed)
			}
			if got := setFileFlags(row.kind); got != row.file {
				t.Fatalf("file flags = %q, want %q", got, row.file)
			}
			for _, input := range row.valid {
				if !ValidInput(row.kind, input) {
					t.Fatalf("%+v was refused", input)
				}
				named := input
				named.Provided = row.allowed
				if !ValidInput(row.kind, named) {
					t.Fatalf("%+v naming only the type's own flags was refused", named)
				}
				for _, field := range every {
					if row.allowed&field != 0 {
						continue
					}
					if ValidInput(row.kind, supply(input, field)) {
						t.Fatalf("%+v with the flag %b of another type was accepted", input, field)
					}
					explicit := input
					explicit.Provided = field
					if ValidInput(row.kind, explicit) {
						t.Fatalf("%+v with an explicitly empty flag %b of another type was accepted", input, field)
					}
				}
			}
			if ValidInput(row.kind, Input{}) {
				t.Fatal("no flag at all was accepted")
			}
		})
	}
}

// A refusal names its Secret only by a valid object name, falls back to the
// command that lists the declared Secrets, and a validation refusal keeps its
// own code and message while gaining the Secret and the remedy.
func TestRefusalsNameTheSecretAndAlwaysANextStep(t *testing.T) {
	object := &diagnostics.ObjectIdentity{APIVersion: "bootwright.io/v1alpha1", Kind: "Secret", Name: "token"}
	for _, test := range []struct {
		name string
		err  error
		want diagnostics.Diagnostic
	}{
		{"named", Refusal("input", "refused", "lab", "token", "remedy"), diagnostics.Diagnostic{Severity: "error", Code: "secret.input", Message: "refused", Object: object, Remediation: "remedy"}},
		{"invalid name", Refusal("declaration", "refused", "lab", "Not A Name", "remedy"), diagnostics.Diagnostic{Severity: "error", Code: "secret.declaration", Message: "refused", Remediation: "remedy"}},
		{"no remedy", Refusal("input", "refused", "lab", "token", ""), diagnostics.Diagnostic{Severity: "error", Code: "secret.input", Message: "refused", Object: object, Remediation: "bootwright secret check --context lab"}},
		{"validation", Attribute(diagnostics.NewFailure("secret.part", "certificate is expired", ""), "lab", "token", "remedy"), diagnostics.Diagnostic{Severity: "error", Code: "secret.part", Message: "certificate is expired", Object: object, Remediation: "remedy"}},
		{"validation remedy", Attribute(diagnostics.NewFailureWithRemediation("secret.part", "certificate is expired", "", "its own"), "lab", "token", "remedy"), diagnostics.Diagnostic{Severity: "error", Code: "secret.part", Message: "certificate is expired", Object: object, Remediation: "its own"}},
		{"untyped validation", Attribute(errors.New("synthetic-cause-canary"), "lab", "token", "remedy"), diagnostics.Diagnostic{Severity: "error", Code: "secret.input", Message: "Secret token's stored material is invalid", Object: object, Remediation: "remedy"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if found := diagnostics.Of(test.err); !reflect.DeepEqual(found, []diagnostics.Diagnostic{test.want}) || diagnostics.IsUsage(test.err) {
				t.Fatalf("refusal = %+v, want %+v", found, test.want)
			}
		})
	}
	if err := UsageRefusal("input", "refused", "lab", "token", "remedy"); !diagnostics.IsUsage(err) || !diagnostics.IsUsage(fmt.Errorf("wrapped: %w", err)) {
		t.Fatal("a usage refusal is not marked as one")
	}
	if err := Attribute(context.Canceled, "lab", "token", "remedy"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation became %v", err)
	}
	generated := Declaration{Name: "token", Type: "token", Source: "generated"}
	stored := Declaration{Name: "pull", Type: "dockerConfigJson", Source: "contextStore"}
	for _, test := range []struct{ got, want string }{
		{Remedy("lab", generated, false), "bootwright secret generate --context lab --name token"},
		{Remedy("lab", generated, true), "bootwright secret generate --context lab --name token --renew"},
		{Remedy("lab", stored, true), "bootwright secret set --context lab --name pull --value-file <path>"},
		{Remedy("", stored, false), "bootwright secret set --name pull --value-file <path>"},
	} {
		if test.got != test.want {
			t.Errorf("remedy %q, want %q", test.got, test.want)
		}
	}
}
