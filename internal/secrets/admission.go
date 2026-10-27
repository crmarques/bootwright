package secrets

import (
	"errors"
	"net/netip"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
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
	return validate(object, true, false)
}
func ValidatePartial(object api.Object, catalog api.Catalog) []api.Issue {
	return validate(object, true, true)
}
func Validate(object api.Object, catalog api.Catalog) []api.Issue {
	issues := validate(object, false, false)
	if ref := object.Spec().Get("source", "generated", "issuerRef").Text(); object.Kind() == api.Secret && ref != "" {
		issuer, found := catalog.Find(api.Secret, ref)
		if !found || issuer.Spec().Get("type").Text() != "caBundle" || !issuer.Spec().Has("source", "generated") || issuer.Spec().Has("source", "generated", "issuerRef") {
			issues = append(issues, api.Issue{Code: "api.invariant", Field: "$.spec.source.generated.issuerRef", Message: "issuerRef requires a generated caBundle without an issuer"})
		}
	}
	return issues
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

// FileSourceContextPlaceholder is the context flag a retired file-source
// remedy carries until the invocation that reports it names its context.
const FileSourceContextPlaceholder = "--context <context>"

const (
	FileSourceInit   = "context init"
	FileSourceUpdate = "context update"
)

type FileSourceRerun struct {
	Command           string
	ConfigurationFile bool
}

const (
	fileSourceDeclare = "declare source: {contextStore: {}} and run "
	fileSourceInherit = ", then for each Secret that inherits it run "
)

// NameFileSourceContext fills, in a copy of err, the context placeholder of
// each retired file-source remedy with contextName, after admitting's rerun.
func NameFileSourceContext(err error, contextName string, admitting FileSourceRerun) error {
	var failure *diagnostics.Failure
	if contextName == "" || !errors.As(err, &failure) || !slices.ContainsFunc(failure.Diagnostics, retiredFileSource) {
		return err
	}
	rerun := ""
	if admitting.Command == FileSourceInit || admitting.Command == FileSourceUpdate {
		rerun = "rerun bootwright " + admitting.Command + " --name " + contextName
		if admitting.ConfigurationFile {
			rerun += " -f <file>"
		}
		rerun += " --input-dir <dir>"
	}
	named := &diagnostics.Failure{Diagnostics: slices.Clone(failure.Diagnostics), Usage: failure.Usage}
	for index, d := range named.Diagnostics {
		if retiredFileSource(d) {
			remedy := strings.ReplaceAll(d.Remediation, FileSourceContextPlaceholder, "--context "+contextName)
			if rerun != "" {
				remedy = strings.Replace(remedy, fileSourceDeclare, "declare source: {contextStore: {}} in the input, "+rerun+", then run ", 1)
				remedy = strings.Replace(remedy, fileSourceInherit, ", "+rerun+fileSourceInherit, 1)
			}
			named.Diagnostics[index].Remediation = remedy
		}
	}
	return named
}

func retiredFileSource(d diagnostics.Diagnostic) bool {
	return (d.Field == "$.spec.source.file" || d.Field == "$.spec.defaults.Secret.source.file") &&
		strings.Contains(d.Remediation, FileSourceContextPlaceholder)
}

func fileSourceRemedy(name, kind string, kindDefault bool) string {
	flags := setFileFlags(kind)
	if kindDefault {
		remedy := "replace source.file in the Environment's Secret kind default with source: {contextStore: {}}" + fileSourceInherit +
			"bootwright secret set --name <name> --context <context> with the file flags of its type"
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
	return fileSourceDeclare + "bootwright secret set --name " + name + " --context <context> " + flags
}

func setFileFlags(kind string) string {
	shape, _ := shapeOf(kind)
	return shape.file
}
