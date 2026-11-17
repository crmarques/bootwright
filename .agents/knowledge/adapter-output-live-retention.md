# Live retention of adapter output

Observed on 2026-09-16. Required behavior stays in
[private operation logs](../../specs/cli/output.md#private-operation-logs).

## Symptom

An `apply` sat on one block for minutes with a `[RUNNING]` row, and
`tail -F` over the operation's `blocks` tree printed nothing. The retained
`attempt-NNNNNN.output` files of blocks that had already finished held their
whole Ansible transcript, so the plumbing worked; only a run still in flight was
invisible, which is exactly when an operator wants to read it.

## Mechanism

Two buffers of about eight kilobytes each, in series.

- **The Ansible child.** ansible-core's `lib/ansible/utils/display.py` writes the callback line and
  deliberately does not flush it. The flush is present but commented out, with a
  note that `TaskQueueManager.cleanup` performs a final flush at shutdown. With
  standard output on a pipe rather than a terminal, CPython block-buffers, so
  nothing leaves the child until it fills a buffer or exits.
- **The Go side.** `operationstore.AdapterOutput` published only once its own
  buffer reached `adapterOutputFlush`, so a short run added nothing to the file.

A block that prints a few kilobytes therefore reached disk only when it ended.

## Decisions

- Both runners launch the interpreter with `-u`
  ([process_linux_amd64.go](../../internal/reconciliation/ansiblerunner/process_linux_amd64.go),
  [runner_linux_amd64.go](../../internal/controller/ansiblelocal/runner_linux_amd64.go)).
  `PYTHONUNBUFFERED` cannot stand in for it: the same command line passes `-I`,
  which implies `-E`, so every `PYTHON*` variable is ignored. Verified by hand —
  a line written before a four-second sleep appears immediately under `-u -I`
  and not at all under `-I`, with or without the variable set.
- The later interval-based coalescer still required another write to notice
  elapsed time, so a wedged adapter could leave its last line buffered. Audit
  003 OUT-11 exposed that gap, and for a while `AdapterOutput` published each
  write synchronously, at one fsync of the log area per output burst. Audit 005
  G11-02 restored batching without the gap: a write publishes at once when
  8 KiB is pending, and otherwise the first pending byte starts a one-second
  timer that publishes it. `Close` stops the timer and publishes the rest
  under the output's mutex, and both callers defer it inside the callback that
  holds the area, so no timer write outlives the held area. See
  [logs.go](../../internal/reconciliation/operationstore/logs.go) and
  `TestAdapterOutputPublishesItsFinalLineWithoutWaitingForAnother` in
  [logs_test.go](../../internal/reconciliation/operationstore/logs_test.go).

## How to apply

Do not drop `-u` when touching either argument list; it looks like a stylistic
flag and is load-bearing. Both runner tests assert the exact
`-u -I -B -S -c` sequence. Any future adapter that is not Python needs its own
equivalent, because retention on this side cannot compensate for a tool that
withholds its output until it exits.

Reading it needs root, because the log root is `0700` and root-owned. Follow the running
blocks' output with `bootwright logs --context <ctx> --follow`, which asks for
authorization itself, or tail the files themselves:

```sh
sudo sh -c 'tail -F /var/log/bootwright/contexts/<ctx>/operations/<op>/blocks/*/*.output'
```
