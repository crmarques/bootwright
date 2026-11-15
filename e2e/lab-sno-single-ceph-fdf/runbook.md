# SNO with single-host Ceph through FDF

This experimental rehearsal extends the isolated FIPS/TPM2 SNO case with one
managed Ceph host and the existing `fdf` add-on. It exercises
[B1420](../../specs/milestones/m4.md#b1420) and the remote-install prerequisite
[B1421](../../specs/milestones/m1.md#b1421). It does not close the production
Data Foundation gate B761. Live provisioning, resource measurements and
operator acceptance remain unrun.

The fixed defaults are Ceph **9.9.1.2**, OpenShift **4.22.22**, and IBM FDF
**4.22.8** (`stable-4.22`, `odf-operator.v4.22.8`). OpenShift's stable mirror
and IBM's catalog were checked on 2026-11-15; the IBM catalog is pinned by
digest. [Pin and support evidence](../../.agents/knowledge/single-host-ceph-fdf.md)
separates native test capability from vendor production support. Keep the
Ceph 9.9.1.2 package and image coordinates together. Do not substitute a
Red Hat ODF CSV for the IBM catalog's CSV.

| VM | vCPU | RAM GiB | Disk GiB |
| --- | --- | --- | --- |
| FIPS controller | 2 | 3 | 40 |
| SNO, FIPS and TPM2 root encryption | 8 | 16 | 100 |
| Ceph, one MON/MGR/MDS/RGW and two OSDs | 3 | 6 | 20 root + 2 × 10 data |
| Provider reserve | 1 | 2 | 10 |
| Total budget | 14 | 27 | 190 |

These are intentionally small test sizes, not vendor sizing recommendations.
NooBaa is disabled through `fdf.multiCloudGateway: false`; object storage uses
external RGW and its FDF StorageClass. The two OSDs each have a fixed 2 GiB
memory target, leaving the remaining Ceph memory for other daemons. Measure
peak use and Pending/OOM events during acceptance; passing static capacity
admission does not establish runtime fit. Swap is not capacity. The disk
budget excludes cached ISOs, package publications, installer staging and
logs; the apply gate also checks those. Import one ISO at a time on the
controller and remove its temporary source after verifying its stored digest.

Run this case after cleaning up any other rehearsal. It shares service ports,
VM names, the infrastructure CIDR and VLAN IDs with the source case. Preserve
other contexts and evidence; never remove their resources by name to make room.

| Network | Attached machines | Purpose |
| --- | --- | --- |
| `openshift-infra` | host, FIPS controller, SNO | installation, API and node infrastructure; the future node-to-node network |
| `ceph-client` | host, FIPS controller, Ceph, SNO | every MON, OSD public, MDS, RGW and FDF client connection |
| `ceph-replication` | host bridge, Ceph only | private OSD replication; no SNO or controller attachment |
| `staging` | SNO trunk VLAN, external staging peer | staging ingress and egress |
| `production` | SNO trunk VLAN, external production peer | production ingress and egress |

The bootstrap context owns infrastructure and client networks. The FIPS
context attaches to them and owns replication and both environment VLANs.
No `routedNetworkRefs` joins these networks. Infrastructure and Ceph client
have NAT for image/package acquisition; replication and the workload VLANs
have `forward: none`; the environment host legs are addressless. SNO has infrastructure and client NICs plus one VLAN
trunk carrying staging and production. Pods use primary OVN networking.
The case-owned NMState policy enables forwarding only on the Ceph client
NIC, so reverse-NATed replies can reach FDF pods under Restricted forwarding.
The shared isolation fixture supplies two IngressControllers, two EgressIPs,
namespace policies and a pre-DNAT node filter. A VM with several NICs is not
itself an isolation boundary: the traffic assertions are required.

Bootwright core owns hosts, networks, installation, Ceph resources and the
export. NMState, MetalLB and FDF remain add-ons. Application policies, ingress
shards and test apps remain native platform/E2E fixtures. There is no separate
storage product or application networking API for this case.

## Prepare on the host

Start in the checkout being qualified, with Bash, `set -euo pipefail` and
`umask 077`. Follow the shared [host preparation](../README.md#conventions)
for the pinned build, controller prerequisites, verified RHEL 9.8 boot/DVD
media, libvirt and required diagnostic tools. Check `MemAvailable`, free space
under `/var/lib/libvirt`, and listeners before provisioning. Reserve the full
combined budget above, even though two contexts plan it in stages.

Set `PULL_SECRET_FILE` to a private OpenShift pull-secret file containing the
Red Hat and IBM Fusion entitlements, and `IBM_KEY_FILE` to the private IBM
registry entitlement password file. Keep both outside Git. Install `oc` for
4.22.22 with its published checksum as in the source case; use that exact
client throughout this run. Freeze the run's executable and case sources as
in the source [runbook](../lab-sno-fips-controller/runbook.md), so later edits
to the checkout do not change a running case.

```bash
CASE="$PWD/e2e/lab-sno-single-ceph-fdf"
ISOLATION_CASE="$PWD/e2e/lab-sno-fips-controller"
RUN_DIR=$(mktemp -d "${TMPDIR:-/tmp}/sno-ceph-fdf.XXXXXXXX")
VALUES="$RUN_DIR/values.yaml"
INPUT_DIR="$RUN_DIR/input"
PROFILE="$RUN_DIR/isolation"
CTX_BOOT=lab-sno-single-ceph-fdf-bootstrap
CTX_FIPS=lab-sno-single-ceph-fdf
CONTROLLER_NAME=fdf-controller
mkdir -p "$PROFILE/app" "$RUN_DIR/catalog"
: "${PULL_SECRET_FILE:?Set a private pull-secret path}"
: "${IBM_KEY_FILE:?Set a private IBM entitlement password path}"
test -r "$PULL_SECRET_FILE"
test -r "$IBM_KEY_FILE"
```

Resolve the exact NMState and MetalLB CSVs from the chosen Red Hat 4.22 index,
using the source case's catalog extraction procedure. Record their channel
and CSV in `VALUES` under `addons.subscriptions.nmstate` and `.metallb`.
Resolve the approved Python image to its **Linux amd64 manifest** and record
it under `isolation.image`; never pin the multi-architecture index. Record a
stable host address in `host.address` too. No actual address, key or catalog
credential belongs in a tracked values file.

```bash
e2e/render.sh lab-sno-single-ceph-fdf --values "$VALUES" --out-dir "$INPUT_DIR"
bootwright validate -f "$INPUT_DIR/bootstrap"
bootwright validate -f "$INPUT_DIR/fips"

helm template isolation "$ISOLATION_CASE/isolation" \
    -f "$CASE/values.yaml" -f "$VALUES" --output-dir "$PROFILE/rendered"
MANIFESTS="$PROFILE/rendered/sno-environment-isolation/templates"
python3 - "$MANIFESTS/verification-config.yaml" "$PROFILE/config.json" <<'PY'
import json
import pathlib
import sys
lines = pathlib.Path(sys.argv[1]).read_text().splitlines()
config = next(json.loads(line.strip()) for line in lines if line.lstrip().startswith('{'))
pathlib.Path(sys.argv[2]).write_text(json.dumps(config) + '\n')
PY
cp -a "$ISOLATION_CASE/app/." "$PROFILE/app/"
python3 "$PROFILE/app/verify.py" --config "$PROFILE/config.json" --mode config
helm template client-network "$CASE/platform" -f "$CASE/values.yaml" -f "$VALUES" \
    > "$RUN_DIR/ceph-client-network.yaml"
helm template storage "$CASE/app" -f "$CASE/values.yaml" -f "$VALUES" \
    > "$RUN_DIR/storage-write.yaml"
helm template storage "$CASE/app" -f "$CASE/values.yaml" -f "$VALUES" \
    --set storageCheckMode=read > "$RUN_DIR/storage-read.yaml"
```

The optional block/file-only render sets `ceph.object=false` consistently in
`VALUES` before every render. The default includes RGW and its OBC fixture.

## Bootstrap and hand over to the FIPS controller

```bash
bootwright context init --name "$CTX_BOOT" --input-dir "$INPUT_DIR/bootstrap"
bootwright secret generate --context "$CTX_BOOT"
bootwright secret check --context "$CTX_BOOT"
bootwright plan --context "$CTX_BOOT"
bootwright apply --context "$CTX_BOOT" --stage controller --yes
bootwright preflight --context "$CTX_BOOT" --stage controller
bootwright apply --context "$CTX_BOOT" --yes
bootwright machine exec --context "$CTX_BOOT" --name "$CONTROLLER_NAME" -- \
    cat /proc/sys/crypto/fips_enabled
```

Require `1`. Use the source case's explicit controller handoff and SSH trust
establishment steps, with **these** context names and `CONTROLLER_NAME`:
transfer the pinned Bootwright binary and `oc`, rendered `fips` input,
`PROFILE`, `ceph-client-network.yaml`, both storage manifests, pull secret and
IBM password file. Install
controller prerequisites inside the VM. Import the verified RHEL boot and DVD
media into that controller's media store, retaining their names and digests;
Ceph's RHEL installation now needs both there. Use direct bounded URL imports
or copy/import/remove one ISO at a time to stay inside the root disk budget.
Do not run the source case's render or apply commands: they select another
input. Preserve its one-key authorization and known-host pinning procedures.

On the controller, initialize this context and bind its credentials:

```bash
CTX_FIPS=lab-sno-single-ceph-fdf
bootwright context init --name "$CTX_FIPS" --input-dir "$HOME/fips"
bootwright secret set --context "$CTX_FIPS" --name openshift-pull-secret \
    --value-file "$HOME/openshift-pull-secret" --yes
bootwright secret set --context "$CTX_FIPS" --name ibm-registry \
    --username cp --password-file "$HOME/ibm-key" --yes
bootwright secret generate --context "$CTX_FIPS"
```

Bind `libvirt-host-known-hosts` from the physical host's verified ECDSA public
host key, and authorize only this context's `libvirt-host-key` public key on
that host, restricted to the controller's infrastructure address. The source
case documents the exact commands and preservation of prior authorization.
No `StrictHostKeyChecking=no`, first-use trust or shared host key is permitted.
The dedicated `ceph-host-key` is generated separately: it is delivered only
through private installer media and later proves the installed Ceph VM.

```bash
bootwright secret check --context "$CTX_FIPS"
bootwright plan --context "$CTX_FIPS"
bootwright apply --context "$CTX_FIPS" --stage controller --yes
bootwright preflight --context "$CTX_FIPS" --stage controller
bootwright apply --context "$CTX_FIPS" --stage infrastructure-services,substrates --yes
bootwright apply --context "$CTX_FIPS" --until sno.nmstate --yes
```

At this pause SNO and NMState are ready; FDF has not started. Obtain its
kubeconfig and establish the source case's private `oc` wrapper now, using
this context and a new private controller-side `RUN_DIR`. Then apply the
client policy before continuing. It restates the existing client address and
changes forwarding on that NIC alone:

```bash
oc apply --dry-run=server -f "$HOME/ceph-client-network.yaml"
oc create -f "$HOME/ceph-client-network.yaml"
oc wait nncp/sno-ceph-client-forwarding --for=condition=Available --timeout=10m
oc debug node/master-0 -- chroot /host cat /proc/sys/net/ipv4/conf/enp3s0/forwarding
```

Require the effective value `1`, no Degraded/NoMatchingNode condition, and the
original address and routes. Ordinary OVN masquerading selects the outgoing
client NIC's address; this path needs no additional EgressIP. Continue only
after the policy converges:

```bash
bootwright apply --context "$CTX_FIPS" --yes
```

Keep the source case's resolver, FIPS and TPM2 installation checks. Ceph SSH,
MONs and client services must use the Ceph client leg; Ceph's replication
address must occur only on Ceph. Confirm the actual NIC inventory with
`bootwright machine exec` for Ceph and `oc debug node/master-0 -- chroot /host
ip -j address` for SNO. There must be no replication NIC on SNO and no routes
between staging and production on the host. Verify `singleHostDefaults` and
native OSD-level CRUSH placement; do not accept degraded pools or bypass
readiness to finish a test.

## Verify storage, isolation and persistence

Keep the private kubeconfig and `oc` wrapper established at the NMState pause.
Keep credentials out of stdout and logs.
Require FDF's declared CSV, Ready/ReconcileComplete StorageCluster, and the
RBD/CephFS StorageClasses. With object enabled require the RGW class too.
No NooBaa object or `openshift-storage.noobaa.io` StorageClass is required.
A cipher/key failure is a failed acceptance, not permission to weaken CephX.

```bash
oc -n openshift-storage get csv,storagecluster,pods
oc get storageclass
oc apply --dry-run=server -f "$HOME/storage-write.yaml"
oc apply -f "$HOME/storage-write.yaml"
oc -n storage-check wait --for=condition=Ready pod/storage-writer --timeout=15m
oc -n storage-check logs storage-writer
```

Require both PVCs Bound and, with object enabled, the OBC Bound. The fixture
writes, flushes and reads a distinct marker on RBD and CephFS, and performs a
signed PUT/GET using only the OBC's generated RGW credentials. It prints no
credentials. Its namespace has default-deny ingress and only DNS/RGW egress.
The laboratory RGW listener is HTTP on the isolated client network; this does
not qualify production transport security.

The separate `storage-reader` pod mounts the same CephFS claim read-only while
the writer is running. Require its successful completion and sharing marker:

```bash
oc -n storage-check wait --for=jsonpath='{.status.phase}'=Succeeded \
    pod/storage-reader --timeout=15m
oc -n storage-check logs storage-reader
```

Then prove persistence after destroying the writer, with the same claims and
bucket. The read fixture never writes a marker and fails on missing data:

```bash
oc -n storage-check delete pod storage-writer --wait=true
oc apply -f "$HOME/storage-read.yaml"
oc -n storage-check wait --for=condition=Ready pod/storage-writer --timeout=15m
oc -n storage-check logs storage-writer
```

On the physical host, after the substrate stage has created its VLAN legs,
create the two external peer attachment networks. Stop if either name already
exists; this step creates no guest bridge and starts no workload:

```bash
TRUNK=$(jq -er '.trunk.bridge' "$PROFILE/config.json")
ip -d link show "$TRUNK"
BRIDGE_FILTER=$(sysctl -n net.bridge.bridge-nf-call-iptables 2>/dev/null || true)
[[ -z "$BRIDGE_FILTER" || "$BRIDGE_FILTER" == 0 ]]
for env in staging production; do
    if sudo podman network exists "e2e-$env" || sudo podman container exists "e2e-$env-app"; then
        echo 'test peer resource already exists'; exit 1
    fi
done
while IFS=$'\t' read -r env vlan cidr; do
    ip -json address show "$TRUNK.$vlan" | jq -e 'all(.[]; .addr_info | length == 0)'
    sudo podman network create --internal -d macvlan -o "parent=$TRUNK.$vlan" \
        --subnet "$cidr" "e2e-$env"
done < <(jq -r '.environments | to_entries[] | [.key,.value.vlan,.value.cidr] | @tsv' "$PROFILE/config.json")
```

Reuse the shared isolation runbook from
[apply the node filter before workloads](../lab-sno-fips-controller/isolation/runbook.md#apply-the-node-filter-before-workloads)
through its ingress/egress, external peer and reboot assertions. Keep this
case's `CTX_BOOT`, `CTX_FIPS`, `CONTROLLER_NAME`, `RUN_DIR` and `PROFILE`; the
shared commands default the controller name only when it is unset. Apply the
filter and wait for its MachineConfig rollout before exposing any environment
workloads. Keep the two external peers on their own VLANs, with deliberate
routes through SNO, and require both positive controls and all cross-network
negative probes. Add probes from each peer to the Ceph client address and
replication address: either successful connection is a failure. Storage CSI
traffic belongs on SNO's client NIC, outside those workload peer paths.

After the unattended SNO reboot, require FIPS, root LUKS/TPM2, healthy cluster
operators, FDF readiness, both traffic matrices and the storage **read** check
again, including client-policy Available and effective enp3s0 forwarding `1`.
Reboot Ceph independently and repeat storage read/readiness; do not
claim availability while its sole host is down. Record memory peaks, OOM and
scheduling events from all three VMs and the provider host.

## Cleanup and evidence

Delete the storage-check namespace and wait for its PVCs, PVs, OBC and bucket
cleanup before destroying either context. Delete `sno-ceph-client-forwarding` after
those consumers are gone; deleting an NNCP does not revert the NIC state, so
forwarding remains available for FDF retirement until the disposable SNO VM
is destroyed. Do not force finalizers. Follow the
shared isolation cleanup for peer containers/networks and platform fixtures;
leave Bootwright-owned VLAN bridges for destroy. Remove SNO consumers before
Ceph by the context's dependency-ordered destroy:

```bash
bootwright destroy --context "$CTX_FIPS" --yes
bootwright status --context "$CTX_FIPS"
```

Preserve private evidence using the shared
[evidence procedure](../README.md#preserve-evidence-before-context-purge) before
purging state. Restore only the SSH authorization added for this run. On the
physical host destroy the bootstrap context last, then preserve its evidence:

```bash
bootwright destroy --context "$CTX_BOOT" --yes
bootwright status --context "$CTX_BOOT"
```

Require no case guests, emulated BMCs, artifact listeners, owned network or
Ceph residue. Only then purge the contexts and private temporary run files.
Only the human owner accepts the rehearsal in the acceptance ledger.
