# Cluster sign-on suspension

This page owns `bootwright cluster sign-on suspend` and `resume`, the exit that
returns a managed Ceph cluster's management gateway to local sign-in while the
identity provider its [CephSignOn](../ceph-resources.md#cephsignon) names is
down, and restores the frozen sign-on later. The sign-on block itself is the
[management sign-on](../ceph.md#management-sign-on); the
[command catalog](commands.md#command-and-flag-catalog) owns the flags and
[output](output.md#cluster-discovery) the presentation.

## Selection and refusals

Both commands accept an explicit or current context and exactly one cluster
name, `--name`, and `--yes`, which skips the one ordinary confirmation. Before
the confirmation, any custody read and any lease, in this order:

| Condition | Diagnostic |
| --- | --- |
| No selected cluster has the name | `engine.sign-on-cluster-unknown` |
| The name is an external CephCluster | `engine.sign-on-external-cluster` |
| The context holds no current operation, or it is not a `done` apply, such as an additive apply left unfinished after a suspension | `engine.sign-on-unapplied` |
| No apply of that lineage froze a sign-on block for the cluster | `engine.sign-on-absent` |
| The lineage's sign-on block of the cluster is not `done` | `engine.sign-on-unapplied` |
| The suspension record is unreadable, names another block or another request digest | `engine.sign-on-suspension-damaged` |

The command reopens the Secret binding of the apply that holds the sign-on
block, the one an [additive apply](../state-reconciliation.md#additive-apply)
registered when the sign-on was added, else the lineage root's, then confirms,
then takes one exclusive mutation lease of the context. Inside the lease it
reads the same basis again: a changed current operation, input revision,
sign-on block, request digest, binding or suspension record refuses
`engine.sign-on-moved`, and nothing runs. The lease covers every record write
and every remote call below.

## Suspension record

The record lives in the operation store beside the records of the apply that
holds the block, `operations/<operation-id>/suspensions/<block-id>.json`:
closed canonical JSON of at most 16 KiB, `version` `sign-on-suspension-v1`,
`block`, `request` (the block's request digest), `state` (`suspending`,
`suspended` or `resuming`) and `preparation`, the block's read-only before
state, at most 12 KiB. It holds no secret. Every write replaces exactly the
bytes this invocation read, or creates the record where it read none, and a
clear removes exactly those bytes. A destroy leaves the record with the
operation it belongs to; no later lineage reads it.

## Suspend

1. With no record, or one in `resuming`, the bootstrap host reads, read-only,
   each `mgmt-gateway`, `grafana`, `prometheus` and `alertmanager` daemon
   cephadm lists with its `last_configured` time. The command writes the
   record `suspending` with that baseline before any remote effect. A record
   in `suspending` or `suspended` keeps its baseline.
2. Over the verified administrative channel on the bootstrap host, the worker
   runs `ceph orch rm oauth2-proxy` only when cephadm stores the oauth2-proxy
   service and that stored document is the one this block's own inputs and
   attempt records prove it applied; another refuses `sign-on-foreign`
   (`ceph.foreign`), and an absent service writes nothing. Because suspend
   lends no sign-on Secret, the comparison covers every member but the stored
   secret values, which need only be present. Just before the removal it reads
   each dependent daemon's configuration time again, and the wait below uses
   the later of that and the recorded baseline, so a proxy that came back after
   the recording is waited out too.
3. The worker then waits, within 900 s, until cephadm stores and lists no
   oauth2-proxy service or daemon, `mgr/dashboard/sso_oauth2` reads false,
   every dependent daemon the baseline names and cephadm still lists was
   configured after its baseline time, and local sign-in is proved. cephadm's
   oauth2-proxy removal disables the dashboard's single sign-on when its last
   daemon goes; while the option still reads true with no proxy listed, the
   worker runs `ceph dashboard sso disable` once and reads it back. A daemon
   created after the baseline needs no reconfiguration. A wait that ends
   refuses `sign-on-suspend-unproved` (`ceph.not-ready`), naming what it
   waited for.
4. Local sign-in is proved on the bootstrap host: the custodied dashboard
   `admin` password is checked against Ceph's bcrypt hash there, with the
   administrator role, enabled and not due for change, as the
   [credential update](cluster-admin-credentials.md#administrator-credential-updates)
   proves it, so the password never leaves the host; a mismatch refuses
   `dashboard-drift`. Then an unauthenticated `GET /` of the gateway's virtual
   IP, else each running gateway daemon, over TLS pinned to the certificate
   the gateway's apply proved, must answer the gateway's own sign-in: a 200,
   or a redirect to an absolute path, or to an `http` or `https` URL on a host
   other than the identity provider's, that is not under `/oauth2/`.
5. The command writes the record `suspended`.

Suspend never contacts the identity provider and lends no sign-on Secret: the
run holds only the hosts' SSH key and known hosts. A repeat over `suspended`
runs steps 2 to 4 again, writes nothing native and reports `changed: false`.
A crash before step 1's write left nothing; after it, the repeat finishes with
the recorded baseline. Prometheus and Alertmanager basic auth return with
cephadm's reconfiguration, with the logins the gateway's apply custodied, so
`cluster info --secrets` reveals every local login. Deadlines: the preparation
1380 s, the suspension 3600 s.

## Resume

With a record not already `resuming`, the command writes it `resuming` first.
It then runs the sign-on block's own [apply protocol](../ceph.md#management-sign-on)
against the frozen request: the sign-on and its cluster re-proved, each
oauth2-proxy host's discovery of the issuer before any effect, refusing
`sign-on-provider-unreachable` while the provider does not answer, the frozen
oauth2-proxy document re-applied with the leaves the owning apply's binding
lends, the rendered-configuration and gateway route proofs, `dashboard sso
enable oauth2` and readiness with the enforcement proof. Its stream admits the
completed document the suspension removed. On success the record is cleared.
A crash between the remote run and the clear is finished by the repeat. A
resume with no record runs the same protocol, which proves and repairs an
active sign-on. A resume the provider pre-check refuses changed nothing, so it
puts back the record it read, and the cluster stays `suspended`. Its deadline is
the sign-on apply's, 4920 s. The block's apply
record is never rewritten.

## While suspended

A `diff --observe` of the suspended block reads, read-only on the bootstrap
host, that no oauth2-proxy service or daemon is back and the dashboard's single
sign-on is off: the block is `present`, never drift or a failure, and carries
the note `<block>: suspended by bootwright cluster sign-on suspend; local
sign-in is on until bootwright cluster sign-on resume ...`. One that came back
refuses `sign-on-suspension-drift` (`ceph.identity-drifted`), whose remedy is
to suspend again or resume. A record in `suspending` or `resuming` reads the
block `unknown` without any host call, its remedy the interrupted command.
`cluster info` shows the cluster's `signOn.state` (`active`, `suspending`,
`suspended` or `resuming`) and, while suspended, points at `cluster info
--secrets` for the local logins.

`apply` over the completed lineage settles without running the sign-on block,
because it runs only in its own apply, and a continuation of that apply cannot
exist while suspend requires it `done`; neither turns the sign-on back on. An
additive apply runs only its added blocks. `destroy` retires the block with its
cluster's hosts, suspended or not.

## Result

The result carries `context`, `cluster` and `signOn`: `state` (`suspended` or
`active`), `provider` (the OIDCProvider's name), `changed` and `localLogins`,
the logins a suspension returned to local sign-in in the
[credential update order](cluster-admin-credentials.md#administrator-credential-updates)
(`dashboard`, then `prometheus` and `alertmanager` when the gateway fronts
them, then `grafana` when it fronts Grafana), empty after a resume. Text output
names the next command, `cluster info --secrets` after a suspend and the
resume command to restore. No password, token, response body or Secret enters
an argument, result, diagnostic, log, record or evidence.
