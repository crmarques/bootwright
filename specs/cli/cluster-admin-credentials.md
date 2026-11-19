# Cluster administrator credentials

This page owns console credential disclosure and explicit administrator login
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

`bootwright cluster update-admin-creds --name <cluster> [--login <list>]`
generates a new password for each administrator login of one cluster and sets
it in the running environment. The logins, in the canonical order the command
runs them, are:

| Login | Cluster | User | Held as |
| --- | --- | --- | --- |
| `console` | OpenShift or OKD | `kubeadmin` | the produced `admin-password` of the installation block |
| `dashboard` | managed Ceph | `admin` | the produced `admin-password` of the cluster block |
| `prometheus`, `alertmanager` | managed Ceph whose frozen management gateway unit lists the service in its `monitoring` | the custodied login's user | one service entry of the gateway block's produced `monitoring` |
| `grafana` | managed Ceph whose frozen monitoring unit binds the Secret at `/spec/initial_admin_password` | `admin` | that bound Secret, as a new store version the apply's binding then holds |

`--login` takes a comma-separated list of these names under the
[list rules](../cli.md#parsing-and-input-conventions): members are trimmed and
deduplicated, and a comma-only value or a member outside the five is `cli.usage`
with exit `2`, before the context is read. The default is every login the
cluster has. The list's order never changes the run order. The command accepts
an explicit or current context and exactly one cluster name. A name no selected
cluster has is `engine.credential-cluster-unknown`, and an external Ceph cluster
is `engine.credential-external-cluster`; both are checked first. A listed login
the cluster lacks is `engine.credential-login-inapplicable`, before confirmation
and before any custody read; its one diagnostic names every such login and the
logins the cluster has. `grafana` is also inapplicable when another block of the
lineage references the Secret it binds, because a new version would change that
block's input too, and when that Secret's declaration is not a token or opaque
Secret, or is a generated token whose value would exceed 128 characters (more
than 96 bytes), because no candidate could be generated; the diagnostic names
the reason, and the default leaves such a login out. A selected login whose block no completed apply proved is
`engine.credential-unapplied`. The command requires a completed current apply,
the completed original blocks and their original administrator access. It
confirms once, naming the logins, before mutation; `--yes` skips ordinary
confirmation. It rotates neither SSH keys nor administrator certificate
kubeconfigs. A missing account is never created.

One exclusive mutation lease covers the whole run: identity reproof,
preparation, remote change and custody publication of every selected login.
Logins run one after another in the canonical order, so Grafana runs last: a
Prometheus update reconfigures Grafana. Each login follows the same protocol with
its own retained candidate. Secrets come from the original frozen binding,
re-proved inside that lease. The native operation proves the original cluster and
account before changing anything. A generated candidate, old-account guard and
previous active-password digest are retained in encrypted produced custody
before remote mutation. A failed or interrupted command reuses that candidate on
repetition. The active value is published only after the native update is
proved; a pending update prevents disclosure of a potentially stale value. The
run stops at the first failing login and keeps the logins before it complete.
The failure's remedy is `bootwright cluster update-admin-creds --context
<context> --name <cluster> --login <failed>,<later...>`, naming the failed login
and every login after it; the adapter's own remedy text precedes that command as
`<remedy>, then run <command>`, unless it only says to repeat the update. A
failure before the lease, such as generating Grafana's candidate, ran no login
and keeps its own remedy. The command's result carries `context`, `cluster` and
`logins`, a list of each updated login's `login` and, for `console`, `dashboard`
and `grafana`, whose users are fixed names, its `user`; a Prometheus or
Alertmanager user is part of its secret login, so the result omits it. The
result points at `cluster info --name <cluster> --secrets` for explicit
disclosure.

OpenShift updates only the existing `kube-system/kubeadmin` Secret, using JSON
Patch tests on UID, resourceVersion and old hash. A deleting, replaced, changed
or bootstrap-ineligible Secret refuses; a reply lost after the update is
resolved by reading back the same UID and candidate hash. The original
certificate kubeconfig authenticates the verified API connection.

Ceph proves the frozen FSID, monitor-map incarnation, bootstrap-host ownership,
retained credential binding and existing administrator account. Its native
password policy generates the dashboard candidate. The dashboard CLI receives the new
password on private standard input, and its bcrypt account read proves the
change. A private journal precedes the remote effect and lets repetition finish
an interrupted retained-file update. The existing custody inode and custody ID
are preserved; updated complete metadata is published with conditional atomic
replacement and directory synchronization. Ordinary apply and observation keep
refusing unexplained password drift.

### Monitoring logins

The Prometheus and Alertmanager candidates are 32 characters of the dashboard
password alphabet that Go generates, with the active login's user. Their
pending records are the protected produced entries `prometheus-update` and
`alertmanager-update` of the gateway block, closed JSON of `guard`, `password`,
`previous` (the SHA-256 of the active value) and `user`. A pending record whose
`previous` is the active value's digest is reused. When it differs but the active
value is the candidate, that update finished and a fresh candidate is generated;
otherwise the run refuses `engine.credential-moved`.

The native update runs on the bootstrap host over the verified tunnel. It proves
the owner marker, FSID, monitor-map incarnation, the gateway unit's completed
record, the service in the unit's `monitoring` and a running gateway. It reads
the login cephadm stores. A stored login equal to the candidate is skipped; one
equal to the previous login is replaced with `ceph orch <service> set-credentials
-i -`, the candidate going in a JSON body on standard input, and read back, a
read back other than the candidate refusing `monitoring-login-unproved`. Any
other stored login refuses `monitoring-login-drift` (`ceph.identity-drifted`)
before anything is set, naming the service and never a value. Because cephadm's
stored login is what its daemons follow, nothing then holds the candidate, so
the run discards the pending record inside the lease; `cluster info --secrets`
then reveals the custodied login the remedy asks the operator to restore, and
the next run generates a fresh candidate. Cephadm reconfigures a daemon itself, because the
daemon's dependencies hash its login. The proof, polled within a 300 second
budget, requires each running daemon of the service to report a configuration
time later than the one the preparation recorded and to answer the candidate with
`200` on its route (`/prometheus/api/v1/status/buildinfo` or
`/alertmanager/api/v2/status`), and the previous login with `401`. The probe
verifies the daemon's TLS against the cephadm root CA with the host's inventory
address as its name, and presents a cephadm-signed client certificate kept only in
memory, because both services require client certificates behind the gateway.
While an oauth2-proxy daemon runs, cephadm disables basic auth, so the proof is
the reconfiguration and the `200` for the candidate, and the `401` check is
skipped: the login then guards nothing at the daemon. A daemon unproved within the
budget, no running gateway, no deployed daemon, an unreachable daemon or a
failed request refuses `monitoring-login-unproved` (`ceph.unproved`), and
repeating the command finishes the update. The dashboard needs no write: it reads
these logins from cephadm per request. The result is published by replacing that
one service's entry of the gateway block's produced `monitoring` and keeping the
other service's.

### Grafana administrator password

The candidate comes from the bound Secret's own generator when it is a `generated`
token, and is a 32-byte token otherwise; custody generates it before the lease,
and a declaration it cannot serve is inapplicable before confirmation.
The pending record is the produced `grafana-update` of the monitoring unit's
block, with the shape above. Grafana applies `initial_admin_password` only when
it creates its database, so a changed Secret changes no running Grafana by
itself, and every Grafana configuration generation sends cephadm's stored value
to the dashboard. The native update therefore changes three things: each running
Grafana daemon's live password, cephadm's stored spec leaf, and the dashboard's
Grafana API password. It proves the owner marker, FSID, monitor-map incarnation
and the unit's inputs record, and lends only the bound password and the
candidate. A stored leaf that is neither the bound version nor the candidate is
drift. When every placed daemon runs and accepts the bound password, which
proves it does not hold the candidate, and the dashboard's Grafana API password
is not the candidate, nothing holds the candidate: the refusal is
`grafana-login-drift-unapplied` (`ceph.identity-drifted`), and the run discards
the pending record as for a monitoring login. Otherwise an earlier attempt may
have placed the candidate, so the refusal is `grafana-login-drift`
(`ceph.identity-drifted`) and the record stays; its remedy reveals the bound
value with `bootwright secret show`, since disclosure refuses while it is
pending.

A Prometheus update reconfigures Grafana in a later cephadm serve pass, so
before the change and after it the update waits within its bound until no
Grafana daemon is starting or has its post-deploy configuration pending, and a
probe that finds a daemon unreachable or answering 5xx is repeated within the
bound. Per daemon, over the verified `/grafana` sub path when a management
gateway runs: a daemon that already accepts the candidate is skipped; one that
accepts the bound password changes it with the Grafana API and must then accept
the candidate, which proves the change because Grafana keeps one password per
user. A successful run so costs each daemon at most one failed login, the first
probe of the candidate. Grafana's failed-login protection locks a user out for five
minutes after five failed logins within five minutes, which a basic-auth
success does not reset, and answers a locked login with the 401 of a
wrong password, so a daemon that refuses both is `grafana-login-refused`
(`ceph.unproved`), never retried: the remedy is to wait five minutes and repeat,
and only if it still refuses both to reset that daemon's admin password to the
bound value. A login that is not `admin` is `grafana-login-drift`. When the
stored leaf is still the bound version, the unit's frozen Grafana document is
re-applied with the candidate spliced in, at most a reconfiguration. When
cephadm's export omits the leaf, the document is re-applied each time. The
dashboard's Grafana API password is set from standard input and read back,
compared in memory by HMAC, and read again after every daemon proves the
candidate, because a configuration generated before the splice sends the bound
value to it. Such a configuration can reach the dashboard before cephadm marks
its daemon, so the update then waits until no daemon's configuration time,
status or pending mark moves over a 30-second serve interval, proving every
daemon again after each move, and reads the dashboard once more. A placed
daemon that is stopped, daemons still reconfiguring or not settled for a serve
interval when the bound ends, an unreachable daemon or a refused request is
`monitoring-login-unproved`. The call has 270 seconds under a five-minute
deadline.

Publication is two scoped custody calls inside the lease, each in its own area
session, because a keyring session publishes once. The first requires the
Secret's current version to be the bound version or already the candidate and
writes the candidate as a new version under the current declaration (equal
material writes nothing, and the declaration's fingerprint must be the bound
version's, else `engine.credential-moved`); the second proves the current
version holds the candidate and rebinds with that Secret pinned at it. Until the
operation record names the new binding, the replaced one still pins the bound
version, which keeps the Secret held against `secret set` and renewal. The rebind replaces the binding of the operation that applied
the monitoring unit's block, falling back to the lineage root's, so that
operation's record names it, an operation-log `rebind` record notes it, and the
replaced binding is released. Because the binding holds the new version, the
Secret's [hold](../secrets.md#effect-material-under-a-live-binding) stays
satisfied and a later apply over unchanged input settles with nothing to bind. A
crash between the new version and the rebind, or after the rebind, is finished
by repeating the command. A crash between the rebind and the operation record
leaves an unnamed binding, which a later collection or destroy releases. A failed
operation-log append after the record update reports an error, and repeating the
command starts a new rotation.

Credential values (passwords, basic-auth logins and Grafana passwords), hashes and guards enter no argument, environment, public
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
`passwordValue` in that entry's `dashboard`, on standard output only. Behind a
management gateway it also reveals, as `user` and `passwordValue` of each
[`monitoring` row](output.md#cluster-discovery), each monitored service's own
login: Prometheus's and Alertmanager's basic-auth logins from the gateway
block's produced output `monitoring`
([management gateway unit](../ceph.md#resources-readiness-and-access)), and
Grafana's user `admin` with the version of the Secret the frozen monitoring
unit binds at `/spec/initial_admin_password` that the owner binding holds: the
version that unit applied, or the one a later
[credential update](#administrator-credential-updates) proved
([secrets](../secrets.md#effect-material-under-a-live-binding)); the current
version only when no apply owns the block. A Grafana with no such binding keeps
anonymous access, and its row says so and reveals nothing. Its checks run in this
order, each before the next:

| Condition | Diagnostic, exit `1` | Next action |
| --- | --- | --- |
| No selected OpenShiftCluster or CephCluster has the name | `access.target`, naming the context and the name | `bootwright cluster info --context <context>` lists the clusters the context selects |
| The name is an external CephCluster | `cluster.not-applicable`, naming the cluster, its kind and the applicable target, a managed CephCluster | as above |
| The context's custody holds no dashboard password for the cluster | `access.unavailable`, naming the cluster | `bootwright apply --context <context> --stage clusters` |
| Behind a management gateway, the custody holds no login for a listed Prometheus or Alertmanager | `access.unavailable`, naming the service and the cluster | `bootwright apply --context <context> --stage clusters` |
| An update of a listed login (the console, the dashboard, a monitoring login or Grafana's password) was left unfinished: its retained candidate is not the value custody or the binding holds | `engine.credential-update-pending`, naming the login | `bootwright cluster update-admin-creds --context <context> --name <cluster> --login <login>` |

The target and its applicability are settled from the compiled graph before
any custody read, so a target or applicability refusal reads no credential
bytes. A copy custody marks unproved, kept by a removal that never proved its
effect, is revealed with exactly one `access.unproved` warning naming the
cluster, and its `dashboard.status` is `unproved`; an unproved monitoring login
is revealed with one such warning naming its service and the cluster. A
refusal reveals nothing: values already read are cleared, never printed in
part. A dashboard whose
evidence alone does not prove it reachable, its copy unmarked, is revealed with
`dashboard.status` `unproved` and its reason and no warning (D183, Q101). Each value occurs only in
a `passwordValue` or monitoring `user` field, and is never copied to a diagnostic, log, evidence
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
