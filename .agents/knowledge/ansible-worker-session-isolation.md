# Ansible worker session isolation

The pinned ansible-core 2.21 task worker starts its own session in
`executor/process/worker.py`. A signal to the playbook process group therefore
misses its workers and their modules.

Both the lifecycle supervisor and its playbook child are Linux child
subreapers. Cancellation signals the outer supervisor before a group kill;
the supervisor walks and kills its descendants. If the supervisor itself is
SIGKILLed, the surviving playbook child receives its parent-death `SIGUSR2`,
walks and kills its own descendants, then ends its group. The handler is
installed before arming the signal, and the original parent is checked again
afterward to close the startup race. The controller-setup child remains
unbound so an acknowledged native transaction may finish.

The dedicated signal matters: the pinned Ansible `TaskQueueManager` replaces
`SIGTERM` and `SIGINT` handlers; it does not replace `SIGUSR2`. A parent-death
`SIGKILL` alone killed the playbook but left session-isolated workers running.
No new namespace or capability requirement is introduced. Linux documents the
[orphan adoption of a child subreaper](https://man7.org/linux/man-pages/man2/PR_SET_CHILD_SUBREAPER.2const.html)
and the [parent-death signal and its fork/thread semantics](https://man7.org/linux/man-pages/man2/PR_SET_PDEATHSIG.2const.html).

The native test `test_real_ansible_leaves_no_worker_or_command` in
`ansible/collections/ansible_collections/bootwright/core/tests/unit/test_controller_supervisor_native.py`
starts the pinned Ansible runtime with a disposable local command, kills its
supervisor and proves the observed worker/module/command processes ended;
it also proves ordinary completion. This controller-suite test uses the
qualified interpreter that includes pinned Ansible. The module-utils file
`ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/test_controller_supervisor.py`
keeps process-tree race, orphan-startup and controller-transaction lifetime
cases and also runs under the managed-host Python floor. Unix-IPC-denied
test environments skip only the native Ansible case; that skip is not native
qualification. [Security](../../specs/security.md#process-boundary) owns the
required lifetime boundary.
