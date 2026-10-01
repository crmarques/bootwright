# What a failed task under `no_log` prints

Observed 2026-09-30 with the pinned ansible-core 2.21.4, reproducing what X31
found. [The security rule](../../specs/security.md#logs-output-and-diagnostics)
owns what the adapter's output may carry; this page records why the adapter
prints through a callback of its own.

## A censored result keeps what the task raised

ansible-core replaces a hidden task's result with its `censored` notice but
keeps a fixed set of keys, `exception`, `warnings` and `deprecations` among
them (the `PRESERVE` set in ansible-core's ansible/_internal/_task.py), and
`ansible.builtin.default` prints those whole, before the censored body,
through `_handle_warnings_and_exception`:

```text
TASK [assert templating a value] ***********************************************
[ERROR]: Task failed: Action failed: secret is S3CRET-VALUE
Origin: .../play.yml:5:7
...
fatal: [localhost]: FAILED! => {"censored": "the output has been hidden due to the fact that 'no_log: true' was specified for this result", "changed": false}
```

The value printed the same way through a module's `fail_json` naming an
argument (`file (S3CRET-VALUE) is absent`), a lookup error (`Unable to access
the file 'S3CRET-VALUE'`), a template error (`object of type 'dict' has no
attribute 'S3CRET-VALUE'`), a `when` or `loop` expression error, an `until`
poll that ran out, a failure a `rescue` caught, a loop item's failure and a
module's own `warn()`. `ignore_errors` did not stop it; `failed_when: false`
did, because that result is `ok` and carries no exception.

The adapter's output therefore goes through `bootwright.core.censored`, which
`ansible/ansible.cfg` names: it prints what the default prints and skips those
keys for a censored result, so a hidden task that fails shows only its name
and the censored notice.

## An identical message prints once

ansible-core's `Display` prints a given `[ERROR]` or `[WARNING]` text once per
run and drops a later identical one. A playbook written as JSON gives its
errors an origin with no line, so two tasks raising the same text print it
once. A test that compares what each task printed gives each task a value of
its own, as the collection's `test_censored_output.py` does.
