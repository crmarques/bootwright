# What a looped, hidden, failure-suppressing task leaves behind

Observed 2026-09-17 with the pinned ansible-core while building the bounded
power reading behind `machine list --power-status`, which polls many management
controllers in one loop and must survive one of them not answering.
[The CLI spec](../../specs/cli.md#resource-inspection-and-explicit-access) owns
what a reading reports; this page records the two result-shape facts that
decide how the protocol plugin reads its own loop.

## `no_log: true` hides the display, not the register

A looped task carrying a secret through a lookup must set `no_log: true`, and
the callback then prints `(censored due to no_log)` for each item. The
registered variable is untouched: `probe.results[i]` still carries `item`,
`stdout`, `rc` and every other returned key, so a later task can read the
result it just hid.

That is what lets a reading publish evidence keyed by machine: each result
carries back its own `item`, which is the frozen survey target, so results are
matched to machines by what the loop returned rather than by position.

## `failed_when: false` rewrites `failed` to false

A loop that must keep polling after one item refuses sets `failed_when: false`.
ansible-core then reports that item as `ok` and the registered result reads:

```json
{"failed": false, "failed_when_result": false,
 "failed_when_suppressed_exception": "(traceback unavailable)",
 "item": {"object": "node-b"}, "msg": "the controller did not answer"}
```

So `result.failed` is **not** the signal that an item refused — it is false for
exactly the failures the suppression was written to tolerate. A module that
refuses through `fail_json` returns only its message, so the usable signal is
that the result carries no state the consumer can name.

The reading protocol therefore decides on the reported state: a result whose
`power` is missing, empty or outside `On`/`Off` is published as `unknown`. It
still refuses a result flagged `failed` or `unreachable`, because such a result
is not an answer either, but that branch is not what a suppressed refusal
takes. See
[`machine_power_protocol.observed`](../../ansible/collections/ansible_collections/bootwright/core/plugins/action/machine_power_protocol.py)
and its cases in
`tests/unit/plugins/action/test_machine_power_protocol.py`.

Both facts were checked by running a two-task probe playbook against
`localhost` with the gate's own interpreter rather than reasoning from the
documentation.
