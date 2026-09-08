# Bootwright

Bootwright coordinates declarative day-0 platform bootstrap. Start with the
[specification index](specs/index.md) and
[current milestone](specs/milestones.md) for scope and availability.

## Run the CLI skeleton

```sh
make build
./bin/bootwright --help
./bin/bootwright version
./bin/bootwright cluster exec --help
./bin/bootwright validate --output json
```

Help, version, and shell completion are implemented. Every application command
parses and validates its arguments, calls an injected typed stub, and reports
`cli.not-implemented` with exit status `1`. The stubs perform no filesystem,
state, credential, process, or network work. Invalid usage exits `2`.

The [command catalog](specs/cli/commands.md) defines the full command tree and
flags. Completion scripts are available through `bootwright completion bash`,
`zsh`, `fish`, and `powershell`.

## Code layout

| Path | Responsibility |
| --- | --- |
| `cmd/bootwright/` | Process inputs, build metadata, and explicit service wiring. |
| `internal/cli/` | Cobra catalog, parsing, validation, consumer interfaces, request translation, help, completion, and output. |
| `internal/<context>/` | Context-owned request types and application stubs, grouped by capability. |
| `internal/availability/` | Shared unavailable-capability error; no presentation or effects. |
| `test/architecture/` | Checks that the skeleton preserves dependency and effect boundaries. |
| `scripts/` | Reproducible build and verification entrypoints. |

Application packages do not depend on Cobra or terminal output. To implement a
future command, replace its owning stub with the authorized use case and define
its result and effect ports under the [architecture contract](specs/architecture.md).
Keep CLI translation and output in the driving adapter.

## Development

The module locks Cobra and its dependencies in `go.mod` and `go.sum`. The build
and check entrypoints select the pinned Go toolchain. See
[development](docs/development.md) for checks and shell-runtime requirements.
