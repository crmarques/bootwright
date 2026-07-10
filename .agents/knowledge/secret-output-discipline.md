# Secret bytes never reach the console: clusteraccess and the pull-secret merge

**clusteraccess summaries are paths-only:** `internal/clusteraccess` never
returns secret bytes in summaries — every summary reports only paths and
presence (`FileStatus` stats a file without reading it). `RevealSecretFile` is
the single deliberate exception that reads a captured credential file
(`kubeadmin-password`, `dashboard-password`), and callers must gate it behind
an explicit `--secrets` opt-in.

**Global pull-secret merge:** `mergedPullSecretReplacement`
(`internal/converge/workflow/apply_addon_effects.go`) preserves the live
pull-secret object's `resourceVersion` in the `oc replace` payload so a
concurrent writer surfaces as a conflict instead of being clobbered, and
reports `changed=false` when the live secret already carries exactly the
desired entry. An `auths` entry that carries the desired credential is left
untouched even when it has extra fields (`email`, `username`) — the registry
already works, so no write is triggered. The merge runs through the quiet
runner so live pull-secret bytes never hit the console; they land only in the
sanctioned `0600` task log.
