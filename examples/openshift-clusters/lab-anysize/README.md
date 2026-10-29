# Any-size OpenShift rehearsal

A synthetic preparation profile for [B728](../../../specs/milestones/m3.md#b728)
and the [acceptance matrix](../../../specs/openshift-coverage.md#acceptance-profiles):
a large mixed-role cluster on one libvirt host. No live run or owner
acceptance is claimed, and the accepted [lab-sno](../lab-sno/) and
[lab-compact](../lab-compact/) rows are prerequisites to the consuming M3d run.

## Worth noting

- Ten guests: 3 masters (`master` profile, 8 vCPU, 16 GiB), 6 workers and 1
  infra node (`compute` profile, 4 vCPU, 8 GiB), all with 120 GiB root disks
  and an emulated TPM. API VIP 198.51.100.10, ingress VIP 198.51.100.11.
- Every node has an active-backup `bond0` over two NICs on the machine
  network (`lab-guests`, NAT) carrying the install address, plus an addressless
  third NIC on a second managed network (`lab-secondary`, `forward: none`).
- Host capacity: at least 56 logical CPUs, 112 GiB available memory and
  1240 GiB pool disk, plus media space; the authored reserve adds 4 CPUs,
  8 GiB and 40 GiB.
- The infra node uses the native worker pool during installation and, once it
  completes, carries `node-role.kubernetes.io/infra` and any `labels` and
  `taints` it authors; no taint is added for the role, so the default ingress
  keeps running on worker-labelled nodes. The infra row (B716) is an operator
  gate not yet accepted.
- Variants (5 masters with no compute, 4 masters with 2 workers) are made by
  editing a private copy: keep five or six Machines, give the first five or
  four nodes `controlPlane` with `profileRef: master` and the rest `worker`
  with `profileRef: compute`, and remove unused nodes and Machines together. No
  arbiter until B717 is defined.
- Addresses, zone, certificate SANs, upstreams and the release pin are
  synthetic. The pull secret is operator-supplied.
