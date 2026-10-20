# Examples

Choose the outcome you want, then follow that scenario's README from the
repository root. Each directory is a self-contained desired-state input;
validate or import that directory, rather than its category or all of
`examples/`, which contain multiple Environments.

| Type | Example | Outcome |
| --- | --- | --- |
| `machines/` | [lab-rhel](machines/lab-rhel/README.md) | Install RHEL on one libvirt guest through an emulated Redfish BMC. |
| `machines/` | [lab-rhel-3-nodes](machines/lab-rhel-3-nodes/README.md) | Install three RHEL guests concurrently using a shared package tree. |
| `machines/` | [lab-baremetal](machines/lab-baremetal/README.md) | Install RHEL on one physical server through its Redfish BMC. |
| `machines/` | [libvirt-network-variants](machines/libvirt-network-variants/README.md) | Install three libvirt guests over bond, VLAN and VLAN-over-bond install interfaces and converge each to its declared network. |
| `infra-components/` | [lab-services](infra-components/lab-services/README.md) | Run managed proxy, DNS, NTP and artifact services on the controller. |
| `storage-clusters/` | [libvirt-3-nodes-ceph](storage-clusters/libvirt-3-nodes-ceph/README.md) | Install three RHEL guests and a core IBM Ceph cluster with an RBD pool and client. |
| `storage-clusters/` | [libvirt-3-nodes-ceph-block-file](storage-clusters/libvirt-3-nodes-ceph-block-file/README.md) | Install three RHEL guests and an IBM Ceph cluster with a placement policy, an RBD image, a CephFS filesystem with an active and a standby MDS, a subvolume group and a subvolume, with block and file data and MDS failover checks. |
| `storage-clusters/` | [libvirt-3-nodes-ceph-object](storage-clusters/libvirt-3-nodes-ceph-object/README.md) | Install three RHEL guests and an IBM Ceph cluster with an object gateway: its realm, zone group, zone, zone pools and two RGW daemons that serve its TLS endpoint, with certificate-name and daemon failover checks. |
| `storage-clusters/` | [libvirt-3-nodes-ceph-nfs](storage-clusters/libvirt-3-nodes-ceph-nfs/README.md) | Install three RHEL guests and an IBM Ceph cluster with a CephFS subvolume served by one NFSv4 export of a direct NFS service, with allowed and denied client checks. |
| `container-clusters/` | [lab-sno](container-clusters/lab-sno/README.md) | Install single-node OpenShift on a libvirt guest. |
| `container-clusters/` | [lab-compact](container-clusters/lab-compact/README.md) | Install compact OpenShift on three libvirt masters with installer-owned virtual IPs. |
| `full-platforms/` | [multidc-platform](full-platforms/multidc-platform/README.md) | Authoring reference for a multi-datacenter platform; current admission refuses its unsupported features. |

The [Ceph profile catalog](../specs/storage-coverage.md#profile-catalog) splits
[B484](../specs/milestones/m2.md#b484)'s full virtual matrix into separate
`storage-clusters/` profiles, each with the runbook sections that page requires:
`libvirt-6-nodes-ceph-pools/`, `libvirt-6-nodes-ceph-services/`,
`libvirt-4-nodes-ceph-observability/`, `libvirt-5-nodes-ceph-nvmeof/`,
`libvirt-3-nodes-ceph-host-variants/`, `libvirt-3-nodes-ceph-disconnected/` and
`libvirt-2x4-nodes-ceph-multisite/`. Each name becomes a table row when its
profile lands.

Machine and cluster examples include the infrastructure they need. There is no
dependency on running another example first. Their shared service addresses and
ports mean these lab scenarios should run one at a time on a host unless you
assign distinct resources. Read each runbook's prerequisites and qualification
limits before applying it. The [operator guide](../docs/operator-guide.md)
owns controller setup and host access; [milestones](../specs/milestones.md)
owns delivery and operator acceptance status.

## Prepare an operator copy

Tracked inputs use documentation addresses and synthetic identities. Copy a
scenario outside the repository, customize it, and supply licensed media and
secrets as its runbook directs. Placeholder checksums and registry digests are
not real content identities: replace them with values verified for your media
and release. `validate` proves admission, not connectivity or readiness.

Context import freezes a copy of the input; edits to the source directory do
not update the context. To replace applied intent, complete destroy while
keeping the context, then use
`context update --name <context> --input-dir <directory>` and review `plan`
before a fresh apply. For input already imported, `diff` compares it with the
context's recorded realization.
An unfinished apply continues with the same stage selection after the cause is
resolved. Follow the refusal's remedy if frozen intent or execution inputs
changed; do not delete context evidence to force a retry. Each scenario gives
its removal sequence.

## Contributing examples

Every tracked example must use `apiVersion: bootwright.io/v1alpha1`, conform to
the [API](../specs/api.md), and contain no secret bytes, real-machine identity,
private endpoint, topology, or credential-bearing URL. Each scenario has an
`environment.yaml` and concise README covering customization, execution,
inspection, recovery and removal. Follow the repository's
[YAML style](../.agents/skills/code-implementation/references/formatting.md#yaml),
including one empty line between logical blocks. Co-located non-object YAML
belongs under the
exact reserved `playbooks`, `roles`, `collections`, `manifests`, or `secrets`
roots so desired-state discovery skips it.

`examples/wip/` is an ignored local work area for operator translations. Verify
the boundary before putting real inputs there:

```sh
git check-ignore -v examples/wip/
git status --short --ignored examples/
```

Local translations must identify missing checksums, digests or other required
operator inputs. Never invent plausible values or claim validation succeeded
without running it. The test suite compiles every tracked scenario and pins
its effective state; the full-platform reference instead has explicit
unsupported-feature assertions.
