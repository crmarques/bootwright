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
is an observation. Only the human owner accepts a row; an agent records only
observations. For the log digest, set `LOGS` to the directory the human result
names as `Logs` and run as root:

```sh
find "$LOGS" -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum
```

## Setup v2 fresh-install row

[B555](../specs/milestones/m1.md#b555)'s owner gate is one row per host, a
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

| Date | Build | Host OS | Example | Commands | Outcome | Log digest | Accepted by |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 2026-09-17 | unrecorded; predates `c42ea10` and every later change | unrecorded | `examples/lab-rhel` | `apply` of `rhel-01`, sequence unrecorded | The installation block completed in 1h10m7s: about ten minutes installing and the identity poll's full hour budget ([knowledge](../.agents/knowledge/installation-completion-proof.md)) | unrecorded | observation, not acceptance |
| 2026-10-05 | Commit 35de8f40c393637a4f64bae0933a8e7c84699dde; Source clean | Retained locally | `examples/lab-rhel` | Retained locally | B49 and B72 accepted; B49's running-disk read remains unqualified ([B277](../specs/milestones/m1.md#b277)) | Retained locally | Owner: B49 and B72, accepted on 2026-10-05 ([D58](../specs/milestones/backlog.md#decisions)) |
