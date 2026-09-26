# Operator Acceptance Ledger

This ledger records operator-run gates: journeys run by hand on a real host,
which no in-tree test can perform. [Milestones](../specs/milestones.md#completion-and-verification)
owns the rule that uses it: a slice with an operator gate completes only on a
row here that matches it.

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
| Accepted by | The human owner who accepted the row and the slice it accepts, or `observation, not acceptance`. |

Each slice with an operator gate records an acceptance baseline commit in its
[milestones](../specs/milestones.md#open-slices) section. A row matches that
slice when its build descends from the baseline and its source stamp is
`clean`; any other row, including one run before the slice records a baseline,
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
