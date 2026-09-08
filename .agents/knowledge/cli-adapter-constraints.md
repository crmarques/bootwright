# CLI adapter constraints

These findings apply to the M1a CLI with Cobra v1.10.2 and pflag v1.0.10.
Recheck them when changing the framework or its integration. Required behavior
remains in [CLI parsing](../../specs/cli.md#parsing-and-input-conventions) and
[completion](../../specs/cli/commands.md#completion).

## Framework completion effects

Cobra's native completion reads environment configuration, including
`BASH_COMP_DEBUG_FILE`, and can create a debug file. Its `ExecuteContext` path
also enters `ExecuteC` platform hooks; the Windows hook can inspect the parent
process and wait on stdin. These observations come from the pinned dependency's
`completions.go`, `command.go`, and `command_win.go`. They explain why
[Runner.run](../../internal/cli/runner.go) calls the
owned private completion handler directly after argument bounds checks, and
[configureCompletion](../../internal/cli/completion.go) uses the static command
tree with Cobra's directive protocol.

`TestCompletionProtocolIgnoresAmbientConfiguration` and `TestCompletionScripts`
in [completion_test.go](../../internal/cli/completion_test.go) exercise the
environment and file sentinels and generated scripts. They do not establish
Windows runtime qualification. The PowerShell compatibility finding is linked
from the owning [completion contract](../../specs/cli/commands.md#completion).

## Flag ownership and request selection

[resolveInvocation](../../internal/cli/parsing.go) enforces command-local flag
positions before pflag parses values. On failure it removes flags belonging to
other commands before `syntaxJSON` selects the diagnostic representation. This
prevents a parent's `--output` from selecting a child's output mode when both
declare that spelling. See `TestUsageJSONRespectsLocalFlagOwnership` in
[parsing_test.go](../../internal/cli/parsing_test.go).

[requestValues.names](../../internal/cli/dispatch_values.go) preserves omitted
cluster selection as a nil slice and explicit empty selection as a non-nil
empty slice. Collapsing those representations would conflate the default
selection with selecting none. `TestDispatchSelectionAndSourceTranslation` in
[dispatch_test.go](../../internal/cli/dispatch_test.go) covers the distinction.

## Shell test harness

[shellQuery](../../test/completion/completion_test.go) captures Zsh's `compadd`
candidate channel while executing the sourced function in Zsh: the real
`compadd` builtin requires ZLE. The PowerShell harness passes a script through
`-File` so test arguments remain arguments instead of being interpreted as
source code. `TestGeneratedIntegrations`, run by `make completion-test`, is the
executable evidence; it fails when a required shell runtime is missing.
