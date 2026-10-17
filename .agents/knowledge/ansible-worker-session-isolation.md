# Ansible worker session isolation

ansible-core 2.21 starts each task worker in its own session: the worker calls
`setsid` (`executor/process/worker.py`, under the `WORKER_SESSION_ISOLATION`
setting, default true) and redirects its standard streams to `/dev/null`. A
signal sent to the playbook's process group therefore misses the workers and
the modules they run, which keep going until their task ends.

Bootwright's lifecycle supervisor is a child subreaper, so it stops an adapter
by walking and signalling its own descendants rather than by killing a process
group, and cancellation signals the supervisor before the group kill. A
parent-death signal armed in the supervisor and the playbook does not reach the
forked workers, so a supervisor killed outright (SIGKILL, the OOM killer) still
leaves them running; [B23](../../specs/milestones/m1.md#b23) tracks that gap.

Lesson: never treat a process-group kill as reaping an Ansible run.
