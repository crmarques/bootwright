# ansible-core retries and task timeouts

Observed on ansible-core 2.21.4, the version the collection qualifies and the
one `./scripts/ansible-check` installs.

An `until` loop does not re-template a task's arguments between attempts.
`TaskExecutor._execute_internal` post-validates the task, which templates its
arguments, once ([task_executor.py line 443](https://github.com/ansible/ansible/blob/v2.21.4/lib/ansible/executor/task_executor.py#L443)) before the retry loop
starts ([line 530](https://github.com/ansible/ansible/blob/v2.21.4/lib/ansible/executor/task_executor.py#L530)). A value such as a remaining time written into `argv` is
therefore computed for the first attempt only; a wait that must shrink per
attempt computes its bound inside each attempt, for example in an included
file looped over bounded attempt numbers.

The task keyword `timeout` fails the task when its alarm fires around an
attempt ([line 534](https://github.com/ansible/ansible/blob/v2.21.4/lib/ansible/executor/task_executor.py#L534)), but it does not stop the module's process. A
`command: sleep 25` task with `timeout: 2` failed after 2 seconds, and its
`sleep` was still running after the play had ended. A task bounded this way
must be safe to leave running, or its process must be ended another way.

GNU `timeout --kill-after` run through `ansible.builtin.command` reports the
killed child as rc `-9`, Python's negative-signal convention, not the `137` a
shell prints; the plain expiry is rc `124`. A classification of a spent budget
therefore matches `124` and `-9`, and cannot tell a `-9` from a kill by
anything else.

An `until` expression is evaluated over every attempt's result, a failed one
included, and an expression that cannot be evaluated ends the task at once: the
evaluation error becomes `An 'until' expression failed.`
([task_executor.py lines 604-612](https://github.com/ansible/ansible/blob/v2.21.4/lib/ansible/executor/task_executor.py#L604-L612))
and no further attempt runs. A module that fails returns only its failure, so
an expression reading a key only a success carries, such as
`result.power == 'Off'`, cannot be evaluated over it. Written as
`(result.power | default('')) == 'Off'`, a failed attempt is one that has not
reached the state yet, so it spends one attempt instead, and once the attempts
are spent the task fails with the module's own message. Re-run on 2026-09-29
with a `command: /bin/false` task registered as `result`, `retries: 3` and
`delay: 0`: unguarded, it ended at its first attempt with
`object of type 'dict' has no attribute 'power'`; guarded, it reported
`attempts: 3` and the command's own message. The Redfish polls that wait on a
read ([substrates](../../specs/substrates.md#identity-and-power-operations))
are guarded this way.

The install role's waits (X16, Z2 cluster budgets) follow all three
([container clusters](../../specs/container-clusters.md)).

`failed_when: false` decides even a poll that runs out. Re-run on 2026-09-30
against `localhost` with a `no_log` file task that failed at every attempt:
registered with `failed_when: false`, `retries: 2` and an `until` it never
met, it reported `ok`, `failed: false`, `attempts: 2` and the module's own
`msg`; ending its poll on `attempts` instead, it reported `attempts: 3`. A
hidden single step that failed registered its `msg` the same way, and a
success registered none. That is what lets a hidden task hand its refusal to a
separate step outside `no_log`, as the
[security rule](../../specs/security.md#logs-output-and-diagnostics) requires
of a management controller's refusal; the collection's
[structural rules](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/test_role_boundaries.py)
hold every role to it.
