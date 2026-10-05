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

## Ledger

| Date | Build | Host OS | Example | Commands | Outcome | Log digest | Accepted by |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 2026-09-17 | unrecorded; predates `c42ea10` and every later change | unrecorded | `examples/lab-rhel` | `apply` of `rhel-01`, sequence unrecorded | The installation block completed in 1h10m7s: about ten minutes installing and the identity poll's full hour budget ([knowledge](../.agents/knowledge/installation-completion-proof.md)) | unrecorded | observation, not acceptance |
| 2026-10-05 | Commit 35de8f40c393637a4f64bae0933a8e7c84699dde; Source clean, as `bootwright version` printed them at 11:14 UTC in a check the recording session ran on the unchanged binary after the run; no capture holds `version` output | Fedora 43, x86_64 | `examples/lab-rhel`, imported from a copy whose only edits are its lab conventions: the controller address 192.168.1.110 for 192.0.2.1, the upstream resolver 192.168.1.1 and the time source 162.159.200.1 | Without the README's `make build`: the run used the binary built on 2026-10-03 at that commit. Before the restart: `setup` twice, the first at 10:42:33 UTC missing from the capture, its result unrecorded; `media list --checksums`, without the two `media add` lines, since both images were already stored and verified; `context init --name lab-rhel`; `context update --name lab-rhel --input-dir <the copy> --yes`; `secret generate`; `secret check`; `apply --stage controller`; `preflight controller --context lab-rhel`; `plan`; `apply --stage infra-components`; the README's `curl`, `dig`, `chronyd -Q -t 3` and `systemctl` checks; `apply`; `status`; four `status --output json`: with cached sudo credentials, the same with standard input from `/dev/null`, after `sudo -k`, and with standard input from `/dev/null` and cached credentials under a sudoers rule denying `/proc/*/exe` and the executable; `apply`; `destroy --authorize data-loss`; `machine stop --name rhel-01`; `destroy --authorize data-loss`; `apply`; `sudo systemctl reboot`. After it: `machine start --name rhel-01`; `machine stop --name rhel-01`; `destroy --authorize data-loss`; `status` | The logged `setup` reported `unchanged`. Apply `op-9727ed61` paused after the controller stage and after the infra-components stage, then ended `done`, the installation taking 7m29s; the artifact server answered 404, the proxy `HTTP/1.1 200 OK`, both names resolved and all four units ran, but `chronyd -Q -t 3` timed out against a server that had selected its upstream source 36 seconds earlier ([B276](../specs/milestones/backlog.md#b276)). Each JSON `status` wrote one document and nothing to standard error: exit 0 twice, then exit 1 with `runtime.privilege` naming the missing password, and exit 1 with `runtime.privilege` carrying sudo's denial of `/proc/<pid>/exe`. The repeated `apply` settled with no effect; the first `destroy` refused `lifecycle.live`, naming `machine stop`, and registered nothing; destroy `op-9b0ac177` ended `done`; apply `op-0fbe7b3b` ended `done`, the installation taking 6m52s. After the restart the controller address was unchanged, `machine start` powered rhel-01 on through its emulated BMC and `machine stop` off, and destroy `op-542f69d9` ended `done` with `next: none`, its absence proofs leaving no guest, network or pool; a check the recording session ran at 11:14 UTC found no `virbr-lab` bridge. Each removal, and the restart, ended the proxy and BMC units `failed` with status 137 ([B270](../specs/milestones/backlog.md#b270)), and the final `status` reported the removed services `[OK]` ([B275](../specs/milestones/backlog.md#b275)). Every domain inspection the libvirt machine role ran, eleven in both applies' and both removals' machine blocks and in the quiescence probe of each `destroy`, read `/proc/1/net/tcp` under the elevated worker, which fails on a table it cannot read, and the removals' absence proofs used the listener it reported. The quiescence probe of the first `destroy` also ran `qemu-img info --force-share` on the running domain's disk, but a failed read reports size 0, the probe's decision never reads the disk size (this one rested on the domain state, `its domain is running`) and keeps no evidence, so this row does not show that read succeeding; both applies proved the same read of the shut-off domain's disk at its frozen size. The build descends from X22's landing and from `8aa4494`, the baselines of [B49](../specs/milestones/delivered.md#x38--the-real-host-run-of-2026-10-05) and [B72](../specs/milestones/m4.md#b72) | `op-9727ed61`: 0d6b4b3e8333732a8beeed8e68a24ec800ea1ec92fa728e5af3f29bd2b6452a3; `op-9b0ac177`: 7e57317d3c5353ece32856c5b7ca21c4dc14c7d42730775112304a7c18914336; `op-0fbe7b3b`: 2f144ba0d95fd53d55b266768af0d440c201815f5fbc1dca9a777c26e9d3ac3e; `op-542f69d9`: 99081fa97f08aa4dc5f10ca2314926f290a14116c6d30d986bf13e4257c45ce1; machine runs `run-ddfe85f9`: 65d672b8f2a2246563abd435edff076832556374fd4bcf65b1b76edc49909ba4, `run-e0a3cd4b`: 975850e3d301ebcc67743d3fb86d14498f8f6415545b38e6def2d90fbc8b782d, `run-902fbc34`: 89a470d4c8c95e2f9311b690593415a46a2bc0c32f845f6dbc3b87f10a61802d | Carlos Marques: B49, accepted on 2026-10-05 and recorded at the owner's request; B49's `qemu-img info --force-share` clause stays unqualified ([B277](../specs/milestones/backlog.md#b277)) |
