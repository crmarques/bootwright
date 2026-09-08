# Development

The CLI uses [Cobra v1.10.2](https://github.com/spf13/cobra/releases/tag/v1.10.2)
for command and flag definitions, parsing, and help. Cobra was explicitly
selected for this skeleton. Its pflag dependency supplies established Boolean,
scalar, and repeated-flag parsing; the standard library handles encoding and
context-independent value validation.

Completion candidates derive from the same Cobra tree. The private adapter
implements the [completion boundary](../specs/cli/commands.md#completion).
Its framework constraints and shell harness findings are indexed in
[CLI adapter knowledge](../.agents/knowledge/cli-adapter-constraints.md).

Build and verification tools are development dependencies; running the
Bootwright executable never installs them. Dependency acquisition during a
build may need network access. See [build knowledge](../.agents/knowledge/build-toolchain.md)
for the exact toolchain selection and release metadata injection points.
`scripts/tools` separately locks `govulncheck` v1.4.0 and its
dependencies, keeping check tooling out of the application module graph.

| Command | Result |
| --- | --- |
| `make build` | Build `bin/bootwright`. |
| `make test` | Run all package tests. |
| `make vet` | Run Go static analysis. |
| `make fmt-check` | Check Go formatting. |
| `make modules-check` | Verify both module locks. |
| `make completion-test` | Source and exercise all four shell integrations. |
| `make vulncheck` | Scan for known reachable vulnerabilities. |
| `make check` | Run all verification gates, including completion. |

Completion tests require `bash`, `zsh`, `fish`, and `pwsh` on the test runner's
`PATH`. Alternatively, set `BOOTWRIGHT_TEST_BASH`, `BOOTWRIGHT_TEST_ZSH`,
`BOOTWRIGHT_TEST_FISH`, and `BOOTWRIGHT_TEST_POWERSHELL` to absolute executable
paths. These variables belong only to the test harness. The Bootwright CLI
does not read them. The shell integration gate fails if a runtime is missing.

The M1a shell checks use Bash `5.3.0`, Zsh `5.9`, Fish `4.0.2`, and PowerShell
`7.7.0-preview.2` on Linux/amd64. PowerShell completion requires that preview
release or later because stable `7.6` lacks the
[empty-result fix](https://github.com/PowerShell/PowerShell/pull/27398). The
generated script rejects older versions; it does not enable filename fallback.
A broader release-platform matrix remains part of release qualification.

Package tests exercise command dispatch, parsing and help precedence, output,
cancellation, and effect boundaries. Completion verification additionally
sources and exercises generated scripts in Bash, Zsh, Fish, and PowerShell.
Unavailable shell runtimes are missing verification evidence, not passes.

Future lifecycle, filesystem, secret, and remote implementations remain in
their [owning milestones](../specs/milestones.md). Stub request structures are
internal scaffolding, not a committed public desired-state API or successful
result format.
