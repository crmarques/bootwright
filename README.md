# Bootwright

Bootwright is a declarative orchestrator that provisions and bootstraps a cloud
platform from scratch until GitOps tools can take over.

**Scope:** [project](specs/project.md) owns the product scope and non-goals.

**Status:** [milestones](specs/milestones.md) is the single owner of what is
delivered, in progress and next. The [specification index](specs/index.md)
owns required behavior.

## Quickstart

```sh
make build
./bin/bootwright --help
./bin/bootwright validate -f examples/machines/lab-rhel
```

Then run the e2e [`lab-rhel`](e2e/lab-rhel/runbook.md) case, which installs
one RHEL guest like
[`examples/machines/lab-rhel`](examples/machines/lab-rhel/README.md) through an
emulated Redfish BMC on a single host. The
[examples index](examples/README.md) describes the others.

## Finding code

[`internal/cli/catalog.go`](internal/cli/catalog.go) declares every command;
each `internal/cli/commands_<domain>.go` names the service it calls. A command
family's use case lives under `internal/<context>/<capability>/`.
`cmd/bootwright/wiring_*.go`, the only composition root, binds each port to its
adapter. [Architecture](specs/architecture.md) owns the rules.

## Development

See [development](docs/development.md) for the pinned toolchain, the check
tiers and shell-runtime requirements, and the
[operator guide](docs/operator-guide.md) for preparing a host, running the
labs and recording a run.
