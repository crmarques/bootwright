package cli

func presentationCommands() []commandSpec {
	return []commandSpec{
		{path: "version", short: "Show executable and dependency build identities"},
		{path: "help", short: "Show human help for an exact command path"},
		{path: "completion bash", short: "Generate a Bash completion script", flags: []flagSpec{boolFlag("no-descriptions", "Omit completion descriptions")}},
		{path: "completion zsh", short: "Generate a Zsh completion script", flags: []flagSpec{boolFlag("no-descriptions", "Omit completion descriptions")}},
		{path: "completion fish", short: "Generate a Fish completion script", flags: []flagSpec{boolFlag("no-descriptions", "Omit completion descriptions")}},
		{path: "completion powershell", short: "Generate a PowerShell completion script", flags: []flagSpec{boolFlag("no-descriptions", "Omit completion descriptions")}, long: "Generate a PowerShell completion script for PowerShell 7.7.0-preview.2 or later. Older versions lack the empty-result fix required to disable filesystem fallback and are rejected when sourcing the script."},
	}
}
