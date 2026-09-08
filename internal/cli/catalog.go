package cli

import "strings"

type commandSpec struct {
	path          string
	short         string
	long          string
	flags         []flagSpec
	payload       bool
	stopAtPayload bool
}

type flagSpec struct {
	name         string
	short        string
	help         string
	kind         string
	defaultValue string
	required     bool
	enum         []string
}

func stringFlag(name, help string) flagSpec { return flagSpec{name: name, help: help, kind: "string"} }
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

func globalFlags() []flagSpec {
	return []flagSpec{
		stringFlag("context", "Select a context (default: current context)"),
		stringFlag("ssh-id-file", "Offer an explicit private key for eligible SSH access"),
		stringFlag("ssh-user", "Borrow an explicit POSIX account for eligible SSH access"),
		boolFlag("ssh-ask-sudo-password", "Request the borrowed account's sudo password"),
		boolFlag("ssh-user-for-provisioned", "Extend the borrowed account to provisioned machines"),
		{name: "help", short: "h", help: "Show help for this command", kind: "bool", defaultValue: "false"},
	}
}

func commandCatalog() []commandSpec {
	name := requiredFlag("name", "Select the exact object name")
	yes := boolFlag("yes", "Confirm the command's ordinary confirmation")
	output := enumFlag("output", "text", "Select output mode", "text", "json")
	clusters := stringFlag("clusters", "Select comma-separated cluster names (default: all)")
	dryRun := boolFlag("dry-run", "Limit work to the command's dry-run boundary")
	verbose := boolFlag("verbose", "Include safe progress details")
	verbose.short = "v"
	sensitive := boolFlag("sensitive", "Authorize sensitive artifact materialization")
	file := requiredFlag("file", "Supply one input directory")
	file.short = "f"
	file.kind = "stringArray"
	authorize := stringFlag("authorize", "Acknowledge a planned risk (data-loss)")
	authorize.kind = "stringArray"
	authorize.enum = []string{"data-loss"}
	trust := boolFlag("trust-on-first-use", "Observe unknown SSH keys without accepting them")
	trust.defaultValue = "true"
	preflight := []flagSpec{clusters, dryRun, output, trust, verbose}
	catalog := []commandSpec{
		{path: "context init", short: "Initialize a context from desired-state input", flags: []flagSpec{name, file, yes}},
		{path: "context update", short: "Replace a context's desired-state input", flags: []flagSpec{name, file, yes}},
		{path: "context use", short: "Select the current context", flags: []flagSpec{name}},
		{path: "context list", short: "List contexts"},
		{path: "context current", short: "Show the current context", flags: []flagSpec{boolFlag("short", "Print only the context name")}},
		{path: "context delete", short: "Delete disposable context data or archive for recovery", flags: []flagSpec{name, boolFlag("purge", "Acknowledge context data deletion"), yes, boolFlag("abandon-resources", "Request recovery-only archival")}},
		{path: "add-ons list", short: "List add-on catalog registrations", flags: []flagSpec{output}},
		{path: "add-ons add", short: "Register an immutable add-on catalog release", flags: []flagSpec{name, stringFlag("version", "Select the catalog release version"), yes}},
		{path: "add-ons delete", short: "Delete an add-on registration", flags: []flagSpec{name, yes}},
		{path: "secret set", short: "Store confidential material", flags: []flagSpec{name, stringFlag("pull-secret", "Read a pull-secret file"), stringFlag("tls-cert", "Read a TLS certificate file"), stringFlag("tls-key", "Read the paired TLS private-key file"), stringFlag("raw-file", "Read raw material from a file"), stringFlag("from-file", "Read material from a file"), boolFlag("password-stdin", "Read a password from standard input"), boolFlag("generate", "Generate secret material"), stringFlag("username", "Associate a username with secret material"), yes}},
		{path: "secret generate", short: "Generate missing declared secrets", flags: []flagSpec{boolFlag("renew", "Renew declarations with a generated source")}},
		{path: "secret check", short: "Check secret availability and types", flags: []flagSpec{output}},
		{path: "secret list", short: "List secret metadata", flags: []flagSpec{output}},
		{path: "secret show", short: "Export raw sensitive secret bytes to standard output", flags: []flagSpec{name, enumFlag("part", "primary", "Select the secret part", "primary", "private", "public", "tls-key")}},
		{path: "secret delete", short: "Delete an unbound secret", flags: []flagSpec{name, yes}},
		{path: "secret encryption init", short: "Initialize the encryption keyring"},
		{path: "secret encryption status", short: "Show encryption metadata", flags: []flagSpec{output}},
		{path: "secret encryption rotate", short: "Rotate the active encryption key", flags: []flagSpec{yes}},
		{path: "media add", short: "Import and verify an installer image", flags: []flagSpec{name, stringFlag("from-file", "Import a local image"), stringFlag("from-url", "Import an HTTP or HTTPS image"), stringFlag("sha256", "Verify a SHA-256 digest"), yes}},
		{path: "media list", short: "List installer images", flags: []flagSpec{boolFlag("checksums", "Compute image checksums"), output}},
		{path: "media delete", short: "Delete an unbound installer image", flags: []flagSpec{name, yes}},
		{path: "validate", short: "Validate a complete desired-state input universe", flags: []flagSpec{{name: "file", short: "f", help: "Supply an input file or directory (repeatable)", kind: "stringArray"}, output}},
		{path: "preflight bastion", short: "Check controller prerequisites"},
		{path: "preflight infra", short: "Check infrastructure readiness", flags: preflight},
		{path: "preflight clusters", short: "Check selected cluster readiness", flags: preflight},
		{path: "preflight container-cluster", short: "Check ContainerCluster readiness", flags: preflight},
		{path: "preflight storage-cluster", short: "Check StorageCluster readiness", flags: preflight},
		{path: "preflight add-ons", short: "Check add-on prerequisites", flags: []flagSpec{clusters, output}},
		{path: "preflight all", short: "Check all prerequisites", flags: []flagSpec{dryRun, output, trust, verbose}},
		{path: "plan", short: "Preview the next complete lifecycle operation"},
		{path: "status", short: "Show context readiness and lifecycle state", flags: []flagSpec{output, boolFlag("watch", "Watch lifecycle state"), {name: "watch-interval", help: "Set the watch interval", kind: "string", defaultValue: "5s"}}},
		{path: "render", short: "Render external-tool artifacts", flags: []flagSpec{stringFlag("input-dir", "Read a context-free input file or directory"), stringFlag("output-dir", "Write artifacts to the selected directory"), clusters, sensitive, output}},
		{path: "render effective", short: "Show normalized effective desired state", flags: []flagSpec{output}},
		{path: "render installer", short: "Render container-cluster installer artifacts", flags: []flagSpec{clusters, sensitive, output}},
		{path: "render storage", short: "Render storage artifacts", flags: []flagSpec{clusters, output}},
		{path: "apply", short: "Apply the complete selected lifecycle unit", flags: []flagSpec{authorize, yes, verbose}},
		{path: "destroy", short: "Destroy the complete selected lifecycle unit", flags: []flagSpec{authorize, yes, verbose}},
		{path: "machine list", short: "List machines and ownership-backed state", flags: []flagSpec{clusters, boolFlag("silent", "Print only sorted machine names"), output}},
		{path: "machine rsh", short: "Print an SSH access descriptor; do not launch a client or connect", flags: []flagSpec{name}},
		{path: "machine exec", short: "Print a command access descriptor; do not launch a client or connect", flags: []flagSpec{name}, payload: true},
		{path: "machine trust", short: "Inspect and maintain exact host-key trust", flags: []flagSpec{stringFlag("machines", "Select comma-separated machine names (default: all)"), stringFlag("replace", "Select comma-separated replacements (default: none)"), dryRun, yes, output}},
		{path: "bastion setup", short: "Set up controller prerequisites", flags: []flagSpec{dryRun, yes}},
		{path: "cluster list", short: "List container and storage clusters", flags: []flagSpec{output}},
		{path: "cluster info", short: "Show cluster details and static access applicability", flags: []flagSpec{stringFlag("name", "Select one cluster (default: all)"), boolFlag("secrets", "Include explicitly selected sensitive values"), output}},
		{path: "cluster rsh", short: "Print an SSH access descriptor; do not launch a client or connect", flags: []flagSpec{name, stringFlag("node", "Select a declared node, FQDN, or role-ordinal")}},
		{path: "cluster exec", short: "Print a command access descriptor; do not launch a client or connect", flags: []flagSpec{name, stringFlag("node", "Select a declared node, FQDN, or role-ordinal")}, payload: true},
		{path: "cluster oc", short: "Print an oc access descriptor; do not launch a client or connect", flags: []flagSpec{name}, payload: true, stopAtPayload: true},
		{path: "cluster kubectl", short: "Print a kubectl access descriptor; do not launch a client or connect", flags: []flagSpec{name}, payload: true, stopAtPayload: true},
		{path: "cluster kubeconfig", short: "Export raw sensitive kubeconfig bytes to standard output", flags: []flagSpec{name}},
		{path: "version", short: "Show executable and dependency build identities"},
		{path: "help", short: "Show human help for an exact command path"},
		{path: "completion bash", short: "Generate a Bash completion script", flags: []flagSpec{boolFlag("no-descriptions", "Omit completion descriptions")}},
		{path: "completion zsh", short: "Generate a Zsh completion script", flags: []flagSpec{boolFlag("no-descriptions", "Omit completion descriptions")}},
		{path: "completion fish", short: "Generate a Fish completion script", flags: []flagSpec{boolFlag("no-descriptions", "Omit completion descriptions")}},
		{path: "completion powershell", short: "Generate a PowerShell completion script", flags: []flagSpec{boolFlag("no-descriptions", "Omit completion descriptions")}},
	}
	for i := range catalog {
		spec := &catalog[i]
		if strings.Contains(spec.short, "access descriptor") {
			spec.long = spec.short + ". Success prints a bounded access descriptor for independent operator execution."
		}
		if spec.payload {
			spec.long += " Use -- before a flag-shaped first payload value to preserve it as data."
			if spec.stopAtPayload {
				spec.long += " After the first payload value, all remaining tokens belong to the payload."
			} else {
				spec.long += " Recognized Bootwright flags remain active in the command tail until --."
			}
		}
		switch spec.path {
		case "completion powershell":
			spec.long = "Generate a PowerShell completion script for PowerShell 7.7.0-preview.2 or later. Older versions lack the empty-result fix required to disable filesystem fallback and are rejected when sourcing the script."
		case "context delete":
			spec.long = "Delete proven-disposable local context data with --purge=true. --yes controls ordinary confirmation; --abandon-resources requests recovery-only archival."
		case "secret set":
			spec.long = "Store one secret using exactly one source mode. --tls-cert and --tls-key are an inseparable pair. --password-stdin requires --username."
		case "render":
			spec.long = "Render external-tool artifacts. Without --input-dir or --output-dir, print this help. Context-free rendering requires both paths and conflicts with --context and --sensitive. Context-backed rendering requires --output-dir and --sensitive."
		case "cluster rsh", "cluster exec":
			spec.long += " Applicable to OpenShift/OKD ContainerClusters and managed Ceph StorageClusters with declared nodes. External StorageClusters are inapplicable."
		case "cluster oc", "cluster kubectl", "cluster kubeconfig":
			if spec.long == "" {
				spec.long = spec.short + "."
			}
			spec.long += " Applicable to OpenShift/OKD ContainerClusters only."
		}
	}
	return catalog
}
