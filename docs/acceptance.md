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

| Date | Build | Host OS | Example | Commands | Outcome | Log digest | Accepted by |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 2026-09-17 | unrecorded; predates `c42ea10` and every later change | unrecorded | `examples/lab-rhel` | `apply` of `rhel-01`, sequence unrecorded | The installation block completed in 1h10m7s: about ten minutes installing and the identity poll's full hour budget ([knowledge](../.agents/knowledge/installation-completion-proof.md)) | unrecorded | observation, not acceptance |
| 2026-10-05 | Commit 35de8f40c393637a4f64bae0933a8e7c84699dde; Source clean | Retained locally | `examples/lab-rhel` | Retained locally | B49 and B72 accepted; B49's running-disk read remains unqualified ([B277](../specs/milestones/delivered.md#x93--the-owner-run-of-2026-10-14)) | Retained locally | Owner: B49 and B72, accepted on 2026-10-05 ([D58](../specs/milestones/backlog.md#decisions)) |
| 2026-10-15 | Commit 80f18c9374a471436ad3d12fc1e7fee1fe48dd3b; Source clean | Fedora 44 amd64, dnf5 5.4.6.0, sudo 1.9.17p2 | `examples/lab-rhel` with the lab conventions only: controller and services at 192.168.1.110, DNS upstream 1.1.1.1, NTP upstream 162.159.200.1; for B339 also a context `b339-fedora` of one managed proxy on port 3129 | context init/update; secret generate; secret check; apply --stage controller; preflight controller; plan; apply --stage infra-components; apply; status; machine list --power-status; machine exec; machine rsh; diff; diff --observe; status --output json (sudo cached; stdin /dev/null; after sudo -k; under a policy denying /proc/[0-9]*/exe and the executable); machine restart; apply; destroy --authorize data-loss; machine stop; destroy --authorize data-loss; apply; host restart; machine start; machine stop; destroy --authorize data-loss; status; context delete --purge; for B339: apply interrupted with Ctrl-C, status, apply, destroy | apply op-cea6a286 done (install 8m24s), replay settled; destroy refused `lifecycle.live`, then op-364c39ed done; apply op-c2301f28 done; after the host restart start on (it was off), stop, destroy op-0985ef89 done, no unit left; chronyd printed an offset; qemu-img read 64424509440 while running; JSON cases exit 0, 0, 1 `runtime.privilege` (password), 1 `runtime.privilege` (denied), each one document, empty stderr; B339: one Ctrl-C recorded the block unknown, exit 130, terminal sane, the next apply finished it; refusals: `lifecycle.live` as designed | op-cea6a286 e7f5cb3120c959f3a5bbb68cf2fe95dcc9feba4c72e0267297c235da83958335; op-364c39ed d3af4af5d4934369b19ec937cff39b099f5aa0f438a164dc290621a03a9442bf; op-c2301f28 e88287428fce243709f27721d4ff40b85bd155ba4da6c68d018276bca55bd279; op-0985ef89 5ca38d508cb80128ca8f6d06d5f3d58dd05a6f751908d30dc92c55a65125ef59 | Owner: B19, B20, B270, B276, B277 and B339, accepted on 2026-10-15 (D154) |
| 2026-10-14 | Commit 80f18c9374a471436ad3d12fc1e7fee1fe48dd3b; Source clean | Fedora 44 amd64, dnf5 5.4.6.0 | none: setup v2 fresh install (B555); `scripts/runtime-lock verify` (B549); controller stage of `lab-rhel` | scripts/runtime-lock verify; setup --dry-run; setup --yes (fresh); setup --yes (ready); setup --yes on 3670ca028 (same lock); apply --context lab-rhel --stage controller; preflight controller | verify: 17 files verified; fresh setup 7.2 s, 0 s of it dnf (both host packages present before bootwright; run.output empty), target 60 s without dnf; ready 0.79 s, "Nothing to do."; same-lock build 1.01 s, "Nothing to do."; wheel cache 10,502,976 B/11 files and runtime 40,719,692 B/2,477 files, target 60 MB and 3,000 files; controller stage 8.6 s with nothing missing; preflight controller ready; dnf preview captured no (nothing to install), core-package note no; Redfish TLS to the emulated BMC of rhel-01 under DEFAULT ok and FUTURE ok; refusals: none | setup-000001 0609c8bb70a5c07476797ee8226f69bda81e579826a6f66a613c5321555594e0; controller stage within op-cea6a286 | Owner: B549 and B555, accepted on 2026-10-15 (D154) |
| 2026-10-15 | Commit 80f18c9374a471436ad3d12fc1e7fee1fe48dd3b; Source clean | RHEL 9.8 amd64 (a guest of `b555-vm`, `examples/lab-rhel` renamed whose install profile adds the hosted DVD tree's BaseOS and AppStream as repositories), dnf 4.14.0, sudo 1.9.17p2 | none: setup v2 fresh install (B555); controller stage of `vm-controller`, the guest as its own controller with one managed proxy; B339 | setup --dry-run; setup (declined, B609); setup --yes (fresh); setup --yes (ready); setup --yes on 3670ca028 (same lock); context init/update; apply interrupted with Ctrl-C; status; apply --yes; preflight controller; destroy --yes; context delete --purge | fresh setup 42.7 s, 36 s of it dnf, target 60 s without dnf; ready 0.63 s, "Nothing to do."; same-lock build 0.67 s, "Nothing to do."; wheel cache 10,507,779 B/11 files and runtime 41,152,010 B/2,477 files; controller stage: no controller block (container runtime present after setup); preflight controller ready; dnf preview captured yes (podman, python3.14, 21 packages, 33 MB), core-package note yes; Redfish TLS n/a, no BMC on this controller; B339: one Ctrl-C recorded the block unknown, exit 130, terminal sane, the next apply finished it; refusals: none | setup-000001 2578c12daec7b7179711ac6adb6f6310f2f34c3d3d38a6ace53d8a5138b47a4b | Owner: B555 and B339, accepted on 2026-10-15 (D154) |
| 2026-10-15 | Commit 80f18c9374a471436ad3d12fc1e7fee1fe48dd3b; Source clean | Fedora 44 amd64, dnf5 5.4.6.0 | `examples/lab-rhel-3-nodes` with the lab conventions and `memoryMiB` 4096 per guest | context init/update; secret generate; secret check; apply --stage controller; preflight controller; plan; apply --stage infra-components; apply; status; machine list --power-status; machine exec x3; apply; machine stop x3; destroy --authorize data-loss; apply; host restart; machine start x3 (failed); diff --observe; destroy --authorize data-loss; context delete --purge | apply op-b8efab61 done: services overlapped, three machines beside the tree, three installs 01:03:27-01:12:12 UTC, one tree block; replay settled; destroy op-6ce72e05 done, no unit or image directory; apply op-1de0ca96 done: services and libvirt host together, installs 01:18:19-01:27:24 UTC; after the host restart the guest network was inactive (firewalld startup race, B608), each `machine start` failed `lifecycle.state` (BMC HTTP 500); diff --observe showed no difference (B610); destroy op-ae0a1115 done, no unit or image directory | op-b8efab61 ec45353def55876b4e95fbc61aa956a479b09fa2f423864e322b047c825cfa34; op-6ce72e05 e267d22a356397411cc46c99f61f7358cfc9b267cf70628d1ba13d3ac6621822; op-1de0ca96 35ae697495976c2b006e44ea29d4593aecb6b0d009625b31f52e9509b0b56d4b; op-ae0a1115 f19a669b2c541d2200afb644b7fd87b0d275eaf36fa1d5552248eba7251bcdcf | observation, not acceptance |
