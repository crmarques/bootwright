package secrets

import (
	"slices"
	"strings"
	"unicode"

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
	return validate(object, true)
}
func Validate(object api.Object, catalog api.Catalog) []api.Issue { return validate(object, false) }

func validate(object api.Object, partial bool) []api.Issue {
	if object.Kind() != api.Secret {
		return nil
	}
	issues := []api.Issue{}
	add := func(field, message string) {
		issues = append(issues, api.Issue{Code: "api.invariant", Field: "$.spec." + field, Message: message})
	}
	spec := object.Spec()
	kind := spec.Get("type").Text()
	if kind == "" {
		return nil
	}
	source := spec.Get("source")
	if file := source.Get("file"); file.Present() {
		var required, optional []string
		switch kind {
		case "opaque", "token", "usernamePassword", "dockerConfigJson", "caBundle":
			required = []string{"path"}
		case "tlsCertificate":
			required = []string{"cert", "key"}
		case "sshKeyPair":
			required = []string{"privateKey"}
			optional = []string{"publicKey"}
		default:
			return nil
		}
		for _, field := range required {
			if !partial && !file.Has(field) {
				add("source.file."+field, "Secret type requires this source path")
			}
		}
		for _, field := range file.Fields() {
			if !slices.Contains(required, field.Name) && !slices.Contains(optional, field.Name) {
				add("source.file."+field.Name, "source path is not accepted by the declared Secret type")
			}
		}
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
			return nil
		}
		for _, field := range generated.Fields() {
			if !slices.Contains(allowed, field.Name) {
				add("source.generated."+field.Name, "generation parameter is not accepted by the declared Secret type")
			}
		}
		if value := generated.Get("username"); value.Present() {
			if strings.ContainsRune(value.Text(), ':') || strings.IndexFunc(value.Text(), unicode.IsSpace) >= 0 {
				add("source.generated.username", "generated username must not contain whitespace or colon")
			}
		}
		if value := generated.Get("comment"); value.Present() {
			if strings.TrimSpace(value.Text()) != value.Text() || strings.ContainsAny(value.Text(), "\r\n") {
				add("source.generated.comment", "SSH comment must be one line without surrounding whitespace")
			}
		}
	}
	return issues
}
