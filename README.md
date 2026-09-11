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

Help, version, shell completion, durable context commands, validation and
`render effective` are implemented.
Validation admits all 26 desired-state kinds, resolves defaults and references,
and reports deterministic diagnostics without reading secret values. Explicit
`validate -f` does not open context state; omission uses the frozen current
context inputs. The complete `secret` tree provides local encrypted custody,
generation, validation, explicit disclosure and key rotation. Other rendering
and lifecycle operations retain `cli.not-implemented` with exit status `1`
before application effects. Invalid usage exits `2`.

Create a context with its default encrypted keyring, then import desired state:

```sh
./bin/bootwright context init --name example
./bin/bootwright context update --name example --input-dir examples/multidc-platform --yes
./bin/bootwright context current --short
```

Contexts live in root-only `/var/lib/bootwright/contexts/<name>`. Bootwright
requests sudo authorization when needed; current selection belongs to the
invoking user in `~/.bootwright/context`. Optional `--file context.yaml` supplies
Context setup configuration, independently of `--input-dir`.

For a context containing Secret declarations, set or generate material:

```sh
./bin/bootwright secret generate
./bin/bootwright secret check --output json
```

Only `local-keyring` is registered in production. Its local key files protect
against disclosure outside the state boundary, not theft of the entire state
root together with its keys. See [secret management](specs/secrets.md) for the
threat model, exact input flags, retention and recovery contracts.

The [command catalog](specs/cli/commands.md) defines the full command tree and
flags. Completion scripts are available through `bootwright completion bash`,
`zsh`, `fish`, and `powershell`.

## Code layout

To find the code behind a command, start at `internal/cli/commands_<domain>.go`
(grep the command path, such as `"secret set"`); its consumer interface names
the service package `internal/<context>/<capability>/`, where `service.go` is the
use case, `contracts.go` lists the ports it consumes and `requests.go` is what the
CLI sees; `cmd/bootwright/wiring.go` binds each port to an adapter package.

| Path | Responsibility |
| --- | --- |
| `cmd/bootwright/` | The only composition root: `main.go` entry/exit and linker metadata; `run.go` privilege boundary and CLI invocation; `wiring.go` constructs the service bundle. |
| `api/v1alpha1/` | Presence-preserving immutable values and closed kind schemas. |
| `internal/cli/` | Driving adapter: `catalog.go` (the single command list), `runner.go`, `dispatch.go`, `results.go`, per-domain `commands_*.go` and `output_*.go`. |
| `internal/<context>/` | Pure domain values and kind admission rules; shared values such as SSH options and secret material. |
| `internal/<context>/<capability>/` | One application `Service` per command family with its requests and ports: `workspace/contexts`, `secrets/custody`, `secrets/encryption`, `desiredstate/compilation`, `controller/prerequisites`; the others are typed stubs. |
| `internal/<context>/<adapter>/` | Driven adapters named by what they bind: `contextfs`, `selectionfs`, `inputfs`, `yamlstream`, `encoding`, `localstore`, `material`, `hostlinux`, `bundlelocal`, `ansiblelocal`, `nativelocal`, `invocation`. |
| `internal/availability/` | Shared unavailable-capability error; no presentation or effects. |
| `test/architecture/` | Fitness checks: dependency direction, effect boundaries, composition-only binding. |
| `scripts/` | Reproducible build and verification entrypoints. |

The [command and package map](specs/architecture.md#command-and-package-map)
lists every command with its CLI file, service package, adapters and status, and
the [communication graph](specs/architecture.md#domain-communication-graph)
shows which interface each domain uses to reach another. The map names the
target package layout; the pending renames are listed in its
[transition table](specs/architecture.md#transition). Application packages are
independent of Cobra and terminal output. The compiler receives verified source
bytes through its input adapter; pure domain owners supply admission rules under
the [package contract](specs/architecture.md#go-package-structure).

## Development

The module locks Cobra, the YAML parser, SSH codecs, and their dependencies in `go.mod`
and `go.sum`. The build and check entrypoints select the pinned Go toolchain. See
[development](docs/development.md) for checks and shell-runtime requirements.
