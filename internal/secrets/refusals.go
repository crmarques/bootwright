package secrets

import (
	"context"
	"errors"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// inputShape is what secret set takes for one Secret type: the flags it
// admits, the shape a refusal names, the flags a remedy shows and the flags
// the retired file source's remedy names.
type inputShape struct {
	fields InputFields
	takes  string
	remedy string
	file   string
}

func shapeOf(kind string) (inputShape, bool) {
	switch kind {
	case "opaque", "token", "dockerConfigJson":
		return inputShape{ValueFileInput | ValueStdinInput, "--value-file <path> or --value-stdin", "--value-file <path>", "--value-file <path>"}, true
	case "usernamePassword":
		return inputShape{UsernameInput | PasswordFileInput | PasswordStdinInput, "--username <username> with --password-file <path> or --password-stdin", "--username <username> --password-stdin", "--username <username> --password-file <path>"}, true
	case "caBundle":
		return inputShape{CertificateFileInput, "--certificate-file <path>", "--certificate-file <path>", "--certificate-file <path>"}, true
	case "tlsCertificate":
		flags := "--certificate-file <path> --private-key-file <path>"
		return inputShape{CertificateFileInput | PrivateKeyFileInput, flags, flags, flags}, true
	case "sshKeyPair":
		flags := "--private-key-file <path> [--public-key-file <path>]"
		return inputShape{PrivateKeyFileInput | PublicKeyFileInput, flags, flags, flags}, true
	}
	return inputShape{}, false
}

// ValidInput reports whether a flag set fits a Secret type: an explicitly
// supplied inapplicable flag refuses even when it is empty or false.
func ValidInput(kind string, input Input) bool {
	shape, known := shapeOf(kind)
	if !known || input.Provided&^shape.fields != 0 {
		return false
	}
	value := (input.ValueFile != "") != input.ValueStdin
	password := (input.PasswordFile != "") != input.PasswordStdin
	noValue := input.ValueFile == "" && !input.ValueStdin
	noPassword := input.Username == "" && input.PasswordFile == "" && !input.PasswordStdin
	noKeys := input.CertificateFile == "" && input.PrivateKeyFile == "" && input.PublicKeyFile == ""
	switch kind {
	case "opaque", "token", "dockerConfigJson":
		return value && noPassword && noKeys
	case "usernamePassword":
		return password && input.Username != "" && noValue && noKeys
	case "caBundle":
		return input.CertificateFile != "" && input.PrivateKeyFile == "" && input.PublicKeyFile == "" && noValue && noPassword
	case "tlsCertificate":
		return input.CertificateFile != "" && input.PrivateKeyFile != "" && input.PublicKeyFile == "" && noValue && noPassword
	case "sshKeyPair":
		return input.PrivateKeyFile != "" && input.CertificateFile == "" && noValue && noPassword
	}
	return false
}

// CheckInput refuses, as a mistake in how secret set was invoked, a flag set
// that does not fit the declared type and a --username no username part holds.
func CheckInput(contextName string, d Declaration, input Input) error {
	shape, known := shapeOf(d.Type)
	if !known {
		return Refusal("declaration", "Secret "+d.Name+" has an unsupported type", contextName, d.Name, "")
	}
	remedy := setRemedy(contextName, d.Name, shape)
	if !ValidInput(d.Type, input) {
		return UsageRefusal("input", "Secret "+d.Name+" is of type "+d.Type+"; secret set takes "+shape.takes, contextName, d.Name, remedy)
	}
	if d.Type == "usernamePassword" && (input.Username == "" || !generableUsername(input.Username)) {
		return UsageRefusal("input", "Secret "+d.Name+" takes a --username that is one nonempty UTF-8 line of at most 1 MiB with no whitespace or colon", contextName, d.Name, remedy)
	}
	return nil
}

// Command is a secret command line bound to a context.
func Command(contextName, verb string) string {
	if contextName == "" {
		return "bootwright secret " + verb
	}
	return "bootwright secret " + verb + " --context " + contextName
}

// Remedy is the command that gives a declared Secret its material: generate
// for a generated Secret, which re-mints missing or stale material and, with
// renew, replaces material that is current but invalid; set with the flags of
// its type otherwise.
func Remedy(contextName string, d Declaration, renew bool) string {
	if d.Source == "generated" {
		remedy := Command(contextName, "generate") + " --name " + d.Name
		if renew {
			remedy += " --renew"
		}
		return remedy
	}
	shape, known := shapeOf(d.Type)
	if !known {
		return Command(contextName, "check")
	}
	return setRemedy(contextName, d.Name, shape)
}

func setRemedy(contextName, name string, shape inputShape) string {
	return Command(contextName, "set") + " --name " + name + " " + shape.remedy
}

// Refusal is one secret.<code> error about the Secret it names. A refusal
// without its own remedy points at secret check, which lists the declared
// Secrets and their state.
func Refusal(code, message, contextName, name, remedy string) error {
	return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{refusal("secret."+code, message, contextName, name, remedy)}}
}

// UsageRefusal is a Refusal of how the command was invoked.
func UsageRefusal(code, message, contextName, name, remedy string) error {
	return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{refusal("secret."+code, message, contextName, name, remedy)}, Usage: true}
}

// Attribute names the Secret whose stored material a validation refused, and
// fills each remedy the validation left empty. It never carries material.
func Attribute(err error, contextName, name, remedy string) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	found := diagnostics.Of(err)
	if len(found) == 0 {
		return Refusal("input", "Secret "+name+"'s stored material is invalid", contextName, name, remedy)
	}
	for i := range found {
		found[i].Object = secretObject(name)
		if found[i].Remediation == "" {
			found[i].Remediation = remedy
		}
	}
	return &diagnostics.Failure{Diagnostics: found}
}

func refusal(code, message, contextName, name, remedy string) diagnostics.Diagnostic {
	if remedy == "" {
		remedy = Command(contextName, "check")
	}
	return diagnostics.Diagnostic{Severity: "error", Code: code, Message: message, Object: secretObject(name), Remediation: remedy}
}

func secretObject(name string) *diagnostics.ObjectIdentity {
	if !api.ValidLexical("name", name) {
		return nil
	}
	return &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.Secret), Name: name}
}
