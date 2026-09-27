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

The install role's waits (X16, Z2 cluster budgets) follow all three
([container clusters](../../specs/container-clusters.md)).
