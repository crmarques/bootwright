# Go Dependencies Skill

Use this skill when adding, upgrading, replacing, or removing a direct Go
module dependency in `go.mod` / `go.sum`.

## Load First

- `/specs/security.md` (Supply Chain section is the normative rule)

## Rule

Only direct-import packages from trusted, widely-used upstreams. The standard
library is always the first choice; reach for a third-party module only when
the standard library does not cover the need.

## Standing Rules

These two checks apply on every dependency add, upgrade, and periodic review:

1. **Community trust** — every direct dependency must meet all four Trust
   Criteria below. If a module no longer meets them (abandoned, licence
   change, maintainer incident), replace or remove it regardless of whether
   the API changed.
2. **Latest stable version** — direct dependencies must be pinned to the
   latest tagged stable release. Patch-level drift is not acceptable. When a
   module migrates to a new canonical import path and releases new versions
   only on that path (example: `gopkg.in/yaml.v3` → `go.yaml.in/yaml/v3`),
   migrate to the new path.

## Trust Criteria

A module qualifies as trusted-and-widely-used when **all** of the following
hold:

- **Source.** One of:
  - Go standard library or `golang.org/x/...`.
  - A CNCF or `kubernetes-sigs` project.
  - A vendor-published SDK for the platform being integrated (for example
    OpenShift, Kubernetes, libvirt, AWS, GCP, Azure SDKs).
  - An established maintainer with broad adoption — representative examples:
    `github.com/spf13/*`, `github.com/fatih/*`, `github.com/hashicorp/*`,
    `github.com/prometheus/*`, `github.com/grpc-ecosystem/*`,
    `github.com/go-yaml/*`.
- **Licence.** OSI-approved permissive: Apache-2.0, BSD-2/3-Clause, MIT,
  MPL-2.0, ISC. Copyleft (GPL/AGPL/LGPL) is not permitted without an
  explicit, documented decision in `/specs/security.md`.
- **Maintenance.** A tagged release within the last 12 months **or** clear
  evidence of active issue triage.
- **Pinning.** Pinned via `go.mod` + `go.sum`. No `replace` directives that
  point at a personal fork unless the deviation is justified in
  `/specs/security.md`.

## Process

1. Confirm the module meets all four trust criteria above and identify the
   prior art (which large project depends on it) before adding it.
2. Prefer the standard library or an existing direct dependency over a new
   one.
3. After editing `go.mod`, run `go mod tidy` so `go.sum` is complete and
   minimal.
4. Re-run repository validation per the `implementation-validation` skill.
5. If a module fails the trust criteria but is genuinely necessary, propose
   a deviation note in `/specs/security.md` first; only add the dependency
   after that spec change is accepted.

## Anti-Patterns

- Adding a module because a tutorial or AI snippet used it.
- Pulling in a personal fork (`github.com/<random-user>/<lib-fork>`) when
  the upstream still ships releases.
- Adding a module to call a one-line helper that the standard library
  already covers.
- Letting `go.sum` drift from `go.mod` (always run `go mod tidy`).
