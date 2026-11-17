# Cluster access

## Client handoffs

`cluster oc` and `cluster kubectl` resolve the context and exact selected
OpenShiftCluster as [administrator access export](../cli.md#administrator-access-export)
does. An unknown name fails `access.target`; a CephCluster fails
`cluster.not-applicable`, naming the requested command and discovery remedy.
These checks precede credential metadata and installed-client inspection.

A handoff requires a completed apply of the cluster install block, a proved
kubeconfig entry in custody metadata, and the matching executable in the
controller closure that its lineage proved. A missing prerequisite refuses
with `access.unavailable` or the controller's diagnostic and remedy. No
credential is unlocked or read. Metadata readiness does not prove the bytes
readable or the remote API reachable; the later explicit kubeconfig export
validates and reveals the credential.

Success prints a bounded JSON descriptor for independent operator execution;
`--output json` and `--output yaml` place it in the normal result envelope.
Its [fields](output.md#cluster-client-handoffs) name the absolute client,
release and distribution, selected target, argument vector and explicit
kubeconfig export command. Bootwright starts no client, opens no session or
network connection, and writes no artifact or lifecycle evidence. The
operator exports the credential into a private file and runs that absolute
client with an explicit kubeconfig, private cache and working directory, and
clean environment. No ambient executable lookup, kubeconfig, credential,
proxy, plugin or configuration is part of the descriptor.

The first payload value must be a non-empty command name, with flags after
it. Payload arguments remain distinct values, including later empty strings; none is
shell text. At most 256 values, 4096 bytes each and 32 KiB total are accepted,
with valid UTF-8 and no NUL, CR or LF. Target, credential, cache and TLS
configuration overrides, `login`, `logout`, `config`, `plugin`, token reveals
and inline Secret literals refuse `access.handoff` without echoing the
rejected argument. Sensitive payloads have no supported transport: callers
must not place secrets in other arbitrary payload values, which the
descriptor prints as data. Failures emit no partial descriptor.

## Sessions

`cluster rsh` opens one session on a node of an OpenShiftCluster or managed
Ceph cluster, and
`cluster exec` opens one or, as [below](#several-clusters-and-nodes), several.
They resolve the explicit `--context`, or the current context, once;
compile that context's effective state; and resolve `--name` in the selected
graph's shared OpenShiftCluster/CephCluster name namespace, never searching
outside it. The node's `machineRef` then opens the session exactly as
[`machine rsh` and `machine exec`](../cli.md#machine-ssh-sessions) do: the identity,
credential and host-key proof come from that Machine's desired state, the
argument vector reaches the client verbatim, and the client's exit status is the
result. `cluster rsh` accepts no command tail.

An OpenShift-installed Machine with no access block resolves the cluster's
`install.nodeSSH` private key and the `core` account at its `installAddressRef`
on port 22. This access-only view is shared by sessions, trust enrollment and
inspection; it changes no authored or effective-state serialization, plan or
power operation. It requires exactly one selected cluster membership,
`os.clusterInstalled`. Authored access wins as
a whole; ambiguous membership and public-only cluster keys derive no access.
A split opaque private key reads its value part, while a keypair reads its
private part. These nodes use declared or enrolled host keys, or interactive
first-use confirmation, because the agent installation records no SSH host
key. They never receive the Machine installer's fleet identity or host-key
proof by inference.

`--node` resolves only within the cluster's declared node roster,
`OpenShiftCluster.spec.nodes` or `CephCluster.spec.topology.nodes`. Canonical node order is ascending
bytewise order of node `name`, independent of declaration or map order.
For a CephCluster, omission selects the administrator node, the `spec.cephadm.bootstrap.nodeRef`
`cluster info` reports, not the first in canonical order, which may be a MON-only
tiebreaker without the administrator keyring. An explicitly empty value is a
usage error under the scalar input rules. A supplied value selects the node whose `name` it equals exactly, and failing that the node whose effective FQDN,
including an authored override, it equals exactly. There is no further tier: a
role is not a selector for Ceph, because topology nodes carry no role. A node name wins
over a FQDN that spells it. Node selection changes no desired state, canonical
serialization or lifecycle scope.

For an OpenShiftCluster, omission selects the first node in canonical name
order. A supplied selector matches exact node name, then exact effective FQDN,
then `<role>-<ordinal>`, where role is the authored `controlPlane`, `worker` or
`infra` and ordinal is zero-based within that role's canonical node order,
spelled `0` or `[1-9][0-9]*`. A literal name wins even when it resembles a role
selector. An ambiguous name or FQDN refuses without falling through; an
unknown, malformed or out-of-range selector never selects the default.

The checks run in this order, each before the next, and every refusal exits
`255` with empty standard output, no connection attempt and no trust record:

| Condition | Diagnostic | Next action |
| --- | --- | --- |
| No selected OpenShiftCluster or CephCluster has the name, including one the cluster lists exclude | `access.target`, naming the context and the name | name a cluster the context selects; `bootwright cluster info --context <context>` lists them |
| The name is an external CephCluster, which declares no node roster | `cluster.not-applicable`, naming the command, the cluster, its kind and the applicable target, a managed CephCluster | `bootwright cluster info --context <context>` lists the clusters the context selects and their kinds |
| `--node` matches no node, or its FQDN match is shared by several nodes, or is omitted and no declared node is the administrator node | `access.target`, naming the requested selector, or the missing administrator node, and the cluster | choose one declared node name; `bootwright cluster info --context <context> --name <cluster>` lists the nodes |

The Machine behind the selected node then resolves as a `machine rsh` or
`machine exec` target does, and refuses as that section states. Target and node
selection refusals read no credential bytes, and the payload tail of
`cluster exec` never selects the node or retargets the session. Help describes
a session, not a descriptor.

### Several clusters and nodes

`cluster exec` takes a comma-separated `--name` and a comma-separated `--node`
under the [list rules](../cli.md#parsing-and-input-conventions); `cluster rsh` names one
cluster and at most one node. One cluster and one node is the session above,
unchanged. Otherwise each listed cluster runs the command on the Machine behind
its administrator node, or, with `--node`, on the Machine behind
every `--node` member it declares, each member matched in each listed cluster by
exact node `name`, then exact effective FQDN, then an OpenShift role ordinal
as above. A node name two
listed clusters declare selects a node in each.

Every resolved node's `machineRef` joins one list, deduplicated, and that list
is run as [`machine exec`](../cli.md#machine-ssh-sessions) runs it. A list of one
Machine, which two nodes or clusters sharing that Machine also yield, is the
single session above. Several run as `machine exec` runs several Machines: the
sessions are concurrent under the same bound, take no standard input and never prompt, and the
prefix of each output line is the Machine name, not the cluster or the node.
The invocation's status is the
highest status any session returned, and a refusal before a session opens exits
`255`.

Clusters resolve in canonical order, ascending bytewise name, and each check
below applies across every listed cluster before the next one runs:

| Condition | Diagnostic | Next action |
| --- | --- | --- |
| Listed names no selected OpenShiftCluster or CephCluster has | one `access.target` naming the context and every such name | as above |
| A listed name is an external CephCluster | `cluster.not-applicable` for the first such cluster | as above |
| A `--node` member matches no node of any listed cluster | one `access.target` naming every such member; with one listed cluster, worded as above | choose declared node names; `bootwright cluster info --context <context>` lists the nodes, and with one listed cluster the remedy above |
| A listed cluster declares none of the `--node` members | one `access.target` naming every such cluster and the members | name only nodes each listed cluster declares, or run the command per cluster |

A FQDN shared by several nodes of one cluster refuses as above, for the first
cluster and member in canonical order, after the cluster checks and before the
two node rows. Without `--node`, a listed cluster with no administrator node refuses
`access.target` as above. Each Machine then resolves, and refuses, as the
[`machine exec`](../cli.md#machine-ssh-sessions) section states for one or for several,
so among several Machines one whose identity is a password refuses the whole
invocation.
