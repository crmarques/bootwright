# Examples

Each directory is a self-contained desired-state input: an `environment.yaml`,
the objects it declares and a concise README that explains the case and what
is worth noting about it. Validate or import that directory, rather than its
category or all of `examples/`, which contain multiple Environments. The
procedure that runs each input is its runbook, `e2e/runbooks/<scenario>.md`,
which its README names. [`e2e/`](../e2e/README.md) also holds templated
end-to-end cases that cover some of these scenarios with their own inputs.

| Type | Example | Outcome |
| --- | --- | --- |
| `machines/` | [lab-rhel](machines/lab-rhel/README.md) | Install RHEL on one libvirt guest through an emulated Redfish BMC. |
| `machines/` | [lab-rhel-external-services](machines/lab-rhel-external-services/README.md) | Install RHEL on one libvirt guest against an external proxy, DNS resolver and time source chosen through Environment kind defaults. |
| `machines/` | [lab-rhel-3-nodes](machines/lab-rhel-3-nodes/README.md) | Install three RHEL guests concurrently using a shared package tree. |
| `machines/` | [lab-baremetal-substrate](machines/lab-baremetal-substrate/README.md) | Create an installer-provisioned guest with a CA-issued emulated BMC for a separately owned physical-install rehearsal. |
| `machines/` | [lab-baremetal](machines/lab-baremetal/README.md) | Install RHEL on one physical server through its Redfish BMC. |
| `machines/` | [libvirt-network-variants](machines/libvirt-network-variants/README.md) | Install three libvirt guests over bond, VLAN and VLAN-over-bond install interfaces and converge each to its declared network. |
| `infrastructure-services/` | [lab-services](infrastructure-services/lab-services/README.md) | Run managed proxy, DNS, NTP and artifact services on the controller. |
| `ceph-clusters/` | [physical-3-nodes-ceph](ceph-clusters/physical-3-nodes-ceph/README.md) | Managed physical hosts, explicit WWN disk binding and opt-in verified logical overwrite before claim release. |
| `ceph-clusters/` | [libvirt-5-nodes-ceph-nvmeof](ceph-clusters/libvirt-5-nodes-ceph-nvmeof/README.md) | Five storage nodes, two NVMe-oF gateways, an owned RBD namespace and explicit host NQN authorization. |
| `ceph-clusters/` | [libvirt-2x4-nodes-ceph-multisite](ceph-clusters/libvirt-2x4-nodes-ceph-multisite/README.md) | Two managed four-host clusters with one-way RBD mirroring and local MDS/RGW. |
| `ceph-clusters/` | [libvirt-6-nodes-ceph-pools](ceph-clusters/libvirt-6-nodes-ceph-pools/README.md) | P1: six hosts, twelve encrypted SSD/HDD OSDs with SSD DB and a TPM2 variant, replicated and EC pools, owned RBD images and an acceptance runbook. |
| `ceph-clusters/` | [libvirt-3-nodes-ceph](ceph-clusters/libvirt-3-nodes-ceph/README.md) | Install three RHEL guests and a core IBM Ceph cluster with an RBD pool and client. |
| `ceph-clusters/` | [libvirt-3-nodes-ceph-block-file](ceph-clusters/libvirt-3-nodes-ceph-block-file/README.md) | Install three RHEL guests and an IBM Ceph cluster with a placement policy, an RBD image, a CephFS filesystem with an active and a standby MDS, a subvolume group and a subvolume, with block and file data and MDS failover checks. |
| `ceph-clusters/` | [libvirt-3-nodes-ceph-object](ceph-clusters/libvirt-3-nodes-ceph-object/README.md) | Install three RHEL guests and an IBM Ceph cluster with an object gateway: its realm, zone group, zone, zone pools and two RGW daemons that serve its TLS endpoint, with certificate-name and daemon failover checks. |
| `ceph-clusters/` | [libvirt-3-nodes-ceph-nfs](ceph-clusters/libvirt-3-nodes-ceph-nfs/README.md) | Install three RHEL guests and an IBM Ceph cluster with a CephFS subvolume served by one NFSv4 export of a direct NFS service, with allowed and denied client checks. |
| `ceph-clusters/` | [libvirt-4-nodes-ceph-observability](ceph-clusters/libvirt-4-nodes-ceph-observability/README.md) | P3: four guests with monitoring, management gateway, OAuth2, SNMPv3 and log collection. |
| `ceph-clusters/` | [libvirt-3-nodes-ceph-host-variants](ceph-clusters/libvirt-3-nodes-ceph-host-variants/README.md) | P5: bonded VLAN networking, managed RHSM, FIPS, AES CephX and TPM2-encrypted root with recovery. |
| `ceph-clusters/` | [libvirt-3-nodes-ceph-disconnected](ceph-clusters/libvirt-3-nodes-ceph-disconnected/README.md) | P6: consume an operator-prepared mirror for every package and image, with monitoring, ingress, isolated networking and an acceptance runbook. |
| `ceph-clusters/` | [libvirt-6-nodes-ceph-services](ceph-clusters/libvirt-6-nodes-ceph-services/README.md) | P2: six storage guests and an independent client allocation, CephFS layouts, TLS RGW ingress, multi-active NFS and clustered SMB. |
| `openshift-clusters/` | [lab-sno](openshift-clusters/lab-sno/README.md) | Install single-node OpenShift on a libvirt guest. |
| `openshift-clusters/` | [lab-compact](openshift-clusters/lab-compact/README.md) | Install compact OpenShift on three libvirt masters with installer-owned virtual IPs. |
| `openshift-clusters/` | [lab-anysize](openshift-clusters/lab-anysize/README.md) | Prepare the bonded ten-node OpenShift acceptance topology. |
| `openshift-clusters/` | [lab-features](openshift-clusters/lab-features/README.md) | Prepare TPM2 encryption, proxy and external mirror acceptance. |
| `openshift-clusters/` | [lab-features-ipv6](openshift-clusters/lab-features-ipv6/README.md) | Prepare the IPv6-only installation-feature variant. |
| `openshift-clusters/` | [lab-networks](openshift-clusters/lab-networks/README.md) | Prepare localnet secondary networking and two MetalLB pool paths. |
| `openshift-clusters/` | [lab-kubevirt](openshift-clusters/lab-kubevirt/README.md) | Install three OpenShift control-plane nodes and two workers on an existing virtualization host with installer VIPs and an image pushed to each guest (B731's lab-kubevirt profile). |
| `openshift-clusters/` | [lab-kubevirt-external](openshift-clusters/lab-kubevirt-external/README.md) | The same on three control-plane nodes behind external endpoints on the derived `none` platform (the coverage variant lab-kubevirt-external). |
| `openshift-clusters/` | [lab-kubevirt-nested](openshift-clusters/lab-kubevirt-nested/README.md) | Install a three-node OpenShift cluster with OpenShift Virtualization on libvirt, then a three-node cluster on its virtual machines (B732's lab-kubevirt-nested profile). |
| `openshift-clusters/` | [lab-physical](openshift-clusters/lab-physical/README.md) | Prepare five physical OpenShift targets with verified private media delivery. |
| `add-ons/` | [lab-sno-nmstate](add-ons/lab-sno-nmstate/README.md) | Install the Kubernetes NMState Operator on single-node OpenShift. |
| `add-ons/` | [lab-sno-metallb](add-ons/lab-sno-metallb/README.md) | Install MetalLB and an eight-address layer 2 pool on single-node OpenShift. |
| `add-ons/` | [lab-compact-hub](add-ons/lab-compact-hub/README.md) | Install pinned ACM and OpenShift GitOps with a bootstrap Application on a compact cluster. |
| `add-ons/` | [lab-compact-df](add-ons/lab-compact-df/README.md) | Install Data Foundation on compact OpenShift over a managed three-node Ceph cluster with TLS object storage. |
| `full-platforms/` | [multidc-platform](full-platforms/multidc-platform/README.md) | Authoring reference for a multi-datacenter platform; current admission refuses its unsupported features. |

A row that starts "Install" describes a scenario written to realize its whole
outcome; "Prepare" describes a synthetic preparation profile that claims no live
run or owner acceptance. The row states the outcome only. Each README states its
qualification limits, and [milestones](../specs/milestones.md) and the
[acceptance ledger](../docs/acceptance.md) own delivery and acceptance status.
The [Ceph profile catalog](../specs/ceph-coverage.md#profile-catalog) defines
the `ceph-clusters/` profiles; their runbooks, with the sections that page
requires, are under `e2e/runbooks/`.

Machine and cluster examples include the infrastructure they need. There is no
dependency on running another example first. Their shared service addresses and
ports mean these lab scenarios should run one at a time on a host unless you
assign distinct resources. Read the README's qualification limits and the
[host preparation](../e2e/README.md#prepare-the-host) before applying one.
The [operator guide](../docs/operator-guide.md) owns controller setup and host
access.

## Prepare an operator copy

Tracked inputs use documentation addresses and synthetic identities. Copy a
scenario outside the repository, customize it, and supply licensed media and
secrets as its README and its runbook direct. Placeholder
checksums and registry digests are not real content identities: replace them
with values verified for your media and release. An IBM Storage Ceph Entitlement carries `license.accept: true` as a
placeholder; reviewing the IBM license terms and accepting them is your explicit
act. `validate` proves admission, not connectivity or readiness.

Context import freezes a copy of the input; edits to the source directory do
not update the context. To replace applied intent, complete destroy while
keeping the context, then use
`context update --name <context> --input-dir <directory>` and review `plan`
before a fresh apply. For input already imported, `diff` compares it with the
context's recorded realization. Review the full `plan` after `context init` and
before the first `apply --stage`: while a staged operation is paused,
`context update` refuses until it completes or is destroyed.
An unfinished apply continues with the same stage selection after the cause is
resolved. Follow the refusal's remedy if frozen intent or execution inputs
changed; do not delete context evidence to force a retry. Each runbook gives
its removal sequence.

## Contributing examples

Every tracked example must use `apiVersion: bootwright.io/v1alpha1`, conform to
the [API](../specs/api.md), and contain no secret bytes, real-machine identity,
private endpoint, topology, or credential-bearing URL. Each scenario has an
`environment.yaml` and a concise README: what the input declares and its
outcome, a short list of what is worth noting (topology, sizes, notable fields,
qualification limits and required operator inputs such as media, Secrets and
placeholder checksums), and one line naming the runbook that
runs it. Procedures do not belong in an example README: they go in the
scenario's runbook, `e2e/runbooks/<scenario>.md`, or in a templated case of
[`e2e/`](../e2e/README.md). Follow the repository's
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
