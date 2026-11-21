package cli

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type commandInvoker func(Services, context.Context, *requestValues, []string) (commandResult, error)

type commandSpec struct {
	path     string
	short    string
	long     string
	flags    []flagSpec
	payload  bool
	operands string
	// implemented marks a command whose application use case is available.
	// available also marks commands that need the privileged context store.
	implemented      bool
	privileged       bool
	session          bool
	example          string
	contextFree      bool
	sshFlags         []string
	presentation     bool
	helpCommand      bool
	invoke           commandInvoker
	validate         commandValidator
	missingInput     func(*pflag.FlagSet) bool
	operandCheck     func([]string) (incomplete bool, problem string)
	operandRemedy    func(*pflag.FlagSet) string
	refuse           func(*Runner, *cobra.Command) string
	classify         classifyRules
	present          commandPresenter
	render           commandRenderer
	results          []jsonResult
	operation        bool
	controllerReport bool
	separateProgress bool
}

type classifyRules struct {
	localInput, rootless, ambient, urlStdin func(*pflag.FlagSet) bool
	unavailable, secretStdin                func(*pflag.FlagSet) string
}

var rootHelpGroups = []struct {
	title string
	names []string
}{
	{"Run the lifecycle", []string{"apply", "destroy"}},
	{"Inspect and troubleshoot", []string{"status", "logs", "diff", "explain"}},
	{"Prepare Bootwright and its inputs", []string{"install", "uninstall", "validate", "context", "catalog", "secret", "render"}},
	{"Recovery", []string{"rebind-host"}},
	{"Work with machines and clusters", []string{"machine", "cluster", "policy"}},
	{"CLI tools", []string{"version", "sbom", "completion", "help"}},
}

func SessionCommand(path string) bool { return specFor(path).session }

func available(spec commandSpec) commandSpec {
	spec.implemented, spec.privileged = true, true
	return spec
}

type flagSpec struct {
	name         string
	short        string
	help         string
	kind         string
	defaultValue string
	required     bool
	enum         []string
	// enumList marks a comma-separated list whose members come from enum, so
	// validation checks each member instead of the whole value.
	enumList bool
	path     string
	// undisclosed marks a value no output repeats.
	undisclosed bool
	bare        string
	// emptyOK marks a flag whose empty occurrence is meaningful, nameList a
	// comma-separated list the validation normalizes, and blankOK a list
	// that may hold only separators.
	emptyOK  bool
	nameList bool
	blankOK  bool
}

func stringFlag(name, help string) flagSpec { return flagSpec{name: name, help: help, kind: "string"} }

// undisclosedFlag is a string flag whose value no output repeats: a download
// URL, whose query may carry a token.
func undisclosedFlag(name, help string) flagSpec {
	f := stringFlag(name, help)
	f.undisclosed = true
	return f
}

// directoryFlag and fileFlag mark a path-valued flag so completion offers
// real filesystem candidates instead of refusing every value.
func directoryFlag(name, help string) flagSpec {
	f := stringFlag(name, help)
	f.path = "directory"
	return f
}

func fileFlag(name, help string) flagSpec {
	f := stringFlag(name, help)
	f.path = "file"
	return f
}

func optionalValueFlag(name, bare, help string) flagSpec {
	f := stringFlag(name, help)
	f.bare = bare
	return f
}

func boolFlag(name, help string) flagSpec {
	return flagSpec{name: name, help: help, kind: "bool", defaultValue: "false"}
}

func requiredFlag(name, help string) flagSpec {
	f := stringFlag(name, help)
	f.required = true
	return f
}

func enumFlag(name, value, help string, values ...string) flagSpec {
	f := stringFlag(name, help)
	f.defaultValue = value
	f.enum = values
	return f
}

func requiredEnumFlag(name, help string, values ...string) flagSpec {
	f := enumFlag(name, "", help, values...)
	f.required = true
	return f
}

func globalFlags() []flagSpec {
	return []flagSpec{
		outputFlag(),
		enumFlag("color", "auto", "Select terminal colors", "auto", "always", "never"),
		enumFlag("progress", "auto", "Select progress animation", "auto", "always", "never"),
		enumFlag("detail", "compact", "Select human detail", "compact", "full"),
		boolFlag("ascii", "Use ASCII status markers"),
		{name: "width", help: "Set presentation width (0: automatic)", kind: "string", defaultValue: "0"},
		emptyFlag("context", "Select a context (default: current context)"),
		stringFlag("ssh-id-file", "Offer an explicit private key for eligible SSH access"),
		stringFlag("ssh-user", "Borrow an explicit POSIX account for eligible SSH access"),
		boolFlag("ssh-ask-sudo-password", "Request the borrowed account's sudo password"),
		boolFlag("ssh-ask-password", "Prompt for the named SSH user's login password"),
		boolFlag("ssh-user-for-provisioned", "Extend the borrowed account to provisioned machines"),
		{name: "help", short: "h", help: "Show help for this command", kind: "bool", defaultValue: "false"},
	}
}

var (
	catalogOnce  sync.Once
	catalogCache []commandSpec
)

func commandCatalog() []commandSpec {
	catalogOnce.Do(func() { catalogCache = buildCommandCatalog() })
	return catalogCache
}

func buildCommandCatalog() []commandSpec {
	var catalog []commandSpec
	catalog = append(catalog, workspaceCommands()...)
	catalog = append(catalog, addOnCatalogCommands()...)
	catalog = append(catalog, secretCommands()...)
	catalog = append(catalog, mediaCommands()...)
	catalog = append(catalog, validationCommand())
	catalog = append(catalog, lifecycleInspectionCommands()...)
	catalog = append(catalog, effectiveStateCommand())
	catalog = append(catalog, acquisitionsCommand())
	catalog = append(catalog, installerRenderCommand())
	catalog = append(catalog, storageRenderCommand())
	catalog = append(catalog, lifecycleMutationCommands()...)
	catalog = append(catalog, machineCommands()...)
	catalog = append(catalog, policyCommands()...)
	catalog = append(catalog, machineTrustCommand())
	catalog = append(catalog, controllerSetupCommand())
	catalog = append(catalog, controllerRebindCommand())
	catalog = append(catalog, controllerUninstallCommand())
	catalog = append(catalog, environmentClusterCommands()...)
	catalog = append(catalog, storageAccessCommands()...)
	catalog = append(catalog, containerAccessCommands()...)
	catalog = append(catalog, presentationCommands()...)
	return catalog
}

func nameFlag() flagSpec { return requiredFlag("name", "Select the exact object name") }

func confirmationFlag() flagSpec {
	return boolFlag("yes", "Confirm the command's ordinary confirmation")
}

func outputFlag() flagSpec {
	flag := enumFlag("output", "text", "Select output mode", "text", "json", "yaml")
	flag.short = "o"
	return flag
}

func clustersFlag() flagSpec {
	return listFlag("clusters", "Select comma-separated cluster names (default: all)")
}

func blankClustersFlag() flagSpec {
	f := clustersFlag()
	f.blankOK = true
	return f
}

func listFlag(name, help string) flagSpec {
	f := emptyFlag(name, help)
	f.nameList = true
	return f
}

func emptyFlag(name, help string) flagSpec {
	f := stringFlag(name, help)
	f.emptyOK = true
	return f
}

func dryRunFlag() flagSpec {
	return boolFlag("dry-run", "Limit work to the command's dry-run boundary")
}

func sensitiveFlag() flagSpec {
	return boolFlag("sensitive", "Authorize sensitive artifact materialization")
}

func contextFileFlag() flagSpec {
	flag := fileFlag("file", "Read one standalone Context YAML file")
	flag.short, flag.kind = "f", "stringArray"
	return flag
}

func stageFlag() flagSpec {
	flag := stringFlag("stage", "Select comma-separated stages to start (default: all)")
	flag.enum, flag.enumList, flag.nameList = []string{"controller", "infrastructure-services", "substrates", "machines", "clusters", "add-ons"}, true, true
	return flag
}

func loginFlag() flagSpec {
	flag := stringFlag("login", "Select comma-separated administrator logins to update (default: all the cluster has)")
	flag.enum, flag.enumList, flag.nameList = slices.Clone(lifecycle.AdminLoginNames), true, true
	return flag
}

func untilFlag() flagSpec {
	return stringFlag("until", "Stop after one add-on instance, named <cluster>.<package>, and the blocks it requires")
}

func authorizationFlag() flagSpec {
	flag := stringFlag("authorize", "Acknowledge a planned risk (data-loss, or data-loss=<digest> of the steps the plan shows, required with --yes or without a terminal), or present a rotated access credential at its current version (rebind=<secret>)")
	flag.kind, flag.enum = "stringArray", []string{"data-loss"}
	return flag
}

func destroyAuthorizationFlag() flagSpec {
	flag := authorizationFlag()
	flag.help += ", or relax one Machine's declared removal erase for this destroy (erase=<machine>:metadataClear or erase=<machine>:retain)"
	return flag
}

func unregisteredFlag() flagSpec {
	flag := stringFlag("unregistered", "Name a Machine whose subscription registration you removed with the subscription service's own tools")
	flag.kind = "stringArray"
	return flag
}

const payloadHelp = " Bootwright flags go before the command." +
	" Use -- before a flag-shaped first payload value to preserve it as data." +
	" After the first payload value, all remaining tokens belong to the payload."

// sessionCommand describes the login, trust and exit status shared by Machine
// and cluster SSH sessions.
func sessionCommand(spec commandSpec) commandSpec {
	spec.session = true
	spec.long = spec.short + ". By default, log in with the Machine's SSH identity." +
		" Select a different account with --ssh-user and its --ssh-id-file or --ssh-ask-password." +
		" Password prompts require a terminal and one target. An unproved host key requires" +
		" interactive confirmation before SSH offers a credential. The exit status is the client's."
	spec.sshFlags = sessionSSHFlags
	spec.short += " as the Machine's resolved identity; confirm or refuse an unproved host key and return the client's exit status"
	if spec.payload {
		spec.long += payloadHelp
	}
	return spec
}

func accessCommand(spec commandSpec, applicability string) commandSpec {
	spec.short = strings.Replace(spec.short, "; do not", " for independent operator execution; do not", 1)
	spec.long = spec.short + ". Success prints a bounded access descriptor."
	if spec.payload {
		spec.long += payloadHelp
	}
	spec.long += applicability
	return spec
}
