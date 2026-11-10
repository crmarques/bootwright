package cli

func presentationCommands() []commandSpec {
	return []commandSpec{
		{path: "version", short: "Show executable and dependency build identities"},
		{path: "schema", short: "Export the advisory schema of every desired-state kind", long: "Export the advisory schema of every desired-state kind. The export states types, required fields, closed field sets, enum values and numeric bounds; bootwright validate stays the authority on lexical grammars, references and cross-field rules.",
			flags: []flagSpec{requiredEnumFlag("format", "Select the schema format", "json-schema")}},
		{path: "sbom", short: "Export this executable's software bill of materials", long: "Export this executable's software bill of materials as one CycloneDX JSON document: what the executable carries (its Go modules and toolchain, the controller runtime's wheels and the embedded automation) and what it pins and acquires at use (the default target clients and the managed services' and emulated BMC's images), each with its version and digest. It reads no context and contacts nothing.",
			flags: []flagSpec{requiredEnumFlag("format", "Select the bill of materials format", "cyclonedx-json")}},
		{path: "help", short: "Show help for an exact command path"},
		{path: "completion bash", short: "Generate a Bash completion script", flags: []flagSpec{boolFlag("no-descriptions", "Omit completion descriptions")}},
		{path: "completion zsh", short: "Generate a Zsh completion script", flags: []flagSpec{boolFlag("no-descriptions", "Omit completion descriptions")}},
		{path: "completion fish", short: "Generate a Fish completion script", flags: []flagSpec{boolFlag("no-descriptions", "Omit completion descriptions")}},
		{path: "completion powershell", short: "Generate a PowerShell completion script", flags: []flagSpec{boolFlag("no-descriptions", "Omit completion descriptions")}, long: "Generate a PowerShell completion script for PowerShell 7.7.0-preview.2 or later. Older versions lack the empty-result fix required to disable filesystem fallback and are rejected when sourcing the script."},
	}
}
