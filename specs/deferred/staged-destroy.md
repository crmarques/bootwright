# Staged destroy

This page holds the design of `bootwright destroy --stage` and of the apply
that restores what it takes back. It authorizes nothing: only an item in an
active slice does, and the owner's decision
[D288](../milestones/backlog.md#d288) names the program. Each slice promotes
its part into the owning spec in its own commit and deletes that part from this
page. A rule that the code already holds is not stated here: slice 0, the
frozen inverse of every destroy step, slice 1, its branches once decided at
run time, slice 2, the ownership fold, slice 3, `destroy --stage` and the
trimmed resting state, slice 4, the restoring apply, slice 5, its changed
input, slice 6, an agent installation taken back on kept nodes before a
boot, slice 7, the root disks of kept libvirt nodes blanked after one,
slice 8, a Ceph cluster no data client was handed access to taken back on kept
hosts, and slice 9, a physical Machine's installation and erase taken back
with its release, are implemented and are owned by
[state reconciliation](../state-reconciliation.md#staged-destroy),
its [restoring apply](../state-reconciliation.md#restoring-apply),
[removal on kept nodes](../openshift-clusters.md#removal-on-kept-nodes),
[a never-used cluster on kept hosts](../ceph.md#a-never-used-cluster-on-kept-hosts) and the
delivered records
[X757](../milestones/delivered.md#x757--the-restoring-apply-restores-what-a-staged-destroy-took-back),
[X759](../milestones/delivered.md#x759--a-restore-admits-changed-input-inside-what-a-staged-destroy-took-back),
[X760](../milestones/delivered.md#x760--an-agent-installation-no-node-booted-is-taken-back-on-kept-nodes),
[X762](../milestones/delivered.md#x762--a-staged-destroy-blanks-the-root-disks-of-kept-libvirt-nodes),
[X763](../milestones/delivered.md#x763--a-never-used-ceph-cluster-is-taken-back-on-kept-hosts)
and item [B1864](../milestones/m1.md#b1864).

Words used below:

- A **stage** is one of `substrates`, `machines`, `clusters` and `add-ons`, in
  the order the engine realizes them.
- A **staged destroy** takes back one stage and everything built on it. The
  context then **rests trimmed**.
- A **kept** block is a block of the context that a staged destroy leaves in
  place. A **kept Machine** is a Machine whose own step is not a deletion.
- **Live** is the set of blocks a context owns.
- A **restoring apply** is the next `apply` over a trimmed context.
- The **step contract** is the pairing of every apply step with one destroy
  step that states its inverse.

## Why

An apply that fails late, such as in cluster provisioning, leaves the operator
only the whole destroy. The whole destroy deletes the Machines that were
already installed and proved, and the operator pays for the whole build again.
The owner asked for a way to take back only the latest stage and keep the
Machines below it, and asked that every apply and destroy flow be backed by a
state machine whose steps each have an equivalent destroy step, so that a later
reconciliation of one changed attribute can build on the same contract.

## Program

The order below is the order of delivery. Each row is one item with its own
slice, and each slice names its decision and the contract and digest moves it
causes.

| Order | Item | Outcome |
| --- | --- | --- |
| 0 | [B1855](../milestones/delivered.md#x753--every-destroy-step-states-its-inverse) | Every destroy step states its inverse, frozen with the removal plan |
| 1 | [B1856](../milestones/delivered.md#x754--run-time-inverse-branches-frozen) | The remaining inverse branches decided at run time are frozen |
| 2 | [B1857](../milestones/delivered.md#x755--one-fold-answers-what-a-contexts-operations-own) | One ownership fold answers what a context owns |
| 3 | [B166](../milestones/delivered.md#x756--a-staged-destroy-takes-back-a-stage-and-the-context-rests-trimmed) | `destroy --stage` and the trimmed resting state |
| 3b | [B1867](../milestones/delivered.md#x758--a-destroy-widens-an-unfinished-staged-destroy) | A `destroy` without `--stage`, or a lower floor, supersedes an unfinished staged destroy |
| 4 | [B1858](../milestones/delivered.md#x757--the-restoring-apply-restores-what-a-staged-destroy-took-back) | The restoring apply over unchanged input |
| 5 | [B1859](../milestones/delivered.md#x759--a-restore-admits-changed-input-inside-what-a-staged-destroy-took-back) | A restore admits changed input inside the taken-back part |
| 5b | [B1868](../milestones/delivered.md#x761--a-restore-binds-what-its-added-steps-name-and-a-removal-presents-each-step-its-creators-binding) | A restore binds its added steps' Secrets, and a whole removal reopens each member's binding |
| 6 | [B1860](../milestones/delivered.md#x760--an-agent-installation-no-node-booted-is-taken-back-on-kept-nodes) | An agent installation whose nodes never booted is taken back on kept nodes |
| 7 | [B1861](../milestones/delivered.md#x762--a-staged-destroy-blanks-the-root-disks-of-kept-libvirt-nodes) | Root disks of kept libvirt nodes are blanked through their substrate |
| 7b | [B1880](../milestones/m1.md#b1880) | Root disks of kept KubeVirt nodes are blanked through their substrate |
| 8 | [B1862](../milestones/delivered.md#x763--a-never-used-ceph-cluster-is-taken-back-on-kept-hosts) | A Ceph cluster no data client was handed access to is taken back on kept hosts |
| 9 | [B1864](../milestones/m1.md#b1864) | A physical Machine's installation and erase are taken back, and the restore discovers its disks again |

`--clusters` narrowing of a staged destroy is delivered
([B1865](../milestones/delivered.md#x764--a-staged-destroy-narrows-to-the-named-clusters)).
Online removal of one add-on instance is [B769](../milestones/backlog.md#b769).

## The trimmed resting state

What lands with later items:

- After every registration the log area still fits one whole removal of what
  the context owns. A staged destroy and its restore cost two operation
  directories, and nothing is pruned. At the bound the command refuses and
  names the whole destroy.

## The restoring apply

The restore, changed input included, its own binding and the removal's
per-step binding are owned by
[state reconciliation](../state-reconciliation.md#restoring-apply).

## CLI

- `--dry-run` shows the steps taken back under a header per stage.
- The ask rules that cover `destroy` cover `destroy --stage`.

## Scenarios

| Scenario | Result |
| --- | --- |
| Cluster provisioning failed and the nodes are KubeVirt VMs that booted | `--stage clusters` refuses `destroy.stage-keeps-host` until the KubeVirt root-disk reset exists ([B1880](../milestones/m1.md#b1880)) |
| A cluster install failed before any node booted the image | `--stage clusters` takes back the agent installation on kept nodes ([removal on kept nodes](../openshift-clusters.md#removal-on-kept-nodes)) |
| A staged destroy fails halfway and the operator widens it | A lower floor or no `--stage` supersedes it behind the widening confirmation ([staged destroy](../state-reconciliation.md#staged-destroy)) |
| `--clusters` narrows a staged destroy | Takes back the named clusters' steps and keeps the others ([staged destroy](../state-reconciliation.md#staged-destroy)) |
| A Ceph cluster, on physical hosts or with an SMB share, consumer, replication or sign-on, none of whose access steps completed | `--stage clusters` takes it back while its hosts run ([a never-used cluster on kept hosts](../ceph.md#a-never-used-cluster-on-kept-hosts)); a lower floor that deletes or releases some of its hosts refuses ([B1895](../milestones/m2.md#b1895)) |
| A Ceph cluster that handed out access is in the scope | Refuses `destroy.stage-keeps-host` until B491's serving-cluster contract ([B491](../milestones/m2.md#b491)) |

## Capabilities

Each row names what a staged destroy does with the capability and the item that
unlocks the part that refuses.

| Capability | In a staged scope | Unlocked by |
| --- | --- | --- |
| libvirt and KubeVirt Machines | Deleted by the scope, or kept with their steps below the floor | [B166](../milestones/delivered.md#x756--a-staged-destroy-takes-back-a-stage-and-the-context-rests-trimmed) |
| Infrastructure services (mirror content, DNS, registry, hardware claims, enrollment) and the controller | Always kept | none; the whole destroy takes them back |
| OpenShift agent installation | Taken back on kept nodes when no node was handed the image | [B1860](../milestones/delivered.md#x760--an-agent-installation-no-node-booted-is-taken-back-on-kept-nodes) |
| OpenShift physical nodes after a boot | Stays open: the installed system would be recorded as residue, the nodes proved off and the next install would consume data loss as it does now | the owner's decision |
| KubeVirt node Machines after a boot | The install step's inverse blanks the root disks through the KubeVirt arm's root-disk reset | [B1880](../milestones/m1.md#b1880) |
| Ceph on kept hosts | Taken back on virtual, provided and physical hosts while no step that hands out access completed, SMB, consumers, replication and sign-on included ([a never-used cluster on kept hosts](../ceph.md#a-never-used-cluster-on-kept-hosts)); a scope that deletes or releases only some of a cluster's hosts refuses; serving clusters wait for B491's client-fencing contract | [B1895](../milestones/m2.md#b1895), [B491](../milestones/m2.md#b491) |
| Add-on instances | Retire offline with their cluster's install in the scope | [B769](../milestones/backlog.md#b769) for one instance online |

## Open for the owner

- Physical OpenShift nodes after a boot.
