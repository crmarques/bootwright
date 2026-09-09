package cli

import (
	"net/url"
	"regexp"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

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
		if len(flag.enum) > 0 && !contains(flag.enum, value) {
			return "--" + flag.name + " has an unsupported value"
		}
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
	if boolValue(flags, "ssh-ask-sudo-password") && selectedJSON(command) {
		return "--ssh-ask-sudo-password conflicts with JSON output"
	}
	for _, name := range []string{"clusters", "machines", "replace"} {
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
	switch path {
	case "context init", "context update":
		if len(arrayValue(flags, "file")) != 1 {
			return "exactly one --file occurrence is required"
		}
	case "context delete":
		if !boolValue(flags, "purge") {
			return "context delete requires --purge=true"
		}
	case "apply", "destroy":
		seen := false
		for _, occurrence := range arrayValue(flags, "authorize") {
			for _, token := range strings.Split(occurrence, ",") {
				if strings.TrimSpace(token) != "data-loss" || seen {
					return "--authorize accepts data-loss exactly once"
				}
				seen = true
			}
		}
	case "add-ons add", "add-ons delete":
		name, version, combined := strings.Cut(stringValue(flags, "name"), ":")
		if name == "" || strings.ContainsAny(name, "/\\ \t\r\n") || strings.ContainsAny(version, "/\\ \t\r\n") || (combined && (version == "" || strings.Contains(version, ":"))) {
			return "--name must identify an add-on and optional version"
		}
		if strings.ContainsAny(stringValue(flags, "version"), "/\\ \t\r\n:") {
			return "--version must identify a catalog release"
		}
		if combined && stringValue(flags, "version") != "" {
			return "an inline add-on version conflicts with --version"
		}
	case "secret set":
		if message := validateSecretInput(flags); message != "" {
			return message
		}
	case "secret encryption init":
		if !api.ValidLexical("name", stringValue(flags, "type")) {
			return "--type must be a lowercase DNS label"
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
	case "status":
		interval, err := time.ParseDuration(stringValue(flags, "watch-interval"))
		if err != nil {
			return "--watch-interval must be a Go duration"
		}
		if boolValue(flags, "watch") {
			if selectedJSON(command) {
				return "--watch conflicts with JSON output"
			}
			if interval <= 0 {
				normalizeScalar(flags, "watch-interval", "5s")
			}
		}
	case "render":
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
	case "machine list":
		if boolValue(flags, "silent") && selectedJSON(command) {
			return "--silent conflicts with JSON output"
		}
	case "machine trust":
		if selectedJSON(command) && !boolValue(flags, "yes") && !boolValue(flags, "dry-run") {
			return "JSON trust changes require --yes or --dry-run"
		}
		machines := normalizeNames(stringValue(flags, "machines"))
		if len(machines) != 0 {
			for _, replacement := range normalizeNames(stringValue(flags, "replace")) {
				if !contains(machines, replacement) {
					return "every --replace name must belong to --machines"
				}
			}
		}
	case "cluster info":
		if boolValue(flags, "secrets") && stringValue(flags, "name") == "" {
			return "--secrets requires an explicit --name"
		}
	}
	return ""
}

func allowsEmpty(path, name string) bool {
	return name == "context" || name == "sha256" || name == "clusters" || name == "machines" || name == "replace" || (path == "add-ons add" && name == "version")
}

func dnsLabel(value string) bool {
	return len(value) <= 63 && regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`).MatchString(value)
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

func validateSecretInput(flags *pflag.FlagSet) string {
	valueFile, valueStdin := stringValue(flags, "value-file") != "", boolValue(flags, "value-stdin")
	username := stringValue(flags, "username") != ""
	passwordFile, passwordStdin := stringValue(flags, "password-file") != "", boolValue(flags, "password-stdin")
	certificate := stringValue(flags, "certificate-file") != ""
	privateKey, publicKey := stringValue(flags, "private-key-file") != "", stringValue(flags, "public-key-file") != ""

	valueInput := valueFile != valueStdin && !username && !passwordFile && !passwordStdin && !certificate && !privateKey && !publicKey
	passwordInput := username && passwordFile != passwordStdin && !valueFile && !valueStdin && !certificate && !privateKey && !publicKey
	certificateInput := certificate && !valueFile && !valueStdin && !username && !passwordFile && !passwordStdin && !publicKey
	privateKeyInput := privateKey && !certificate && !valueFile && !valueStdin && !username && !passwordFile && !passwordStdin
	if valueInput || passwordInput || certificateInput || privateKeyInput {
		return ""
	}
	return "secret set requires exactly one type-specific input shape"
}

func mediaName(value string) bool {
	if len(value) < 5 || len(value) > 255 || !strings.HasSuffix(value, ".iso") {
		return false
	}
	stem := strings.TrimSuffix(value, ".iso")
	if !regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`).MatchString(stem) {
		return false
	}
	return !regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])$`).MatchString(stem)
}

func validateMediaSource(flags *pflag.FlagSet) string {
	file, sourceURL := stringValue(flags, "from-file"), stringValue(flags, "from-url")
	if (file == "") == (sourceURL == "") {
		return "media add requires exactly one of --from-file and --from-url"
	}
	digest := stringValue(flags, "sha256")
	if digest != "" {
		digest = strings.TrimPrefix(digest, "sha256:")
		if !regexp.MustCompile(`^[A-Fa-f0-9]{64}$`).MatchString(digest) {
			return "--sha256 must contain a verifiable SHA-256 digest"
		}
		normalizeScalar(flags, "sha256", strings.ToLower(digest))
	}
	if sourceURL != "" {
		if digest == "" {
			return "--from-url requires --sha256"
		}
		parsed, err := url.Parse(sourceURL)
		if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return "--from-url must be an HTTP or HTTPS URL without userinfo"
		}
	}
	return ""
}
