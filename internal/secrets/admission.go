package secrets

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func Normalize(object api.Object, _ api.Catalog) (api.Object, []api.Issue) {
	if object.Kind() != api.Secret {
		return object, nil
	}
	spec := object.Spec()
	source := spec.Get("source")
	if source.Present() && source.Len() == 0 {
		source = api.MapValue(api.FieldValue{Name: "contextStore", Value: api.MapValue()})
	}
	if generated := source.Get("generated"); generated.Present() {
		switch spec.Get("type").Text() {
		case "usernamePassword":
			generated = generated.Default("username", api.StringValue("admin"))
		case "tlsCertificate", "caBundle":
			generated = generated.Default("validityDays", api.IntegerValue("3650"))
		case "sshKeyPair":
			generated = generated.Default("keyType", api.StringValue("ed25519"))
		}
		source = source.With("generated", generated)
	}
	if source.Present() {
		spec = spec.With("source", source)
	}
	return object.WithSpec(spec), nil
}

func ValidateAuthored(object api.Object, catalog api.Catalog) []api.Issue {
	return validate(object, true, false)
}
func ValidatePartial(object api.Object, catalog api.Catalog) []api.Issue {
	return validate(object, true, true)
}
func Validate(object api.Object, catalog api.Catalog) []api.Issue {
	return validate(object, false, false)
}

func validate(object api.Object, partial, kindDefault bool) []api.Issue {
	if object.Kind() != api.Secret {
		return nil
	}
	issues := []api.Issue{}
	add := func(field, message string) {
		issues = append(issues, api.Issue{Code: "api.invariant", Field: "$.spec." + field, Message: message})
	}
	spec := object.Spec()
	kind := spec.Get("type").Text()
	source := spec.Get("source")
	if source.Get("file").Present() {
		issues = append(issues, api.Issue{Code: "api.field", Field: "$.spec.source.file",
			Message:     "the Secret file source is retired: Secret material is read only from context custody, which bootwright secret set loads",
			Remediation: fileSourceRemedy(object.Name(), kind, kindDefault)})
	}
	if kind == "" {
		return issues
	}
	if generated := source.Get("generated"); generated.Present() {
		allowed := []string{}
		switch kind {
		case "token":
			allowed = []string{"bytes"}
		case "usernamePassword":
			allowed = []string{"username"}
		case "tlsCertificate", "caBundle":
			allowed = []string{"commonName", "dnsNames", "ipAddresses", "validityDays"}
			if !partial && !generated.Has("commonName") {
				add("source.generated.commonName", "generated certificates require a common name")
			}
		case "sshKeyPair":
			allowed = []string{"keyType", "comment"}
		case "opaque", "dockerConfigJson":
			add("source.generated", "the declared Secret type does not permit generation")
		default:
			return issues
		}
		for _, field := range generated.Fields() {
			if !slices.Contains(allowed, field.Name) {
				add("source.generated."+field.Name, "generation parameter is not accepted by the declared Secret type")
			}
		}
		if value := generated.Get("username"); value.Present() && !generableUsername(value.Text()) {
			add("source.generated.username", "generated username must be UTF-8 within the part byte limit and must not contain whitespace, colon or NUL")
		}
		if value := generated.Get("comment"); value.Present() && !generableComment(value.Text()) {
			add("source.generated.comment", "SSH comment must be one UTF-8 line within the part byte limit, without surrounding whitespace, NUL or another control character, a Unicode line or paragraph separator, a double quote or a backslash")
		}
	}
	return issues
}

// generableUsername and generableComment repeat what generation refuses, so a
// declaration it cannot honour is refused at validate instead. Material
// imports this package and pins the agreement.
func generableUsername(value string) bool {
	return len(value) <= MaxPartBytes && utf8.ValidString(value) && !strings.ContainsAny(value, ":\x00") && strings.IndexFunc(value, unicode.IsSpace) < 0
}

func generableComment(value string) bool {
	return len(value) <= MaxPartBytes && strings.TrimSpace(value) == value && SSHComment(value)
}

// SSHComment reports whether value can be an SSH public key's comment. The
// fleet key's public half is written inside a double-quoted Kickstart value,
// which Anaconda splits on every line break str.splitlines knows, so a
// control character, a Unicode line or paragraph separator, a double quote
// or a backslash would end that line or its quoting.
func SSHComment(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsAny(value, `"\`) && strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
	}) < 0
}

func fileSourceRemedy(name, kind string, kindDefault bool) string {
	flags := setFileFlags(kind)
	if kindDefault {
		remedy := "replace source.file in the Environment's Secret kind default with source: {contextStore: {}}, then for each Secret that inherits it run bootwright secret set --name <name> with the file flags of its type"
		if flags != "" {
			remedy += " (" + flags + " for " + kind + ")"
		}
		return remedy
	}
	if !api.ValidLexical("name", name) {
		name = "<name>"
	}
	if flags == "" {
		flags = "with the file flags of its type"
	}
	return "declare source: {contextStore: {}} and run bootwright secret set --name " + name + " " + flags
}

func setFileFlags(kind string) string {
	switch kind {
	case "opaque", "token", "dockerConfigJson":
		return "--value-file <path>"
	case "usernamePassword":
		return "--username <username> --password-file <path>"
	case "caBundle":
		return "--certificate-file <path>"
	case "tlsCertificate":
		return "--certificate-file <path> --private-key-file <path>"
	case "sshKeyPair":
		return "--private-key-file <path> [--public-key-file <path>]"
	}
	return ""
}
