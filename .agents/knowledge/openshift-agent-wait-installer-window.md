# The agent wait gives up after 60 minutes, and the cluster does not

**Symptom:** `openshift-install agent wait-for bootstrap-complete` exits
non-zero with `Bootstrap failed to complete: : bootstrap process timed out:
context deadline exceeded`, followed by a dump of degraded ClusterOperators —
typically `authentication`, `ingress` (`0/2 of replicas are available`),
`monitoring` (`the server could not find the requested resource (post
routes.route.openshift.io)`) and `openshift-apiserver`
(`APIServicesAvailable: PreconditionNotReady`). The install log shows the
non-rendezvous control-plane hosts reaching `Done` and the rendezvous host
parked at `Waiting for bootkube`, and `kube-scheduler` reporting
`NodeInstallerProgressing: 1 node is at revision 0; 1 node is at revision 2`.

That combination is a control plane that is still converging, not one that
failed. Every operator in the dump is downstream of the rendezvous node not
having pivoted yet.

**Root cause:** the give-up is client-side and its budget is fixed.
`WaitForBootstrapComplete` (`pkg/agent/waitfor.go`) opens
`context.WithTimeout(cluster.Ctx, 60*time.Minute)` where `cluster.Ctx` is
`context.Background()` — the deadline is anchored at command start, is
compiled in, and `agent wait-for bootstrap-complete` has no flag to raise it:

```text
Flags:
  -h, --help   help for bootstrap-complete

Global Flags:
      --dir string         assets directory (default ".")
      --log-level string   log level (default "info")
```

`install-complete` is not a longer window: `newWaitForInstallCompleteCmd` calls
the *same* `WaitForBootstrapComplete` before it waits on ClusterOperators, so
both waits carry the same 60-minute bootstrap cap.

The installer stopping is not the install stopping. The rendezvous node keeps
running bootkube, and `wait-for` is a pure observer — it reads the
assisted-service REST API and the kube API and starts nothing. Re-invoking it
resumes monitoring against the live cluster.

**What eats the window:** anything that slows convergence past an hour. Release
image pulls are the usual one, and they are shared: installing two clusters in
the same apply puts both control planes on one registry path and one controller,
so each converges slower than it would alone. bootwright does not serialize
cluster installs — `RunPreparedApplyTaskGraph` dispatches every ready task up to
`ParallelismRedfish` / `ParallelismPerHost` / the global budget, and those guards
count BMCs and hosts, not clusters. Nothing is corrupted by the overlap; the
slower cluster simply misses a fixed deadline that the faster one made.

**Fix:** `wait_install.yml` treats `bootstrap process timed out` as a resumable
give-up alongside the two stalled-in-ready give-ups
(`bootwright_install_resumable_wait_pattern` unions
`bootwright_install_stalled_wait_pattern` and
`bootwright_install_timed_out_wait_pattern`), and bounds the retries by wall
clock rather than by attempt count. `Set the agent wait budget deadline` stamps
`bootwright_install_wait_deadline` once, from
`bootwright_install_wait_budget_seconds` — the bootstrap or install timeout,
selected by `bootwright_install_wait_target` — and each `until` re-evaluates
`now(utc=true).timestamp()` against it, so a give-up is re-invoked only while
budget remains.

The budget bounds when a new attempt may *start*, not when the task returns: at
the 5400s default, a wait that keeps timing out starts a second 3600s window and
so runs up to ~two hours. Raising the budget buys further windows, but the knob
differs per target — the bootstrap wait reads
`bootwright_install_bootstrap_timeout_seconds` and the install wait reads
`bootwright_install_timeout_seconds`, which the bootstrap one merely defaults
from. `bootwright_install_wait_budget_var` resolves the one that applies and is
what the hint names, so the reported remedy is never the inert knob.

**One give-up is never re-invoked.** `bootwright_install_host_error_pattern`
matches `has hosts in error` and `updated status from <stage> to error`, and
both `until` expressions exit on it. That class is deliberately outside
`bootwright_install_resumable_wait_pattern`: assisted-service has stopped
installing the cluster and diverted into recovery, so a second window watches a
state that cannot change, and the "still converging, not proof of a failed
install" wording above would be actively wrong. See
[openshift-agent-host-error-strands-rendezvous.md](openshift-agent-host-error-strands-rendezvous.md)
for why that state does not resolve on its own.

Each give-up class carries its own hint, and the classification leads the
failure message rather than trailing the command line: the run tree keeps only
a 180-rune middle-ellipsis of it (`apply_failures.go`, `status/applyrun.go`), so
whatever comes first is the only part an operator reads without opening the log.
`bootwright_install_stalled_wait_hint`
names the declared-vs-known host count; `bootwright_install_timed_out_wait_hint`
names the installer window and says the ClusterOperator dump is the state at the
last give-up, not proof of failure. Reporting the stalled hint for a timed-out
wait would send the operator hunting an unregistered node that registered
fine — the fail task selects on the narrow per-class pattern, never on the
union.

**Not a fix:** raising `async`. `bootwright_install_bootstrap_timeout_seconds`
also feeds `async:`, and the installer exits on its own deadline long before
Ansible's. An async kill reports `async task did not complete within the
requested time` with no installer stderr, which is a different failure and is
deliberately not resumable.
