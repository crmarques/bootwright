# Operator Acceptance Ledger

This ledger records operator-run gates: journeys run by hand on a real host,
which no in-tree test can perform. [Milestones](../specs/milestones.md#completion-and-verification)
owns the rule that uses it: an item with an operator gate completes only on a
row here that matches it. The [operator guide](operator-guide.md#record-a-run)
walks a run from a prepared host to a row.

## Row format

| Field | Content |
| --- | --- |
| Date | The day the journey ended, `YYYY-MM-DD`. |
| Build | The `Commit` and `Source` lines `bootwright version` prints. |
| Host OS | Distribution, release and architecture of the controller host. |
| Example | The example directory the journey ran, and any local edits beyond its lab conventions. |
| Commands | The `bootwright` command sequence, in order, as it was run. |
| Outcome | The final `status` of each operation and every refusal met on the way. |
| Log digest | SHA-256 of each operation's log directory, computed as below. |
| Accepted by | The human owner who accepted the row and the item it accepts, or `observation, not acceptance`. |

Each item with an operator gate records an acceptance baseline commit in its
section of its [milestone page](../specs/milestones.md#status). A row matches
that item when its build descends from the baseline and its source stamp is
`clean`; any other row, including one run before the item records a baseline,
is an observation. Only the human owner accepts a row; an agent may record
that explicit acceptance but cannot supply it. Confidential environment fields,
command history, exact outcomes and log digests are retained locally; the public
row records the accepted qualification and its build. For the log digest, set
`LOGS` to the directory the human result names as `Logs` and run as root:

```sh
find "$LOGS" -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum
```

## Setup v2 fresh-install row

[B555](../specs/milestones/delivered.md#x93--the-owner-run-of-2026-10-14)'s owner gate is one row per host, a
Fedora 44 controller and a RHEL 9 VM, run on a build that contains X90 after
the [fresh install](operator-guide.md#a-host-an-earlier-bootwright-build-manages)
removed the earlier store. It runs no example, so its log digest covers
setup's runs (`/var/lib/bootwright/setup/runs`) and the controller stage's
operation. Copy this template into the ledger and replace every `<...>`:

```text
| <YYYY-MM-DD> | Commit <sha>; Source clean | <Fedora 44 or RHEL 9.x> amd64, <dnf4 or dnf5> | none: setup v2 fresh install (B555); controller stage of <context> | setup --dry-run; setup --yes (fresh); setup --yes (ready); setup --yes on <same-lock build commit>; apply --context <context> --stage controller; preflight controller | fresh setup <s> wall, <s> of it dnf (from run.output), target 60 s without dnf; ready <s>, target 2 s, "Nothing to do."; same-lock build <s>, target 2 s, "Nothing to do."; wheel cache <bytes>/<files> and runtime <bytes>/<files>, target 60 MB and 3,000 files; controller stage <s> with nothing missing; preflight controller <ready or not>; dnf preview captured <yes/no>, core-package note <yes/no>; Redfish TLS to <BMC> under DEFAULT <ok/refused> and FUTURE <ok/refused>; refusals: <none or each one> | <sha256> | observation, not acceptance |
```

Measure the bytes with
`sudo du -sb /var/lib/bootwright/setup/cache/wheels /var/lib/bootwright/setup/runtimes`
and count each directory's files with `sudo find <directory> -type f`. Switch
the crypto policy with `sudo update-crypto-policies --set FUTURE`, and back
with `--set DEFAULT`, around the Redfish check.

## Ledger

**B615 retrospective baseline:** X99 records B615's implementation baseline
`fa4b87d0f0fa028bb910c1e071d6cd08bc1dd5a2` when closing the completed journey
under the owner's acceptance in conversation. The baseline implementation
preceded the run; its designation on the milestone page is retrospective.
The B615 row is an explicit exception to advance baseline registration above;
its clean builds still descend from that implementation. Future operator
gates retain the advance-registration requirement.

| Date | Build | Host OS | Example | Commands | Outcome | Log digest | Accepted by |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 2026-09-17 | unrecorded; predates `c42ea10` and every later change | unrecorded | `examples/machines/lab-rhel` | `apply` of `rhel-01`, sequence unrecorded | The installation block completed in 1h10m7s: about ten minutes installing and the identity poll's full hour budget ([knowledge](../.agents/knowledge/installation-completion-proof.md)) | unrecorded | observation, not acceptance |
| 2026-10-05 | Commit 35de8f40c393637a4f64bae0933a8e7c84699dde; Source clean | Retained locally | `examples/machines/lab-rhel` | Retained locally | B49 and B72 accepted; B49's running-disk read remains unqualified ([B277](../specs/milestones/delivered.md#x93--the-owner-run-of-2026-10-14)) | Retained locally | Owner: B49 and B72, accepted on 2026-10-05 ([D58](../specs/milestones/backlog.md#decisions)) |
| 2026-10-15 | Commit 80f18c9374a471436ad3d12fc1e7fee1fe48dd3b; Source clean | Retained locally | Retained locally | Retained locally | Required apply/replay, live-resource destroy refusal, stopped-resource teardown, restart recovery, clock and disk checks, JSON privilege cases, interruption and continuation accepted. Exact outcomes and refusal evidence retained locally. | Retained locally | Owner: B19, B20, B270, B276, B277 and B339, accepted on 2026-10-15 (D154) |
| 2026-10-14 | Commit 80f18c9374a471436ad3d12fc1e7fee1fe48dd3b; Source clean | Retained locally | Retained locally | Retained locally | D154 accepts runtime verification, fresh/ready/same-lock setup, measured timing/cache budgets, controller readiness and crypto-policy checks. Exact measurements and package-preview exceptions retained locally. | Retained locally | Owner: B549 and B555, accepted on 2026-10-15 (D154) |
| 2026-10-15 | Commit 80f18c9374a471436ad3d12fc1e7fee1fe48dd3b; Source clean | Retained locally | Retained locally | Retained locally | D154 accepts fresh/ready/same-lock setup, measured timing/cache budgets, controller readiness, interrupted apply and continuation. Exact measurements, package preview and applicability notes retained locally. | Retained locally | Owner: B555 and B339, accepted on 2026-10-15 (D154) |
| 2026-10-15 | Commit 80f18c9374a471436ad3d12fc1e7fee1fe48dd3b; Source clean | Retained locally | Retained locally | Retained locally | Concurrent infrastructure and same-profile installations, shared publication, replay and teardown passed. After host restart the managed network was inactive and guest starts refused (B608); observed-state reporting omitted this (B610). Exact outcomes retained locally. | Retained locally | observation, not acceptance |
| 2026-10-16 | Commit fcff2f58f397cc29517e082dd6f6f636a8ef912c; Source clean | Retained locally | Retained locally | Retained locally | B476 console evidence through teardown and purge; dashboard fingerprint comparison, successful login and expected host/OSD inventory confirmed by owner attestation. Exact outcomes and image digest retained locally. Post-destroy inspection deviation tracked and corrected as B613/X94; no live rerun of that correction claimed. | Retained locally | Owner: B476, accepted on 2026-10-16 ([D155](../specs/milestones/backlog.md#decisions)) |
| 2026-10-16 | Commit 591af9616446596a77a599cd6303970a85131694; Source clean | Retained locally | Retained locally | Retained locally | B466 and B608: fresh apply, controller host restart, automatic network and managed-service recovery, explicit guest startup and connectivity, stopped-resource teardown, absence checks, verified evidence archive and context purge passed. Earlier concurrency, shared-publication and replay evidence carried forward. Exact outcomes and all operation-log digests retained locally. | Retained locally | Owner: B466 and B608, accepted on 2026-10-16 ([D156](../specs/milestones/backlog.md#decisions)) |
| 2026-10-17 | Commits aa7f4557efc582e134e47457d0aabb5e1369843a (negative selection), 082ae7cfa29c1d07e69bc338ace0dcd3a0b9219e (apply), 288165004c6ed4cb6ca95031a744ea17708e768d (continuation and retirement); Source clean | Retained locally | Retained locally | Retained locally | B615's separate native invalid-selection refusal and authored-artifact journey accepted through readiness, RBD data and client checks, dashboard certificate/login, explicit targeted recovery, stopped-host teardown, owned absence, verified private archives, purge and retained-media verification. B476's earlier acceptance remains separate. No fresh unattended core-v6 run or all-version qualification is claimed. Exact outcomes retained locally. | Retained locally | Owner: B615 and M2b, accepted on 2026-10-17 ([X99](../specs/milestones/delivered.md#x99--operator-selected-ceph-artifacts-and-m2b-completion)) |
