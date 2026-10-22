# NVMe-oF with two gateways

## Budget and prerequisites

P4 supplies five storage nodes and a RHEL initiator template. Three nodes host MONs,
two host MGRs, all five host one SSD OSD, and the final two host one NVMe-oF
gateway each. The subsystem maps namespace 1 to the declared `rbd-disk` image
and authorizes only `nqn.2026-01.test.example:initiator`. Its two TCP listeners
use port 4420. Gateway management requires native mTLS. This example authors
public `enable_auth: false`; Bootwright derives native `enable_mtls: true`, so
the selected Ceph build must support its native certificate management. Do not
add `enable_mtls` to public input. A build with only native `enable_auth` requires
public `enable_auth: true` and all five certificate/key bindings described in
[the native contract](../../../specs/storage-native-api.md).
The worker qualifies the selected build's SDK and registered NVMe commands
before applying a gateway service. The image tag is authored example input,
not a Bootwright default or a claim that this combination has been run.

## Initial setup

Use the same controller, installation and Secret preparation as
[the base storage example](../libvirt-3-nodes-ceph/README.md). Review the
example addresses, provider bridge and artifact references before use. The
provider CPU must satisfy the IBM NVMe-oF requirement recorded in
[storage coverage](../../../specs/storage-coverage.md). The six guests request
30 vCPU and 70 GiB RAM, 340 GiB root disks and 300 GiB data disks, before the
provider reserve. The initiator's `nvme-cli` installation and I/O are operator
acceptance steps; Bootwright does not install or connect an initiator.

## IBM entitlement

Prepare the IBM entitlement and native image registry access using the base
example instructions. Keep credential values in local Secret sources; do not
record them in acceptance output. Confirm entitlement for both Ceph and the
selected gateway image.

## Create the context

```sh
bootwright validate -f examples/storage-clusters/libvirt-5-nodes-ceph-nvmeof
bootwright context init --name nvmeof-lab --input-dir examples/storage-clusters/libvirt-5-nodes-ceph-nvmeof
```

The storage context selects five guests; its root closure excludes the
unattached initiator. For the client, copy this example to a separate input
directory and replace its `environment.yaml` with
`environment-client.yaml.in`, which selects only the initiator and shared
infrastructure declarations. Use a separate `nvmeof-initiator` context and an
independently owned provider/network allocation: never let two contexts manage
the same libvirt bridge, domain, or service. Alternatively supply an existing
operator-owned RHEL initiator. The aggregate six-guest budget above includes
that separately managed client. Its package installation is operator-run.

## Apply by stage

Run `bootwright apply --context nvmeof-lab` and retain the stage results.
Confirm substrate and installation readiness before assessing the clusters
stage. An unsupported native mTLS or command surface must refuse before the
gateway service is applied; select a qualified artifact before retrying.

## Validate the cluster

Inspect the context and native Ceph health. Confirm five admitted OSDs, three
MONs, two MGRs and two gateway daemons, the declared pools and RBD image, and
the exact subsystem, namespace, listeners and host allowlist. Capture readback
without credential values.

## Client access

After apply, use the initiator to discover both gateway addresses and connect
both paths with the declared host NQN. Confirm one subsystem, the same
namespace UUID on both paths and an optimized ANA path. Write a uniquely
named test file through a filesystem created only on this test namespace,
read it back, and record a checksum. Test a different, undeclared host NQN:
connection must be denied, and the authorized mapping must remain unchanged.

## Failover

Stop one gateway through the operator's native maintenance procedure; verify
I/O and checksum through the surviving path, restore it and verify both paths.
Capture native subsystem, namespace, listener and host readback without keys.

The readiness proof checks owned image and gateway identities, exact host NQN
allowlists, verified gateway management, HA/ANA state and a TCP connection to
every declared listener. It does not claim an initiator connected or completed
I/O. Gateway management mTLS does not encrypt the NVMe/TCP data path.
DH-HMAC-CHAP and TLS-PSK remain unconfigured; LV-N10 is unrun and gates the
former. Physical CPU qualification, denied-initiator access and failover are
also unrun operator acceptance rows.

## Interruption and continuation

Interrupt one apply after the subsystem has been created, preserving the
original context. Reapply that context and confirm the same serial, namespace
UUID and owned image, with no duplicate subsystem or namespace. A foreign
subsystem must be refused rather than adopted.

## Replay

Repeat `bootwright apply --context nvmeof-lab` after readiness. Confirm native
identities, host authorization and image contents remain unchanged, and no
additional subsystem, namespace or gateway service is created.

## Check that changed intent is refused

In a copy of the input, change a namespace image reference, listener or host
NQN and validate it. On the completed context, import the copy with
`context update` and retain its completed-apply warning; the update is allowed.
Run `apply` and require the B491 changed-intent refusal before any native
effects. Restore the original input and import it again with `context update`
before continuing.

## Measured budget

No operator measurement has been run. Record these fields for acceptance; do
not infer them from the declared budgets:

| Field | Result |
| --- | --- |
| Guests | unrun; declared 5 storage + 1 separate client |
| Declared vCPU | 30 aggregate |
| Declared RAM GiB | 70 aggregate |
| Declared disk GiB | 340 root + 300 data |
| MemAvailable at (a)/(b)/(c) | unrun |
| Minimum MemAvailable during clusters stage | unrun |
| Allocated vs apparent disk bytes | unrun |
| Per-guest balloon RSS | unrun |
| Daemons by type | unrun |
| Clusters-stage wall time | unrun |
| OOM kills | unrun |

## Cleanup

Before retirement, disconnect every initiator and follow the whole-cluster
quiescence procedure in [storage](../../../specs/storage.md). Retire the owned
context as a whole with `bootwright destroy --context nvmeof-lab`. Removing a
namespace, listener, authorized host or subsystem on a surviving cluster
remains refused under B491. Preserve the original context for interrupted
creation: the same attempt resumes only its exact native identities.
