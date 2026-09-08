# Bootwright

Bootwright coordinates declarative day-0 platform bootstrap. Start with the
[specification index](specs/index.md) and
[current milestone](specs/milestones.md) for scope and availability.

## Run Bootwright

```sh
make build
./bin/bootwright --help
./bin/bootwright version
./bin/bootwright cluster exec --help
./bin/bootwright validate -f examples/multidc-platform --output json
```

Help, version, shell completion, and context-free `validate -f` are implemented.
Validation admits all 21 desired-state kinds, resolves defaults and references,
and reports deterministic diagnostics without reading payloads or contexts.
Context commands, validation without `-f`, rendering, and lifecycle operations
retain `cli.not-implemented` with exit status `1` before application effects.
Invalid usage exits `2`.

The [command catalog](specs/cli/commands.md) defines the full command tree and
flags. Completion scripts are available through `bootwright completion bash`,
`zsh`, `fish`, and `powershell`.

## Code layout

| Path | Responsibility |
| --- | --- |
| `cmd/bootwright/` | `main.go` owns entry/exit and linker metadata; `run.go` assembles the CLI invocation; `wiring.go` constructs the service bundle. |
| `api/v1alpha1/` | Presence-preserving immutable values and closed kind schemas. |
| `internal/cli/` | Command-family declarations, consumer interfaces, and request translation; shared parsing, help, completion, dispatch, and output. |
| `internal/<context>/` | Pure domain values and rules, including shared SSH options and secret sources. |
| `internal/<context>/<capability>/` | Concrete `Service`, typed requests, and consumed interfaces, such as `workspace/contexts` and `reconciliation/lifecycle`. |
| `internal/availability/` | Shared unavailable-capability error; no presentation or effects. |
| `test/architecture/` | Checks dependency direction and admission effect boundaries. |
| `scripts/` | Reproducible build and verification entrypoints. |

The [service ownership map](specs/architecture.md#application-service-ownership)
identifies each capability and its CLI dependency. The
[interaction contracts](specs/architecture.md#service-interactions) describe
explicit-input compilation and future context admission, lifecycle, and
artifact publication. Application packages are independent of Cobra and
terminal output. The compiler receives verified source bytes through its
input adapter; pure domain owners supply admission rules under the
[package contract](specs/architecture.md#go-package-structure).

## Development

The module locks Cobra, the YAML parser, and their dependencies in `go.mod`
and `go.sum`. The build and check entrypoints select the pinned Go toolchain. See
[development](docs/development.md) for checks and shell-runtime requirements.
