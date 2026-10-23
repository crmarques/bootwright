# Three physical Ceph hosts

This synthetic input combines managed bare-metal RHEL installation with three
Ceph hosts, one explicitly selected WWN disk per host, replicated pools, CephFS
and RBD. The OSD memory target is 4 GiB per OSD. It is an authoring and planning
example; live BMC, image, hardware and storage acceptance remain unrun.

Start with the [physical installation prerequisites](../../machines/lab-baremetal/README.md).
Copy this directory outside Git before supplying real BMC endpoints, NIC
addresses, root-disk names, WWNs, media and credentials. All shipped identities
are documentation placeholders. Each OSD WWN must resolve to an empty local
whole disk distinct from the installation's root disk and every boot device.
The [cluster](storage/cluster.yaml) uses a separate OSD service for each host
because physical WWNs are unique; a shared selector cannot identify three disks.

The existing native `data_devices.paths`, `db_devices.paths` and
`wal_devices.paths` fields accept canonical WWN by-id paths. This initial
bounded physical path supports at most three selected disks per host. It rejects
partitions, shared WWNs, multipath targets and missing disk serials. The original
Machine proof must contain the system UUID and complete MAC inventory, which
must match the installed host's kernel inventory. A bond's physical slaves use
the kernel's saved original addresses only with complete reciprocal bond
membership proof; unknown or changed hardware still refuses. Installed SSH
host-key verification remains required. The implementation admits three to six
storage hosts; larger physical profiles require separate budget qualification.

Device preparation retains the observed WWN, serial, capacity, rotational
status, kernel path and native Ceph device ID before any OSD effect. Read-only
host observation re-proves those facts. Existing foreign signatures, mounted
filesystems and nonempty DB/WAL disks refuse; this input authorizes no disk
zap or replacement. A host or disk substitution cannot be accepted by retrying.

The Machine files explicitly opt in to verified logical overwrite on removal.
Their root and OSD device names, WWNs, serial numbers and exact byte capacities
are synthetic placeholders. Replace every target from independently verified
inventory before planning a copied input. Omitting `eraseOnRemoval` retains the
installed system and makes realized physical Ceph retirement refuse. Add every selected
DB/WAL disk to its Machine's target list when extending this example.
Choose the erase contract before the original apply: that operation freezes its
policy. Editing the inputs later cannot add targets or authorize automatic
retirement of a context originally applied without this policy.

Validate and review the customized plan before acknowledging its data-loss
authorization. Removal first records pending storage retirement, removes its
consumers and cleans managed-OS resources. A separate RAM worker verifies the
original machine and all targets, writes and reads back every addressable byte,
and sends a receipt that the controller must durably record before shutdown.
The erase worker has a four-hour deadline; the full invocation admits at most
six hours including media preparation, BMC boot, startup and cleanup. Choose
target capacities and qualified hardware that can complete full write and
readback within that bound; no throughput or completion time is assumed.
Storage verifies those receipts against the original Ceph disk bindings before
Machine claims can be released. A lost receipt, changed disk or partial readback
keeps removal incomplete; continue the original operation without substituting
new hardware. This is logical overwrite, with no claim about SSD remapped sectors
or hardware sanitization. Physical image, interruption and full-cluster
qualification remain unrun.

If the original records positively prove that neither OS installation nor the
Ceph cluster started, cleanup can release the unused claims without overwriting
disks. It records retention without installation rather than an erase receipt.
Missing or uncertain history cannot authorize that cleanup.

## Prepare and install

Complete the controller setup, media import and BMC/media trust preparation in
the [physical installation runbook](../../machines/lab-baremetal/README.md#run-it).
Set `physical_ceph_input` to your customized private input directory. Review
every root and Ceph disk against independent inventory, including the original
`eraseOnRemoval` policy, before initializing the context:

```sh
./bin/bootwright validate -f "$physical_ceph_input"
./bin/bootwright context init --name physical-ceph --input-dir "$physical_ceph_input"
./bin/bootwright secret generate --context physical-ceph
./bin/bootwright secret set --context physical-ceph --name lab-bmc-credentials --username <account> --password-stdin
./bin/bootwright secret set --context physical-ceph --name lab-bmc-ca --certificate-file /path/to/bmc-ca.pem
./bin/bootwright secret set --context physical-ceph --name ibm-registry --username cp --password-stdin
./bin/bootwright secret check --context physical-ceph
./bin/bootwright plan --context physical-ceph
```

The generated artifact certificate chains to `artifact-server-ca`; the BMC's
declared media trust must accept that authority. The BMC CA supplied above
independently verifies the controller-to-BMC connection. Complete any additional
context-store Secrets introduced by the selected service or host variant before
applying. Keep the context's bound credentials and recovery values available
through removal.

The complete frozen plan includes physical installation's `data-loss`
authorization, including during earlier staged applies. Start with the machines
powered off and run the stages separately:

```sh
./bin/bootwright apply --context physical-ceph --stage controller --authorize data-loss
./bin/bootwright preflight controller --context physical-ceph
./bin/bootwright apply --context physical-ceph --stage infra-components,substrates --authorize data-loss
./bin/bootwright apply --context physical-ceph --stage machines --authorize data-loss
./bin/bootwright status --context physical-ceph
./bin/bootwright machine list --context physical-ceph --power-status
./bin/bootwright machine exec --context physical-ceph --name metal-01,metal-02,metal-03 -- lsblk -b -o NAME,TYPE,SIZE,SERIAL,WWN,ROTA,FSTYPE,MOUNTPOINTS
./bin/bootwright plan --context physical-ceph --stage clusters
./bin/bootwright apply --context physical-ceph --stage clusters --authorize data-loss
./bin/bootwright cluster info --context physical-ceph --name ceph-lab
./bin/bootwright diff --context physical-ceph --observe
```

Review the installed identity, final networking and every selected disk before
starting the clusters stage. After interruption, read `status` and repeat the
same stage and authorization against the original frozen input. A changed disk,
host key or UUID requires resolving the reported mismatch; a retry cannot adopt
the replacement.

## Exercise the physical matrix

Use the [P1-P7 profile catalog](../../../specs/storage-coverage.md#profile-catalog)
and each linked README for service intent, client checks, failure/recovery,
replay and measured resource obligations. Adapt a private copy of each
canonical profile:

1. Convert only the Machines named by each StorageCluster's topology to the
   bare-metal provider shape above. Preserve independently managed clients,
   initiators and their context boundaries. Declare every physical ethernet
   NIC and its MAC, verified BMC trust, explicit root path and delivered host
   key. Preserve the profile's final network and host/security variants; FIPS
   needs admitted fleet and delivered host-key types.
2. Replace each selected data, DB and WAL path with that host's canonical WWN
   path. Give each host its own OSD ServiceSpec and unique `service_id`, retaining
   the original encryption, TPM2 and allocation fields. Carry the other native
   services and declarative storage resources across unchanged, adjusting only
   the actual host/network coordinates in the private copy.
3. Include the root disk and every selected data/DB/WAL disk in that Machine's
   original erase policy, with independently verified serial and exact byte
   capacity. Validate and inspect the complete plan before the first apply.
4. Follow the staged physical sequence above, then the canonical profile's
   service and client checks on physical hosts. Record real capacity and wall
   time in place of guest allocation/balloon measurements, and exercise the
   [physical qualification cases](../../../specs/storage-coverage.md#physical-qualification).

P1 uses six hosts with two data disks and one DB disk per host. P2 uses six
storage hosts and retains its separate client. P4 retains its independently
managed initiator; P7 retains two separate clusters and their client/peer
boundaries. P7's RGW multisite credential interface remains blocked by
[B483](../../../specs/milestones/m2.md#b483); planning its currently implemented
RBD-mirroring trajectory does not qualify full P7.

`TestPhysicalCephMatrixPreservesCanonicalPlans` adapts these canonical inputs
in memory and checks production admission, frozen physical installation/erase
requests and inverse ordering for all seven profiles. It preserves their
storage resources and independent client declarations. It proves composition;
firmware behavior, actual service/client effects and owner acceptance remain
operator gates. Record each executed case using the
[physical ledger template](../../../docs/acceptance.md#physical-qualification-row).

## Remove and continue cleanup

Retain the required data and review the removal plan first. Keep Machines
reachable for any online cleanup, including managed unregistration. Begin with:

```sh
./bin/bootwright destroy --context physical-ceph --authorize data-loss
./bin/bootwright status --context physical-ceph
```

Follow the reported `Stop first` and `Stop after online cleanup` groups. Stop a
deferred Machine only after Bootwright records its required online cleanup;
then use `machine stop` for the named Machines and repeat the same destroy with
`--authorize data-loss`. A failed online cleanup must be resolved before those
Machines stop. Bootwright performs no implicit power-off to bypass this proof.

The offline worker then overwrites and reads back the exact original targets,
and the controller records its receipt before shutdown. Storage verifies every
original disk binding against those receipts before the physical claim releases.
After interruption, keep the context and original hardware intact, inspect
`status`, and repeat destroy with the same authorization. Retain the final
receipt, media cleanup and claim-release evidence before deleting the context.
Omitted erase authority records retention and cannot complete realized physical
Ceph retirement.
