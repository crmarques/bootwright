package cli

import (
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var untilObject = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func validateInvocation(command *cobra.Command, path string) string {
	flags := command.Flags()
	spec := specFor(path)
	if spec.operandCheck != nil {
		if err := spec.operandCheck(flags.Args()); err != nil {
			return err.Error()
		}
	} else if hasPayload(command) {
		if len(flags.Args()) == 0 || flags.Args()[0] == "" {
			return "a non-empty command argument vector is required"
		}
	} else if len(flags.Args()) != 0 {
		if spec.operandRemedy != nil {
			if remedy := spec.operandRemedy(flags); remedy != "" {
				return remedy
			}
		}
		return "this command accepts no positional operands"
	}
	if message := validateFlagValues(flags, spec); message != "" {
		return payloadFlagRemedy(command, message)
	}
	if message := validateSharedFlags(command, spec); message != "" {
		return message
	}
	if spec.validate != nil {
		return spec.validate(command)
	}
	return ""
}

func payloadFlagRemedy(command *cobra.Command, message string) string {
	name, ok := strings.CutSuffix(strings.TrimPrefix(message, "--"), " is required")
	if !ok || !hasPayload(command) {
		return message
	}
	for _, operand := range command.Flags().Args() {
		if operand == "--"+name || strings.HasPrefix(operand, "--"+name+"=") {
			return message + "; Bootwright flags go before the remote command"
		}
	}
	return message
}

func validateFlagValues(flags *pflag.FlagSet, spec commandSpec) string {
	for _, flag := range append(globalFlags(), spec.flags...) {
		parsed := flags.Lookup(flag.name)
		if parsed == nil {
			continue
		}
		if value, ok := parsed.Value.(*scalarValue); ok && value.hasEmptyOccurrence && !flag.emptyOK {
			return "--" + flag.name + " must not contain an empty occurrence"
		}
		if flag.kind == "stringArray" {
			values := arrayValue(flags, flag.name)
			for _, value := range values {
				if value == "" {
					return "--" + flag.name + " must not contain an empty occurrence"
				}
			}
			if flag.required && len(values) == 0 {
				return "--" + flag.name + " is required"
			}
			continue
		}
		value := stringValue(flags, flag.name)
		if flag.required && value == "" {
			return "--" + flag.name + " is required"
		}
		if flag.enumList {
			if message := validateEnumList(flag, normalizeNames(value), flags.Changed(flag.name)); message != "" {
				return message
			}
			continue
		}
		if len(flag.enum) > 0 && !contains(flag.enum, value) {
			return "--" + flag.name + " must be one of: " + strings.Join(flag.enum, ", ")
		}
	}
	return ""
}

func validateSharedFlags(command *cobra.Command, spec commandSpec) string {
	flags := command.Flags()
	width, err := strconv.Atoi(stringValue(flags, "width"))
	if err != nil || width != 0 && (width < 24 || width > 500) {
		return "--width must be 0 or an integer from 24 through 500"
	}
	if contextName := stringValue(flags, "context"); contextName != "" && !dnsLabel(contextName) {
		return "--context must be a lowercase DNS label"
	}
	if flags.Changed("ssh-user") {
		user := strings.TrimSpace(stringValue(flags, "ssh-user"))
		if !regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`).MatchString(user) {
			return "--ssh-user must be a POSIX account name"
		}
		normalizeScalar(flags, "ssh-user", user)
	}
	if boolValue(flags, "ssh-user-for-provisioned") && stringValue(flags, "ssh-user") == "" {
		return "--ssh-user-for-provisioned requires --ssh-user"
	}
	if boolValue(flags, "ssh-ask-password") {
		if stringValue(flags, "ssh-user") == "" {
			return "--ssh-ask-password requires --ssh-user"
		}
		if flags.Changed("ssh-id-file") {
			return "--ssh-ask-password conflicts with --ssh-id-file"
		}
	}
	if spec.session && selectedStructured(command) {
		return "live SSH sessions require --output text"
	}
	if boolValue(flags, "ssh-ask-sudo-password") && selectedJSON(command) {
		return "--ssh-ask-sudo-password conflicts with " + structuredLabel(command) + " output"
	}
	if flags.Lookup("until") != nil && flags.Changed("until") && !untilObject.MatchString(stringValue(flags, "until")) {
		return "--until must name an add-on instance as <cluster>.<package>"
	}
	for _, flag := range spec.flags {
		if !flag.nameList || flags.Lookup(flag.name) == nil {
			continue
		}
		raw := stringValue(flags, flag.name)
		names := normalizeNames(raw)
		if flag.name == "clusters" && len(names) == 0 && strings.TrimSpace(raw) != "" {
			if !flag.blankOK {
				return "--clusters must not contain only separators"
			}
			normalizeScalar(flags, flag.name, ",")
		} else {
			normalizeScalar(flags, flag.name, strings.Join(names, ","))
		}
	}
	return ""
}

func validateMachineTrust(command *cobra.Command, flags *pflag.FlagSet) string {
	if selectedJSON(command) && !boolValue(flags, "yes") && !boolValue(flags, "dry-run") {
		return structuredLabel(command) + " trust changes require --yes or --dry-run"
	}
	machines := normalizeNames(stringValue(flags, "name"))
	expected := map[string]string{}
	for _, flag := range []string{"replace", "expect"} {
		for _, member := range normalizeNames(stringValue(flags, flag)) {
			name, fingerprint, named := strings.Cut(member, "=")
			switch {
			case flag == "expect" && !named, named && !hostKeyFingerprint.MatchString(fingerprint):
				return "--" + flag + " names a fingerprint as <machine>=SHA256:<fingerprint>"
			case named && expected[name] != "" && expected[name] != fingerprint:
				return "--expect and --replace name two fingerprints for " + name
			case flag == "replace" && len(machines) != 0 && !contains(machines, name):
				return "every --replace name must belong to --name"
			}
			if named {
				expected[name] = fingerprint
			}
		}
	}
	return ""
}

var hostKeyFingerprint = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)

func validateUninstallFlags(flags *pflag.FlagSet) string {
	if boolValue(flags, "discard-unreadable") && boolValue(flags, "keep-packages") {
		return "--discard-unreadable conflicts with --keep-packages; a discard removes no package"
	}
	return ""
}

func validateRenderDirectories(flags *pflag.FlagSet) string {
	input, output := stringValue(flags, "input-dir"), stringValue(flags, "output-dir")
	if input != "" {
		if output == "" {
			return "--input-dir requires --output-dir"
		}
		if stringValue(flags, "context") != "" {
			return "--input-dir conflicts with an explicit context"
		}
		if boolValue(flags, "sensitive") {
			return "--input-dir conflicts with --sensitive"
		}
	} else if output != "" && !boolValue(flags, "sensitive") {
		return "context-backed --output-dir requires --sensitive"
	} else if output == "" && boolValue(flags, "sensitive") {
		return "--sensitive requires --output-dir"
	}
	return ""
}

func dnsLabel(value string) bool {
	return api.ValidLexical(api.LexicalName, value)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func normalizeNames(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" && !contains(result, item) {
			result = append(result, item)
		}
	}
	return result
}

// validateSecretInput refuses only the alternatives no Secret type takes
// together. Which flags a Secret takes depends on its declared type, so the
// custody refuses every other flag set once it has read the declaration.
func validateSecretInput(flags *pflag.FlagSet) string {
	valueFile, valueStdin := stringValue(flags, "value-file") != "", boolValue(flags, "value-stdin")
	passwordFile, passwordStdin := stringValue(flags, "password-file") != "", boolValue(flags, "password-stdin")
	switch {
	case valueFile && valueStdin:
		return "--value-file conflicts with --value-stdin"
	case passwordFile && passwordStdin:
		return "--password-file conflicts with --password-stdin"
	case valueStdin && passwordStdin:
		return "--value-stdin conflicts with --password-stdin"
	}
	return ""
}

func validateMediaSource(flags *pflag.FlagSet) string {
	file, sourceURL, urlFile := stringValue(flags, "from-file"), stringValue(flags, "from-url"), stringValue(flags, "from-url-file")
	if given := len(slices.DeleteFunc([]string{file, sourceURL, urlFile}, func(v string) bool { return v == "" })); given != 1 {
		return "media add requires exactly one of --from-file, --from-url and --from-url-file"
	}
	digest := stringValue(flags, "sha256")
	if digest != "" {
		normalized, valid := media.NormalizeDigest(digest)
		if !valid || normalized == "" {
			return "--sha256 must contain a verifiable SHA-256 digest"
		}
		digest = normalized
		normalizeScalar(flags, "sha256", digest)
	}
	if file != "" {
		return validateMediaFile(file)
	}
	if digest == "" {
		return "a download requires --sha256"
	}
	if urlFile != "" {
		if strings.ContainsFunc(urlFile, controlCharacter) {
			return "--from-url-file must not contain a control character"
		}
		return ""
	}
	if strings.ContainsFunc(sourceURL, controlCharacter) {
		return "--from-url must not contain a control character"
	}
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed.Scheme != "https" || parsed.Opaque != "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return "--from-url must be an HTTPS URL without userinfo or fragment"
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return "--from-url must not carry a query; pass a signed URL through --from-url-file <path|->"
	}
	origin := &url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: parsed.Path, RawPath: parsed.RawPath}
	if len(origin.String()) > media.MaxMediaOrigin {
		return "--from-url must name a scheme, host and path of at most " + strconv.Itoa(media.MaxMediaOrigin) + " bytes"
	}
	return ""
}

// validateMediaFile refuses a path the media record could not carry as its
// file:// origin. A relative path is checked by the media store once it is
// made absolute.
func validateMediaFile(file string) string {
	if strings.ContainsFunc(file, controlCharacter) {
		return "--from-file must not contain a control character"
	}
	if path := strings.TrimSpace(file); filepath.IsAbs(path) && len("file://"+filepath.ToSlash(filepath.Clean(path))) > media.MaxMediaOrigin {
		return "--from-file must name a path whose file:// URL is at most " + strconv.Itoa(media.MaxMediaOrigin) + " bytes"
	}
	return ""
}

// controlCharacter is a character a media record's origin never carries.
func controlCharacter(c rune) bool { return c < 0x20 || c == 0x7f }

// validateEnumList checks each member of a comma-separated enum list. An
// omitted flag selects the command's documented default; a supplied value that
// resolves to no member is a usage error rather than a silent empty selection.
func validateEnumList(flag flagSpec, members []string, supplied bool) string {
	if !supplied {
		return ""
	}
	if len(members) == 0 {
		return "--" + flag.name + " must select at least one value"
	}
	for _, member := range members {
		if !contains(flag.enum, member) {
			return "--" + flag.name + " accepts " + strings.Join(flag.enum, ", ")
		}
	}
	return ""
}

func structuredLabel(command *cobra.Command) string {
	if stringValue(command.Flags(), "output") == "yaml" {
		return "YAML"
	}
	return "JSON"
}
