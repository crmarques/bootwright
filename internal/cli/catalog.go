package cli

import "strings"

type commandSpec struct {
	path    string
	short   string
	long    string
	flags   []flagSpec
	payload bool
	// implemented marks a command whose application use case is available.
	// Every such command also needs the privileged context store, so available
	// records both facts with the declaration instead of a separate list.
	implemented bool
	privileged  bool
	session     bool
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
}

func stringFlag(name, help string) flagSpec { return flagSpec{name: name, help: help, kind: "string"} }

// undisclosedFlag is a string flag whose value no output repeats: a Secret's
// username part, or a download URL whose query may carry a token.
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
		stringFlag("context", "Select a context (default: current context)"),
		stringFlag("ssh-id-file", "Offer an explicit private key for eligible SSH access"),
		stringFlag("ssh-user", "Borrow an explicit POSIX account for eligible SSH access"),
		boolFlag("ssh-ask-sudo-password", "Request the borrowed account's sudo password"),
		boolFlag("ssh-user-for-provisioned", "Extend the borrowed account to provisioned machines"),
		{name: "help", short: "h", help: "Show help for this command", kind: "bool", defaultValue: "false"},
	}
}

func commandCatalog() []commandSpec {
	var catalog []commandSpec
	catalog = append(catalog, workspaceCommands()...)
	catalog = append(catalog, addOnCatalogCommands()...)
	catalog = append(catalog, secretCommands()...)
	catalog = append(catalog, mediaCommands()...)
	catalog = append(catalog, validationCommand())
	catalog = append(catalog, controllerPreflightCommand())
	catalog = append(catalog, infrastructurePreflightCommand())
	catalog = append(catalog, clustersPreflightCommand())
	catalog = append(catalog, containerPreflightCommand())
	catalog = append(catalog, storagePreflightCommand())
	catalog = append(catalog, addOnPreflightCommand())
	catalog = append(catalog, allPreflightCommand())
	catalog = append(catalog, lifecycleInspectionCommands()...)
	catalog = append(catalog, nativeArtifactCommand())
	catalog = append(catalog, effectiveStateCommand())
	catalog = append(catalog, installerRenderCommand())
	catalog = append(catalog, storageRenderCommand())
	catalog = append(catalog, lifecycleMutationCommands()...)
	catalog = append(catalog, machineCommands()...)
	catalog = append(catalog, machineTrustCommand())
	catalog = append(catalog, controllerSetupCommand())
	catalog = append(catalog, environmentClusterCommands()...)
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
	return stringFlag("clusters", "Select comma-separated cluster names (default: all)")
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
	flag.enum, flag.enumList = []string{"controller", "infra-components", "substrates", "machines", "clusters", "add-ons"}, true
	return flag
}

func authorizationFlag() flagSpec {
	flag := stringFlag("authorize", "Acknowledge a planned risk (data-loss)")
	flag.kind, flag.enum = "stringArray", []string{"data-loss"}
	return flag
}

func unregisteredFlag() flagSpec {
	flag := stringFlag("unregistered", "Name a Machine whose subscription registration you removed with the subscription service's own tools")
	flag.kind = "stringArray"
	return flag
}

func trustFlag() flagSpec {
	flag := boolFlag("trust-on-first-use", "Observe unknown SSH keys without accepting them")
	flag.defaultValue = "true"
	return flag
}

func preflightFlags() []flagSpec {
	return []flagSpec{clustersFlag(), dryRunFlag(), trustFlag()}
}

const payloadHelp = " Bootwright flags go before the command." +
	" Use -- before a flag-shaped first payload value to preserve it as data." +
	" After the first payload value, all remaining tokens belong to the payload."

// sessionCommand describes one command that opens an SSH session. Its help
// states what the operator is actually getting: a live session as an identity
// they did not choose, over a host key they may be asked to confirm, whose
// exit status is the client's rather than a Bootwright result.
func sessionCommand(spec commandSpec) commandSpec {
	spec.session = true
	spec.short += " as the Machine's resolved identity; confirm or refuse an unproved host key and return the client's exit status"
	spec.long = spec.short + ". The session runs as the identity the Machine's desired state authorizes," +
		" over a host key proved before any credential is offered; an unproved key is confirmed" +
		" interactively or refused. The exit status is the client's."
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
