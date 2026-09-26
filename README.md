# Bootwright

Bootwright is a declarative orchestrator for provisioning and bootstrapping a
cloud platform from scratch until it is ready for GitOps tools to take over
ongoing platform management. Users describe one complete environment:
substrate, machines, infrastructure services, container and storage clusters
and bootstrap integrations. Bootwright validates and freezes that intent,
orders dependencies, drives qualified native tools through controlled adapters
and verifies each selected outcome. Lifecycle initiation, confirmation and
irreversible authorization stay explicit operator actions.

**Status:** [milestones](specs/milestones.md) is the single owner of what is
delivered, in progress and next. The [specification index](specs/index.md)
owns required behavior.

## Quickstart

```sh
make build
./bin/bootwright --help
./bin/bootwright validate -f examples/lab-rhel
```

Then follow [`examples/lab-rhel`](examples/lab-rhel/README.md), which installs
one RHEL guest through an emulated Redfish BMC on a single host. The
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
