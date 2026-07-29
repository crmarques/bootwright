# Tests

Repository tests run from the project root.

- `test/e2e/<case>/`: self-contained Bootwright input sets for real host
  validation and apply flows.
- Go unit and rendering fixtures live with the package tests that consume
  them.

E2E cases are runnable test assets. Canonical user-authored examples live under
`/examples/`; keep the two in sync when schema examples change.

## E2E Case Names

Case names describe the bastion/substrate shape — where the controller
runs, what provider the cluster lands on, and the cluster topology. OCP
install mode (connected vs. disconnected) is documented in each case's
`README.md`, not the case name.

[`test/e2e/README.md`](e2e/README.md) carries the current case roster and each
case's layout; `make list-e2e-cases` prints the same list from the tree.

## Running A Case

```text
make build
make list-e2e-cases
make e2e-dry-run CASE=001-sno-libvirt
make e2e         CASE=001-sno-libvirt
make clean-e2e-state CASE=001-sno-libvirt
```

The user-facing equivalent is plain `bootwright`:

```text
bootwright context init --name <case> -f test/e2e/<case> --yes
bootwright preflight bastion
bootwright preflight infra --dry-run
bootwright preflight all --dry-run
bootwright plan
bootwright apply --yes
```

## Running All Cases

Use this from the repository root when every fixture's provider hosts,
networks, BMCs, VIPs, and secrets are available. Cases run sequentially and
stop on the first failure.

Dry-run every fixture:

```bash
set -euo pipefail
for case_dir in test/e2e/[0-9]*; do
  case_name=$(basename "$case_dir")
  make e2e-dry-run CASE="$case_name"
done
```

Apply, destroy, and remove generated state for every fixture:

```bash
set -euo pipefail
for case_dir in test/e2e/[0-9]*; do
  case_name=$(basename "$case_dir")

  make e2e CASE="$case_name"
  bin/bootwright context init --name "$case_name" -f "$case_dir" --yes
  bin/bootwright destroy --stage clusters --yes
  bin/bootwright destroy --stage infra --yes
  make clean-e2e-state CASE="$case_name"
done
```

For bastion/container setup, context input customization, secrets, verification,
and log-following, use [`test/e2e/README.md`](e2e/README.md).

## Common Prerequisites

- Linux host with KVM support (`/dev/kvm`) for libvirt cases.
- Go toolchain compatible with `go.mod`.
- Python 3.12+ on the controller, or permission for `bootwright bastion setup`
  to install it.
- Permission to escalate to root on provider hosts and manage the Bootwright
  host runtime under `/var/lib/bootwright`.
- Required install secrets in the active context secrets directory; use
  `bootwright secret set`, `bootwright secret generate`, and
  `bootwright secret list`.

## Logs And Artifacts

Authored inputs stay in the workspace directory recorded by `context init`
(the fixture directory itself); edits there are picked up directly by the next
command. Reviewable generated output lives under
`/var/lib/bootwright/contexts/<context>/rendered/`, secret-inlined installer
runtime output under `clusters/<cluster>/runtime/installer/`, managed service
files under `managed-services/`, provider state under `provider-state/`, and
apply ledgers plus per-run input snapshots under `runs/`. Failed phases print
the relevant log path.
