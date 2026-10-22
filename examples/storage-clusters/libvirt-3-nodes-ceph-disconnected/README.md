# Disconnected three-node Ceph (P6)

This is the P6 input and runbook for B493 and the B484 core cells. It has host-independent validation and planning coverage; no live acceptance is claimed. All addresses and image digests are synthetic. Copy the directory to operator-owned storage and replace every synthetic digest with the verified linux/amd64 manifest digest in your mirror before importing it. Never put credentials or real environment details in tracked files.

## Budget and prerequisites

Three RHEL 9.8 guests declare 4 vCPU and 20 GiB RAM each, 60 GiB root and 80 GiB SSD data disks: 12 vCPU, 60 GiB guest RAM, 180+240 GiB disk. Reserve another 4 GiB for the controller, plus operator-managed mirror storage and services. The core daemon memory baseline is 11, 11 and 8 GiB (30 GiB total); 2 GiB OS headroom per guest and a provisional 7 GiB auxiliary allowance per guest fit within the declaration. This allowance is a qualification budget, not a vendor recommendation or measured peak. Record the actual placements and memory before accepting P6; adjust the operator-owned input upward if needed. MON 3, MGR 2 and OSD 3 retain quorum and explicit size/min-size 2/2.

The profile places Prometheus on rhel-01, Alertmanager on rhel-02, Grafana on rhel-03, node-exporter on all three hosts, and both RGW and ingress (HAProxy and Keepalived) on rhel-01 and rhel-02. The six auxiliary images and the core image are all authored beneath the same Entitlement registry. Core packages are `20.2.2-349.el9cp` and cephadm-ansible `5.0.3-1.el9cp`; these authored selections do not claim live release qualification. Package and container OS versions are independent.

Prepare the mirror externally on the controller or another reachable isolated service. Bootwright consumes it and does not install or populate a registry. Preserve every selected image's linux/amd64 manifest, configuration and layer closure, and the full signed RPM repository with its original metadata and dependencies. Serve the unchanged IBM publisher key at the authored key URL. Its fingerprint must match the code pin in [native_signatures.py](../../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/native_signatures.py); a mirror CA never substitutes for publisher authentication. Preserve the RHEL boot/DVD media and their vendor checksums. The DVD tree supplies the RHEL dependency closure.

The mirror serves registry HTTPS and package/key HTTPS at `mirror.lab.example.test`, with a certificate verified by `mirror-ca`. Its DNS record must resolve to the reachable isolated address. The key and repository must share the HTTPS host and effective port; no redirect or public fallback is allowed. The registry must serve all blobs locally without public delivery redirects. No registry credential is installed in native Podman's authfile; invocation-local brokers retain it. The artifact server's separate generated certificate verifies its own DVD tree.

The libvirt network uses `forward: none`; guests have no default route. Provide local DNS and NTP, and change the synthetic upstreams to isolated reachable services. Block public egress on the controller as well as the guests and retain firewall/capture evidence outside the repository. DNS, NTP, image layers, RPM dependencies, controller packages, locked automation wheels and collections must already be available locally. The local controller package repositories must supply setup and virtualization prerequisites. Existing setup supports `--download-only --dest-dir` on a connected preparation host and `--wheel-mirror` on the isolated host; these flags prepare automation wheels, not Ceph images or RPM mirrors.

## Initial setup

Follow the [controller prerequisites](../../machines/lab-rhel/README.md#host-prerequisites), including KVM and host package sources. Set the controller, service bind addresses and TLS SANs consistently in your copy. The bridge address is `198.51.100.1`; do not substitute a public proxy. Confirm the mirror, artifact listeners and local services have distinct ports. Prepare the locked controller runtime before disabling its public route:

```sh
make build
./bin/bootwright setup --download-only --dest-dir /path/to/transfer/wheels
./bin/bootwright setup --wheel-mirror file:///path/to/transfer/wheels
./bin/bootwright preflight controller
./bin/bootwright media add --name rhel-9.8-x86_64-boot.iso --from-file /path/to/rhel-9.8-x86_64-boot.iso
./bin/bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file /path/to/rhel-9.8-x86_64-dvd.iso
./bin/bootwright media list --checksums
```

The download command runs on the connected preparation host. Run the remaining commands on the isolated controller; preserve the locked wheel set and media through continuation and removal.

## IBM entitlement

Acquire the original IBM artifacts under the operator's entitlement and review the IBM license before accepting it. Mirror access uses the `ibm-registry` Secret and the mirror's account, not necessarily IBM's registry username. The Entitlement still identifies IBM Storage Ceph. RHEL media authorization is separate.

The selected 20.2.x build uses automatic Call Home, so the authored `enabled` intent is retained. Public egress remains blocked; record any Call Home connectivity warning. This profile does not disable native reporting by changing a release rule and does not claim reporting works offline. Do not accept unrelated health warnings as equivalent to a blocked reporting endpoint.

## Create the context

The tracked directory validates as 26 files and 26 objects. Validation checks declarations, not mirror contents. Complete all image replacements, DNS, trust and services in the copied input first:

```sh
./bin/bootwright validate -f /path/to/adjusted/libvirt-3-nodes-ceph-disconnected
./bin/bootwright context init --name ceph-offline --input-dir /path/to/adjusted/libvirt-3-nodes-ceph-disconnected
./bin/bootwright context use --name ceph-offline
./bin/bootwright secret generate
./bin/bootwright secret set --name mirror-ca --certificate-file /path/to/mirror-ca.pem
./bin/bootwright secret set --name ibm-registry --username mirror-reader --password-stdin --yes
./bin/bootwright secret check
./bin/bootwright render effective
./bin/bootwright plan --stage controller
```

Supply passwords from standard input, never YAML or arguments. Before the first apply, an adjusted copy may be imported with `./bin/bootwright context update --name ceph-offline --input-dir /path/to/adjusted/libvirt-3-nodes-ceph-disconnected --yes`. After registration, input changes require the documented recovery/removal journey. On a FIPS controller select an RSA or ECDSA generated fleet key in your copy before import.

## Apply by stage

Keep public egress blocked throughout. Start the memory sampler from Measured budget in a second terminal before the clusters stage:

```sh
./bin/bootwright apply --stage controller
./bin/bootwright apply --stage infra-components,substrates
./bin/bootwright apply --stage machines
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- ip route
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- getent hosts mirror.lab.example.test
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- chronyc tracking
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- lsblk -b -o NAME,TYPE,SIZE,SERIAL,ROTA,FSTYPE,MOUNTPOINTS
./bin/bootwright plan --stage clusters
./bin/bootwright apply --stage clusters
./bin/bootwright status
```

Prove `/dev/sda` is the owned nonrotational data disk, the root is `/dev/vda`, local time is synchronized and there is no guest default route before cluster apply. Image resolution freezes every selected digest before host effects; package acquisition verifies the full signed closure before installation. Missing images, trust failure, unavailable RPMs or a mismatched publisher must fail closed; repair the mirror or the authored input through the stated recovery path, never enable public fallback.

## Validate the cluster

```sh
./bin/bootwright cluster info --name ceph-lab
./bin/bootwright preflight storage-cluster
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- rpm -q cephadm ceph-common cephadm-ansible
```

Use the exact `cephCommand` printed by `cluster info` (including context, node, configuration and keyring). Run these read-only checks and retain redacted output locally:

```text
<cephCommand> -s
<cephCommand> health detail
<cephCommand> orch ls --format json-pretty
<cephCommand> orch ps --format json-pretty
<cephCommand> osd tree
<cephCommand> osd pool ls detail
<cephCommand> mgr services
```

Expect three MONs in quorum, one active and one standby MGR, three OSDs up/in, all declared auxiliary daemons running, and every container image digest matching the frozen mirror reference. Verify Prometheus targets, the Grafana endpoint, local ingress VIP and both RGW backends. Capture no credentials, keyrings or native configuration dumps. Check the controller and guest egress counters/capture: no successful public exchange is allowed.

## Client access

Use the pool-scoped `rbd-canary` client through the [core client procedure](../libvirt-3-nodes-ceph/README.md#validate-the-cluster), retaining its configuration/keyring privately. The declared `rbd-disk` is 1 GiB. Perform its bounded write/read canary and retain the matching data checksum; do not repeat an ambiguous write. Query the ingress endpoint `http://198.51.100.51:8082/` from the isolated controller and retain its HTTP status (an unauthenticated RGW refusal still proves the route). This example's ingress is HTTP; add an independently qualified TLS binding before carrying sensitive object data.

## Failover

Identify the admin node and choose a different guest. Stop it with `./bin/bootwright machine stop --name rhel-03` after confirming that choice. Within 180 seconds record quorum, OSD state, client read and ingress response; the remaining two hosts must preserve min-size 2. Restart with `./bin/bootwright machine start --name rhel-03`, wait up to 600 seconds and repeat health, OSD and auxiliary checks. Never lower replica/min-size to make the test pass. Record whether the stopped node held the ingress VIP and whether it moved.

## Interruption and continuation

During the first clusters apply, interrupt once with Ctrl-C after a Ceph block starts. Run `./bin/bootwright status`, retain the operation identity and private logs, then repeat `./bin/bootwright apply --stage clusters` with public egress still blocked. Record that it continued the same frozen operation and reused the same image digests/package decision. Never remove custody records or supply new secret versions to bypass a refusal.

## Replay

Run `./bin/bootwright apply` on the completed context. It must settle without effect, new image resolution or changed package/image identity. Retain status and mirror request evidence.

## Check that changed intent is refused

In the copied input change one image selection, run `validate`, then attempt `context update` while an operation is paused. It must refuse invalidating that operation. Restore the copied input. Separately, after apply completes, validate a changed copy and import it with `context update`: expect the completed-apply warning, not refusal. Run `apply` and require changed frozen intent to be refused before any native effects. Restore the original input and import it again with `context update` before continuation or retirement. To adopt another image, use `diff`, destroy, then update and start a new operation. Do not clear context state.

## Measured budget

Record every field: `Guests`, `Declared vCPU`, `Declared RAM GiB`, `Declared disk GiB`, `MemAvailable at (a)/(b)/(c)`, `Minimum MemAvailable during clusters stage`, `Allocated vs apparent disk bytes`, `Per-guest balloon RSS`, `Daemons by type`, `Clusters-stage wall time` and `OOM kills`. Declared values are 3, 12, 60 and 420 respectively. Measurement points are (a) before apply, (b) after clusters, and (c) after client/failover. Keep all observations local. Any OOM kill fails the profile.

```text
nproc
grep MemAvailable /proc/meminfo
sudo du -sb --apparent-size /var/lib/libvirt/images/bootwright/ceph-offline
sudo du -sb /var/lib/libvirt/images/bootwright/ceph-offline
virsh --connect qemu:///system domstats --balloon --vcpu
<cephCommand> orch ps --format json-pretty
<cephCommand> df
```

Read each guest's journal for the run and count `Out of memory` lines. Record stage start/end time and the maximum observed daemon count per type. Set `ev` to a private local evidence directory and run the sampler until cluster apply returns:

```sh
while sleep 10; do date +%s; grep MemAvailable /proc/meminfo; done > "$ev/memavailable-clusters.txt"
```

## Cleanup

Retain data first. With guests running, `./bin/bootwright destroy --authorize data-loss` must refuse `lifecycle.live`. Then retire all three owned guests together:

```sh
./bin/bootwright machine stop --name rhel-01,rhel-02,rhel-03
./bin/bootwright destroy --authorize data-loss
./bin/bootwright status
./bin/bootwright cluster info --name ceph-lab
virsh --connect qemu:///system list --all
virsh --connect qemu:///system net-list --all
./bin/bootwright context delete --name ceph-offline --purge
```

Prove owned guests, data disks, network and cluster evidence are absent before deleting the context. Repeat an interrupted destroy with the same authorization after inspecting status. External mirrors, media and controller packages remain operator-owned; Bootwright does not remove them. Record this run under the [acceptance process](../../../docs/acceptance.md) only after all gates pass.
