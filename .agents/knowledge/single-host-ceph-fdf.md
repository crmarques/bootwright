# Single-host Ceph and external FDF rehearsal

The [single-host API](../../specs/api/ceph.md#single-host-testing) selects the
existing implementation explicitly. Ceph documents the native bootstrap flag
and OSD-level placement in its [Tentacle installation guide](https://docs.ceph.com/en/tentacle/cephadm/install/).
IBM's [Ceph architecture guide](https://www.redbooks.ibm.com/redpapers/pdfs/redp5721.pdf)
also describes the test flag; its edition covers Ceph 7, so it does not prove
qualification of a later IBM build. IBM's
[9.9.1 FAQ](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=faqs-installation-questions)
requires at least three SSD nodes for production. A single-host rehearsal is
not production support or availability testing.

IBM documents [disabling MCG independently](https://www.ibm.com/support/pages/node/7274160)
with `multiCloudGateway.reconcileStrategy: ignore`. The
[FDF package](../../specs/api/addons.md#package-fdf) exposes that choice without
requiring NooBaa readiness. External RGW still supplies OBCs; block and file use
RBD and CephFS. IBM's [external deployment parameters](https://www.ibm.com/docs/en/fusion-software/2.12.0?topic=clusters-deploying-data-foundation-external-storage-cluster)
include all three. No source establishes a supported SNO plus single-host
virtual Ceph configuration; the [case](../../e2e/lab-sno-single-ceph-fdf/runbook.md)
keeps that combination experimental.

Public pin evidence checked 2026-11-15: Red Hat's
[stable-4.22 release record](https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/stable-4.22/release.txt)
reports OpenShift 4.22.22. IBM's public catalog tag `v4.22.8-1`, digest
`sha256:2093b98971178a5859c38e50340c131420adeea3040b17d67ff17dda76e1bc4f`,
contains `stable-4.22` with CSV `odf-operator.v4.22.8`. The case freezes these
values. [AES256K client qualification](cephx-key-types.md) remains a live gate.

For the separate client NIC, [CNO's Restricted initialization](https://github.com/openshift/cluster-network-operator/blob/release-4.22/bindata/network/ovn-kubernetes/common/008-script-lib.yaml#L641-L654)
disables IPv4 forwarding; OVN re-enables only its
[managed interfaces](https://github.com/openshift/ovn-kubernetes/blob/release-4.22/go-controller/pkg/util/net_linux.go#L1055-L1073).
The case therefore applies a disjoint NNCP for `enp3s0` after NMState and
before FDF. The
[local-gateway masquerade rule](https://github.com/openshift/ovn-kubernetes/blob/release-4.22/go-controller/pkg/node/gateway_nftables.go#L740-L824)
uses the outgoing interface address, so another storage EgressIP is unnecessary.
Successful pod traffic and post-reboot forwarding still need live proof.
