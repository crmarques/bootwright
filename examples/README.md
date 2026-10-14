# Examples

This directory is the versioned home for synthetic Bootwright desired-state
examples. Every tracked example must:

- use `apiVersion: bootwright.io/v1alpha1` on each Bootwright object;
- conform to [the desired-state API](../specs/api.md);
- contain no secret bytes, real-machine identity, private endpoint, private
  topology, credential-bearing URL, or other environment-specific value; and
- put co-located YAML payloads below the exact reserved `playbooks`, `roles`,
  `collections`, `manifests`, or `secrets` roots so desired-state discovery
  never reads them.

[`multidc-platform/`](multidc-platform/) is a synthetic, non-operational
multi-datacenter graph. It demonstrates a large resource and reference surface
using fictional identities, documentation address ranges, and deliberately
fake operational coordinates. Its private Secret payload root is ignored.
It is a future authoring reference: current admission explicitly refuses its
stretch, observability, and CephFS/RGW/export features pending their qualified
slices. The test suite asserts those refusals.

Inspect the whole reference graph; this command currently reports the expected
unsupported-feature diagnostics:

```text
bootwright validate -f examples/multidc-platform
```

[`lab-rhel/`](lab-rhel/) is the single-machine lab: the development machine is
the controller, hosts the managed proxy, DNS, NTP and artifact-server
containers of the `infra-components` stage, and provides the libvirt guest
whose RHEL Bootwright installs through an emulated Redfish BMC. It uses
documentation addresses and a synthetic domain, and its README walks the
journey that prepares the controller from it.

[`lab-rhel-3-nodes/`](lab-rhel-3-nodes/) extends it to three RHEL guests using
one install profile and package tree. Its README checks concurrent services,
machine realization and installation, followed by replay and safe removal.

[`libvirt-3-nodes-ceph/`](libvirt-3-nodes-ceph/) adds dedicated OSD disks,
native core services, an RBD pool and a pool-scoped client. Its README is the
staged-apply procedure from the first context to the removal of the cluster and
its guests.

[`lab-sno/`](lab-sno/) is that same single-machine lab with one
single-node OpenShift cluster instead of an installed RHEL guest: the same
managed proxy, DNS, NTP and artifact-server set, one libvirt Machine the
substrate realizes and the agent installer supplies the operating system of,
and the cluster that installs onto it. Its README walks the journey.

[`lab-baremetal/`](lab-baremetal/) is the physical counterpart of `lab-rhel`:
one operator-owned server that RHEL is installed on through its own Redfish
management controller. Its installation publishes the installer image and the
delivered host key privately and needs a verified controller leg
([B73](../specs/milestones/m2.md#b73)); its README states the
[host prerequisites](lab-baremetal/README.md#host-prerequisites) and
[the run](lab-baremetal/README.md#run-it), proved in-tree only, and the
rehearsal against an emulated controller that is not provided yet.

The [operator guide](../docs/operator-guide.md) prepares a host for these lab
journeys and records their runs in the acceptance ledger.

## Local work in progress

`examples/wip/` is intentionally ignored. It is the place for complete
environment-specific translations used for operator review before a safe
synthetic example is extracted. Such local cases are not part of Git history
and must remain ignored because their values describe real environments.

A translation may expose required immutable input that the source never
carried, such as an image checksum or digest. Its local README must identify
that gap for operator input; it must not invent a plausible value or claim the
graph is validation-ready.

Verify the boundary before working with local examples:

```text
git check-ignore -v examples/wip/
git status --short --ignored examples/
```

A WIP graph must satisfy the field tables and cross-object rules in
`specs/api/`; the absence of a successful contract-conforming validation never
counts as success.
