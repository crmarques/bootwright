# Ansible module wrapper killed by pkill -f

**Symptom:** Cleanup tasks fail with `rc=-15`, `Module result deserialization failed`, or Ansible reports that a module died before printing JSON after a task tries to kill lingering `openshift-install` or `oc` processes.

**Root cause:** With local connection plus task-level `environment:`, Ansible puts environment assignments in the wrapper command line. A broad `pkill -f <pattern>` can match and terminate the shell running Ansible's Python module wrapper instead of only the stale child process.

**Fix:** Find candidate PIDs first, then kill those captured PIDs directly. Match the relative installer work path (`runtime/<name>/installer`, surfaced as `bootwright_current_cluster.ocp.installer.relativeWorkDir`) so cleanup still works when contexts use different absolute state paths between runs.
