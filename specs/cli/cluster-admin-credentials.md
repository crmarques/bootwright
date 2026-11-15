# Cluster administrator credentials

This page owns console credential disclosure and explicit administrator password
updates. [Cluster discovery](output.md#cluster-discovery) owns their presentation;
[CLI conventions](../cli.md#parsing-and-input-conventions) own selection.

## Console credentials

`cluster info --name <cluster> --secrets` reveals the `kubeadmin` console
username and password of one OpenShiftCluster, for OpenShift and OKD alike.
The installer password is captured in protected produced custody only after
installation completion is proved. An updated password takes precedence over
the installer's original copy. Ordinary inspection shows the username and
hidden-password guidance, with no password read. JSON and YAML place the
explicitly revealed value in `console.passwordValue`, beside `console.user`.

Absent, malformed or unproved password custody refuses `access.unavailable`
or `secret.store.corrupt`, with no credential result. Older installations that
retained only a kubeconfig can generate a password through the update command;
their original password remains in the installation work area's
`auth/kubeadmin-password`. Inspection makes no network call and describes
custodied credentials; it cannot detect an account removed or changed externally.

## Administrator credential updates

`bootwright cluster update-admin-credentials --name <cluster>` generates and
sets a new password for the existing console administrator of OpenShift/OKD or
the dashboard administrator of managed Ceph. It accepts an explicit or current
context and exactly one cluster name; external Ceph is `cluster.not-applicable`.
It requires a completed current apply, a completed original cluster block and
its original administrator access. It confirms before mutation; `--yes` skips
ordinary confirmation. It rotates neither SSH keys nor administrator certificate
kubeconfigs. A missing account is never created.

The context's exclusive mutation lease covers identity reproof, preparation,
remote change and custody publication. Secrets come from the original frozen
binding, re-proved inside that lease. The native operation proves the original
cluster and account before changing anything. A generated candidate, old-account
guard and previous active-password digest are retained in encrypted produced
custody before remote mutation. A failed or interrupted command reuses that
candidate on repetition. The active password is published only after the native
update is proved; a pending update prevents disclosure of a potentially stale
password. The command's result carries `context`, `cluster` and `user`, and
points at `cluster info --name <cluster> --secrets` for explicit disclosure.

OpenShift updates only the existing `kube-system/kubeadmin` Secret, using JSON
Patch tests on UID, resourceVersion and old hash. A deleting, replaced, changed
or bootstrap-ineligible Secret refuses; a reply lost after the update is
resolved by reading back the same UID and candidate hash. The original
certificate kubeconfig authenticates the verified API connection.

Ceph proves the frozen FSID, monitor-map incarnation, bootstrap-host ownership,
retained credential binding and existing administrator account. Its native
password policy generates the candidate. The dashboard CLI receives the new
password on private standard input, and its bcrypt account read proves the
change. A private journal precedes the remote effect and lets repetition finish
an interrupted retained-file update. The existing custody inode and custody ID
are preserved; updated complete metadata is published with conditional atomic
replacement and directory synchronization. Ordinary apply and observation keep
refusing unexplained password drift.

Credential values, hashes and guards enter no argument, environment, public
result, diagnostic, log, plan or lifecycle evidence. Operation-scoped files are
private and removed by the runner. The retained candidate is cleared from
memory on every return; the protected pending record remains attributable for
recovery and subsequent updates.

## Storage dashboard and object-user disclosure

A managed Ceph cluster's apply enables the Ceph dashboard and leaves its
administrator password in the context's
[custody](../ceph.md#secret-custody) as the produced output `dashboard` of the
cluster's block. `cluster info` reports the dashboard's access information in
the [managed Ceph entry](output.md#cluster-discovery) without any secret:
its status, user, URLs and certificate SHA-256 fingerprint.

`cluster info --name` takes a comma-separated list under the
[list rules ](../cli.md#parsing-and-input-conventions) and selects exactly the listed
clusters, in canonical order; unknown members fail `access.target` in one
diagnostic naming each of them. `--secrets` reveals one cluster's values, so it
requires `--name` to name exactly one cluster after normalization: a list of
several is `cli.usage` with exit `2`, before any evidence is read.

`cluster info --secrets --name <cluster>` additionally reveals the password as
`passwordValue` in that entry's `dashboard`, on standard output only. Its checks
run in this order, each before the next:

| Condition | Diagnostic, exit `1` | Next action |
| --- | --- | --- |
| No selected OpenShiftCluster or CephCluster has the name | `access.target`, naming the context and the name | `bootwright cluster info --context <context>` lists the clusters the context selects |
| The name is an external CephCluster | `cluster.not-applicable`, naming the cluster, its kind and the applicable target, a managed CephCluster | as above |
| The context's custody holds no dashboard password for the cluster | `access.unavailable`, naming the cluster | `bootwright apply --context <context> --stage clusters` |

The target and its applicability are settled from the compiled graph before
any custody read, so a target or applicability refusal reads no credential
bytes. A copy custody marks unproved, kept by a removal that never proved its
effect, is revealed with exactly one `access.unproved` warning naming the
cluster, and its `dashboard.status` is `unproved`. A dashboard whose
evidence alone does not prove it reachable, its copy unmarked, is revealed with
`dashboard.status` `unproved` and its reason and no warning (D183, Q101). The value occurs only in
the `passwordValue` field, and is never copied to a diagnostic, log, evidence
record, plan or second stream.

`cluster info --object-user <name> --name <cluster>` reveals the S3 keys of
that one declared CephObjectUser of the cluster, and nothing else: no
dashboard password and no other user's keys (D183, Q116). The result's
`objectUsers` holds its one entry, with `name`, `gateway`, `accessKey` and
`secretKey`; `--secrets` and ordinary inspection omit this member and read no
object-user custody. A name the cluster does not declare fails `access.target`
before any custody read. The reader checks the original produced block/output,
completed original cluster/gateway/realm/zone scope, exact canonical credential
shape and matching user ID before export. A missing, corrupt or unproved copy
refuses without any partial output, and all borrowed material is cleared on
failure and after rendering. Credentials of a different cluster or gateway, or
of another user, are never read. These fields occur only in
the explicitly sensitive standard output; no native credential retrieval runs.

For a private S3 client handoff, redirect the result into a protected local
file and transfer it over an operator-verified channel. Keep this file outside
logs and acceptance captures:

```sh
umask 077
private_access=$(mktemp)
bootwright cluster info --name ceph-lab --object-user documents --output json > "$private_access"
```

The S3 client uses the declared gateway endpoint and its verified TLS trust.
Use a protected client credential file; never put key values in arguments.
