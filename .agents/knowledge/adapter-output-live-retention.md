# Live retention of adapter output

Observed on 2026-09-16. Required behavior stays in
[private operation logs](../../specs/cli/output.md#private-operation-logs).

## Symptom

An `apply` sat on one block for minutes with a `[RUNNING]` row, and
`tail -F` over the operation's `logs` tree printed nothing. The retained
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
- `AdapterOutput` publishes on a bounded delay (`adapterOutputInterval`) as well
  as on its size threshold, and its first write is always published. It is not
  published per line because `operationArea.Append` walks the whole operation
  subtree through `capacity` on every call, so a line-at-a-time log would pay
  that walk thousands of times.
- The flush time is recorded even when the write fails, so an area that is
  refusing writes is retried on the interval rather than on every line.

## How to apply

Do not drop `-u` when touching either argument list; it looks like a stylistic
flag and is load-bearing. Both runner tests assert the exact
`-u -I -B -S -c` sequence. Any future adapter that is not Python needs its own
equivalent, because retention on this side cannot compensate for a tool that
withholds its output until it exits.

Reading it needs root, because the state tree is `0700` and root-owned, and a
non-root shell cannot even expand a glob into it:

```sh
sudo sh -c 'tail -F /var/lib/bootwright/contexts/<ctx>/state/operations/<op>/logs/blocks/*/*.output'
```
