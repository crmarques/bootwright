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

Current cases:

- `001-sno-libvirt` — Bootwright CLI runs inside a UBI9
  container; the environment bastion points at the libvirt host on the same
  machine through the fixture's declared Host SSH address; one SNO cluster.
- `002-sno-emul-baremetal` — single-node bare-metal shape backed by
  emulated services.
- `003-3nodes-libvirt` — same bastion/provider shape with one compact
  3-node OpenShift cluster.
- `004-3nodes-emul-baremetal` — three-node bare-metal shape backed by
  emulated services.
- `005-3nodes-baremetal` — three-node real bare-metal shape with bastion
  artifact publishing, external proxy/DNS, bonded VLAN node networking, and
  OpenShift-managed VIPs.

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
bootwright context init <case> -f test/e2e/<case> --base-dir /tmp/bootwright-<case> --yes
bootwright check bastion
bootwright check infra --dry-run
bootwright check all --dry-run
bootwright apply all --dry-run
bootwright apply all --yes
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
  base_dir="/tmp/bootwright-$case_name"

  make e2e CASE="$case_name"
  bin/bootwright context init "$case_name" -f "$case_dir" --base-dir "$base_dir" --yes
  bin/bootwright destroy cluster --yes
  bin/bootwright destroy infra --yes
  make clean-e2e-state CASE="$case_name"
done
```

For bastion/container setup, context input customization, secrets, verification,
and log-following, use [`test/e2e/README.md`](e2e/README.md).

## Common Prerequisites

- Linux host with KVM support (`/dev/kvm`) for libvirt cases.
- Go toolchain compatible with `go.mod`.
- Python 3.12+ on the controller, or permission for `bootwright apply bastion`
  to install it.
- Permission to escalate to root on provider hosts and manage the Bootwright
  host runtime under `/var/lib/bootwright`.
- Required install secrets in the active context secrets directory; use
  `bootwright secret set`, `bootwright secret generate`, and
  `bootwright secret list`.

## Logs And Artifacts

Generated state defaults to `~/bootwright/<context>/state`; the Makefile e2e
targets use `/tmp/bootwright-<case>/state`. Apply task logs are written under
`/var/lib/bootwright/contexts/<context>/workflow/`. Failed phases print the
relevant log path.
