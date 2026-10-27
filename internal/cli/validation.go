package cli

import (
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var untilObject = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func validateInvocation(command *cobra.Command, path string) string {
	flags := command.Flags()
	var spec commandSpec
	for _, candidate := range commandCatalog() {
		if candidate.path == path {
			spec = candidate
			break
		}
	}
	if hasPayload(command) {
		if len(flags.Args()) == 0 || flags.Args()[0] == "" {
			return "a non-empty command argument vector is required"
		}
	} else if len(flags.Args()) != 0 {
		return "this command accepts no positional operands"
	}
	if message := validateFlagValues(flags, path, spec); message != "" {
		return payloadFlagRemedy(command, message)
	}
	if message := validateSharedFlags(command, path); message != "" {
		return message
	}
	return validateCommandFlags(command, path)
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

func validateFlagValues(flags *pflag.FlagSet, path string, spec commandSpec) string {
	for _, flag := range append(globalFlags(), spec.flags...) {
		parsed := flags.Lookup(flag.name)
		if parsed == nil {
			continue
		}
		if value, ok := parsed.Value.(*scalarValue); ok && value.hasEmptyOccurrence && !allowsEmpty(path, flag.name) {
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

func validateSharedFlags(command *cobra.Command, path string) string {
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
	if SessionCommand(path) && selectedStructured(command) {
		return "live SSH sessions require --output text"
	}
	if boolValue(flags, "ssh-ask-sudo-password") && selectedJSON(command) {
		return "--ssh-ask-sudo-password conflicts with " + structuredLabel(command) + " output"
	}
	if flags.Lookup("until") != nil && flags.Changed("until") && !untilObject.MatchString(stringValue(flags, "until")) {
		return "--until must name an add-on instance as <cluster>.<package>"
	}
	for _, name := range []string{"clusters", "machines", "replace", "stage"} {
		if flags.Lookup(name) == nil {
			continue
		}
		raw := stringValue(flags, name)
		normalized := normalizeNames(raw)
		if name == "clusters" && len(normalized) == 0 && strings.TrimSpace(raw) != "" {
			if path != "machine list" {
				return "--clusters must not contain only separators"
			}
			normalizeScalar(flags, name, ",")
		} else {
			normalizeScalar(flags, name, strings.Join(normalized, ","))
		}
	}
	if strings.HasPrefix(path, "context ") && flags.Lookup("name") != nil && !dnsLabel(stringValue(flags, "name")) {
		return "--name must be a lowercase DNS label"
	}
	return ""
}

func validateMachineNames(flags *pflag.FlagSet, path string) string {
	raw := stringValue(flags, "name")
	if path == "machine rsh" {
		if strings.Contains(raw, ",") {
			return "machine rsh opens one interactive session; name one machine"
		}
		return ""
	}
	names := normalizeNames(raw)
	if len(names) == 0 {
		return "--name must name at least one machine"
	}
	normalizeScalar(flags, "name", strings.Join(names, ","))
	return ""
}

func validateCommandFlags(command *cobra.Command, path string) string {
	flags := command.Flags()
	switch path {
	case "machine rsh", "machine exec", "machine start", "machine stop", "machine restart":
		if message := validateMachineNames(flags, path); message != "" {
			return message
		}
	case "cluster rsh", "cluster exec", "cluster info":
		if message := validateClusterNames(flags, path); message != "" {
			return message
		}
	case "machine info", "secret info":
		if message := validateInfoNames(flags, path); message != "" {
			return message
		}
	}
	switch path {
	case "context init", "context update":
		if len(arrayValue(flags, "file")) > 1 {
			return "at most one --file occurrence is allowed"
		}
		if path == "context update" && len(arrayValue(flags, "file")) == 0 && stringValue(flags, "input-dir") == "" {
			return "context update requires --file or --input-dir"
		}
	case "setup":
		return validateSetupFlags(flags)
	case "context delete":
		if !boolValue(flags, "purge") {
			return "context delete requires --purge=true"
		}
	case "apply", "destroy":
		seen := map[string]bool{}
		for _, occurrence := range arrayValue(flags, "authorize") {
			for _, token := range strings.Split(occurrence, ",") {
				token = strings.TrimSpace(token)
				secret, rebind := strings.CutPrefix(token, lifecycle.RebindAuthorization)
				name, digest, bound := strings.Cut(token, "=")
				if rebind {
					name = token
				}
				dataLoss := name == "data-loss" && (!bound || authorizationDigest(digest))
				if !dataLoss && (!rebind || !api.ValidLexical("name", secret)) || seen[name] {
					return "--authorize accepts data-loss or data-loss=<digest> and rebind=<secret>, each at most once"
				}
				seen[name] = true
			}
		}
		for _, occurrence := range arrayValue(flags, "unregistered") {
			for _, name := range strings.Split(occurrence, ",") {
				if strings.TrimSpace(name) == "" {
					return "--unregistered must not contain an empty member"
				}
			}
		}
	case "secret set":
		if message := validateSecretInput(flags); message != "" {
			return message
		}
	case "media add", "media delete":
		if !mediaName(stringValue(flags, "name")) {
			return "--name must be a portable installer-image basename ending in .iso"
		}
		if path == "media add" {
			if message := validateMediaSource(flags); message != "" {
				return message
			}
		}
	case "render":
		return validateRenderDirectories(flags)
	case "machine list":
		if boolValue(flags, "silent") && selectedJSON(command) {
			return "--silent conflicts with " + structuredLabel(command) + " output"
		}
		// A silent invocation carries names alone, so a reading it could never
		// print would contact every management controller for nothing.
		if boolValue(flags, "silent") && boolValue(flags, "power-status") {
			return "--silent conflicts with --power-status"
		}
	case "machine stop", "machine restart":
		if selectedJSON(command) && !boolValue(flags, "yes") {
			return structuredLabel(command) + " power changes require --yes"
		}
	case "machine trust":
		return validateMachineTrust(command, flags)
	}
	return ""
}

func validateMachineTrust(command *cobra.Command, flags *pflag.FlagSet) string {
	if selectedJSON(command) && !boolValue(flags, "yes") && !boolValue(flags, "dry-run") {
		return structuredLabel(command) + " trust changes require --yes or --dry-run"
	}
	machines := normalizeNames(stringValue(flags, "machines"))
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
				return "every --replace name must belong to --machines"
			}
			if named {
				expected[name] = fingerprint
			}
		}
	}
	return ""
}

var hostKeyFingerprint = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)

func validateSetupFlags(flags *pflag.FlagSet) string {
	download := boolValue(flags, "download-only")
	switch {
	case download && boolValue(flags, "dry-run"):
		return "--download-only conflicts with --dry-run; setup --dry-run --output json lists the locked files"
	case download && stringValue(flags, "dest-dir") == "":
		return "--download-only requires --dest-dir"
	case !download && flags.Changed("dest-dir"):
		return "--dest-dir requires --download-only"
	}
	if _, err := prerequisites.ParseWheelMirror(stringValue(flags, "wheel-mirror")); err != nil {
		return "--wheel-mirror must be " + prerequisites.WheelMirrorUsage
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
		return "context-backed render requires --sensitive"
	}
	return ""
}

func allowsEmpty(path, name string) bool {
	return name == "context" || name == "sha256" || name == "clusters" || name == "machines" || name == "replace"
}

func dnsLabel(value string) bool {
	return api.ValidLexical("name", value)
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

func mediaName(value string) bool {
	if len(value) < 5 || len(value) > media.MaxMediaName || !strings.HasSuffix(value, ".iso") {
		return false
	}
	stem := strings.TrimSuffix(value, ".iso")
	if !regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`).MatchString(stem) {
		return false
	}
	return !regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])$`).MatchString(stem)
}

func validateMediaSource(flags *pflag.FlagSet) string {
	file, sourceURL, urlFile := stringValue(flags, "from-file"), stringValue(flags, "from-url"), stringValue(flags, "from-url-file")
	if given := len(slices.DeleteFunc([]string{file, sourceURL, urlFile}, func(v string) bool { return v == "" })); given != 1 {
		return "media add requires exactly one of --from-file, --from-url and --from-url-file"
	}
	digest := stringValue(flags, "sha256")
	if digest != "" {
		digest = strings.TrimPrefix(digest, "sha256:")
		if !regexp.MustCompile(`^[A-Fa-f0-9]{64}$`).MatchString(digest) {
			return "--sha256 must contain a verifiable SHA-256 digest"
		}
		normalizeScalar(flags, "sha256", strings.ToLower(digest))
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
	origin := &url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: parsed.Path, RawPath: parsed.RawPath}
	if len(origin.String()) > media.MaxMediaOrigin {
		return "--from-url must name a scheme, host and path of at most " + strconv.Itoa(media.MaxMediaOrigin) + " bytes; its query is never recorded"
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

func authorizationDigest(value string) bool {
	if len(value) != 16 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
