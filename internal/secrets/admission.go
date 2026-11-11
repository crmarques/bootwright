package secrets

import (
	"net/netip"
	"slices"
	"strconv"
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

func DeriveCertificateNames(object api.Object, catalog api.Catalog, consumers ...func(api.Catalog, string) []string) api.Object {
	generated := object.Spec().Get("source", "generated")
	if object.Kind() != api.Secret || object.Spec().Get("type").Text() != "tlsCertificate" || !generated.Present() || generated.Has("dnsNames") || generated.Has("ipAddresses") {
		return object
	}
	var names, addresses []string
	for _, consumer := range consumers {
		for _, name := range consumer(catalog, object.Name()) {
			if address, err := netip.ParseAddr(name); err == nil {
				addresses = append(addresses, address.String())
			} else if name != "" {
				names = append(names, strings.ToLower(name))
			}
		}
	}
	slices.Sort(names)
	slices.Sort(addresses)
	if names = slices.Compact(names); len(names) != 0 {
		generated = generated.With("dnsNames", api.StringList(names...))
	}
	if addresses = slices.Compact(addresses); len(addresses) != 0 {
		generated = generated.With("ipAddresses", api.StringList(addresses...))
	}
	return object.WithSpec(object.Spec().WithPath(generated, "source", "generated"))
}

func ValidateAuthored(object api.Object, catalog api.Catalog) []api.Issue {
	return validate(object, true)
}
func ValidatePartial(object api.Object, catalog api.Catalog) []api.Issue {
	return validate(object, true)
}
func Validate(object api.Object, catalog api.Catalog) []api.Issue {
	issues := validate(object, false)
	if ref := object.Spec().Get("source", "generated", "issuerRef").Text(); object.Kind() == api.Secret && ref != "" {
		issuer, found := catalog.Find(api.Secret, ref)
		if !found || issuer.Spec().Get("type").Text() != "caBundle" || !issuer.Spec().Has("source", "generated") || issuer.Spec().Has("source", "generated", "issuerRef") {
			issues = append(issues, api.Issue{Code: "api.invariant", Field: "$.spec.source.generated.issuerRef", Message: "issuerRef requires a generated caBundle without an issuer"})
		}
	}
	return issues
}

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
	source := spec.Get("source")
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
			if kind == "tlsCertificate" {
				allowed = append(allowed, "issuerRef")
			}
			if !partial && !generated.Has("commonName") {
				add("source.generated.commonName", "generated certificates require a common name")
			}
			if value := generated.Get("commonName"); value.Present() && !generableCommonName(value.Text()) {
				add("source.generated.commonName", "generated common name must be non-empty UTF-8 within the part byte limit and must not contain NUL")
			}
		case "sshKeyPair":
			allowed = []string{"keyType", "comment"}
		case "opaque", "dockerConfigJson", "trustBundle":
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

// generableUsername, generableCommonName and generableComment repeat what
// generation refuses, so a declaration it cannot honour is refused at validate
// instead. Material imports this package and pins the agreement.
func generableUsername(value string) bool {
	return len(value) <= MaxPartBytes && utf8.ValidString(value) && !strings.ContainsAny(value, ":\x00") && strings.IndexFunc(value, unicode.IsSpace) < 0
}

func generableCommonName(value string) bool {
	return value != "" && len(value) <= MaxPartBytes && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func generableComment(value string) bool {
	return len(value) <= MaxPartBytes && strings.TrimSpace(value) == value && SSHComment(value)
}

func GeneratedCertificateNames(generated api.Value, address string) bool {
	if ip, err := netip.ParseAddr(address); err == nil {
		for _, listed := range generated.Get("ipAddresses").Strings() {
			if san, err := netip.ParseAddr(listed); err == nil && san.Unmap() == ip.Unmap() {
				return true
			}
		}
		return false
	}
	for _, listed := range generated.Get("dnsNames").Strings() {
		if strings.EqualFold(listed, address) {
			return true
		}
	}
	return false
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

func ValidateTrust(object api.Object, catalog api.Catalog, exceptions func(api.Object, string) bool) []api.Issue {
	var issues []api.Issue
	var walk func(*api.Shape, api.Value, string)
	walk = func(shape *api.Shape, value api.Value, path string) {
		if shape == nil || !value.Present() {
			return
		}
		if shape.Element != nil && shape.Type == api.Sequence {
			for index, entry := range value.Items() {
				walk(shape.Element, entry, path+"["+strconv.Itoa(index)+"]")
			}
		}
		for _, field := range shape.Fields {
			current := value.Get(field.Name)
			switch field.Name {
			case "verification", "tlsVerification", "packageRepositoryTLSVerification":
				if !current.Present() {
					continue
				}
				mode := current.Text()
				refField := "trustBundleRef"
				if field.Name == "packageRepositoryTLSVerification" {
					refField = "packageRepositoryTrustBundleRef"
				}
				full := path + "." + field.Name
				add := func(message, remedy string) {
					issues = append(issues, api.Issue{Code: "api.invariant", Field: full, Message: message, Remediation: remedy})
				}
				allowsExceptions := exceptions != nil && exceptions(object, path)
				if mode == "certificate" && !value.Has(refField) {
					add("certificate verification requires an explicit server certificate trust bundle", "set "+path+"."+refField+" to a trustBundle Secret containing exactly one server certificate")
				}
				if mode == "certificate" {
					if secret, ok := catalog.Find(api.Secret, value.Get(refField).Text()); ok && secret.Spec().Get("type").Text() != "trustBundle" {
						add("certificate verification requires a trustBundle Secret containing a server certificate", "reference a contextStore trustBundle Secret instead of a CA-only or serving-key Secret")
					}
				}
				if mode == "none" && value.Has(refField) {
					add("disabled TLS verification cannot use a trust bundle", "remove "+path+"."+refField+" or select full or certificate verification")
				}
				if allowsExceptions && value.Has("verify") && value.Get("verify").Bool() != (mode != "none") {
					add("tls.verify and tls.verification disagree", "remove tls.verify and use tls.verification alone, or make both settings agree")
				}
				if !allowsExceptions && mode != "full" {
					add("this connection's consumer does not implement "+mode+" TLS verification", "select full verification and use a certificate matching this endpoint")
				}
			}
			walk(field.Shape, current, path+"."+field.Name)
		}
	}
	walk(api.Schema(object.Kind()), object.Spec(), "$.spec")
	return issues
}
