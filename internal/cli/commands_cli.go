package cli

import "github.com/spf13/cobra"

type commandPresenter func(r *Runner, root, command *cobra.Command, path string) int

func presentVersion(r *Runner, _, _ *cobra.Command, _ string) int {
	if err := r.writeSelectedVersion(); err != nil {
		return 1
	}
	return 0
}

func presentSchema(r *Runner, _, command *cobra.Command, path string) int {
	if err := r.writeSelectedSchema(); err != nil {
		return r.failure(command, path, "runtime.encode", "the schema could not be encoded", 1, selectedStructured(command))
	}
	return 0
}

func presentSBOM(r *Runner, _, command *cobra.Command, path string) int {
	if err := r.writeSelectedBillOfMaterials(); err != nil {
		return r.failure(command, path, "runtime.encode", "the bill of materials could not be composed: "+err.Error(), 1, selectedStructured(command))
	}
	return 0
}

func presentCompletion(r *Runner, root, command *cobra.Command, path string) int {
	if err := r.writeSelectedCompletion(root, path, boolValue(command.Flags(), "no-descriptions")); err != nil {
		return 1
	}
	return 0
}

func presentationCommands() []commandSpec {
	return []commandSpec{
		{path: "version", results: []jsonResult{versionPresentation{}}, present: presentVersion, example: "", contextFree: true, presentation: true, short: "Show executable and dependency build identities"},
		{path: "catalog schema", results: []jsonResult{schemaPresentation{}}, present: presentSchema, example: "--format json-schema", contextFree: true, presentation: true, short: "Export the advisory schema of every desired-state kind", long: "Export the advisory schema of every desired-state kind. The export states types, required fields, closed field sets, enum values and numeric bounds; bootwright validate stays the authority on lexical grammars, references and cross-field rules.",
			flags: []flagSpec{requiredEnumFlag("format", "Select the schema format", "json-schema")}},
		{path: "sbom", results: []jsonResult{billOfMaterialsPresentation{}}, present: presentSBOM, example: "--format cyclonedx-json", contextFree: true, presentation: true, short: "Export this executable's software bill of materials", long: "Export this executable's software bill of materials as one CycloneDX JSON document: what the executable carries (its Go modules and toolchain, the controller runtime's wheels and the embedded automation) and what it pins and acquires at use (the default target clients and the managed services' and emulated BMC's images), each with its version and digest. It reads no context and contacts nothing.",
			flags: []flagSpec{requiredEnumFlag("format", "Select the bill of materials format", "cyclonedx-json")}},
		{path: "explain", results: []jsonResult{explainPresentation{}}, present: presentExplain, example: "--code runner.deadline", contextFree: true, presentation: true, short: "Explain the stop codes apply and destroy report",
			long:  "Explain the stop codes apply and destroy report. With no flag it lists every stop; with --code it prints that stop's meaning, the level it stops at, whether its effect is certain, its cause, its recovery, whether repeating the command helps and its remedy. It reads no context and contacts nothing.",
			flags: []flagSpec{stringFlag("code", "Select one stop by its code")}},
		{path: "help", results: []jsonResult{helpPresentation{}}, example: "context init", contextFree: true, presentation: true, helpCommand: true, short: "Show help for an exact command path"},
		{path: "completion bash", results: []jsonResult{exportResult{}}, present: presentCompletion, example: "", contextFree: true, presentation: true, short: "Generate a Bash completion script", flags: []flagSpec{boolFlag("no-descriptions", "Omit completion descriptions")}},
		{path: "completion zsh", results: []jsonResult{exportResult{}}, present: presentCompletion, example: "", contextFree: true, presentation: true, short: "Generate a Zsh completion script", flags: []flagSpec{boolFlag("no-descriptions", "Omit completion descriptions")}},
		{path: "completion fish", results: []jsonResult{exportResult{}}, present: presentCompletion, example: "", contextFree: true, presentation: true, short: "Generate a Fish completion script", flags: []flagSpec{boolFlag("no-descriptions", "Omit completion descriptions")}},
		{path: "completion powershell", results: []jsonResult{exportResult{}}, present: presentCompletion, example: "", contextFree: true, presentation: true, short: "Generate a PowerShell completion script", flags: []flagSpec{boolFlag("no-descriptions", "Omit completion descriptions")}, long: "Generate a PowerShell completion script for PowerShell 7.7.0-preview.2 or later. Older versions lack the empty-result fix required to disable filesystem fallback and are rejected when sourcing the script."},
	}
}
