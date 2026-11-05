# lab-sno-ceph-df

Data Foundation over managed IBM Storage Ceph and single-node OpenShift, all on
one libvirt host and in one Bootwright context. The OpenShift cluster `sno` is
one guest, `sno-01` at `198.51.100.31` (12 vCPU, 32 GiB, 120 GiB, an emulated
TPM), installed by the agent installer through an emulated Redfish BMC, as in
[lab-sno](../lab-sno/runbook.md). The Ceph cluster `ceph-e2e` is three RHEL 9.8
guests `rhel-01` to `rhel-03` at `198.51.100.11` to `198.51.100.13`, each 4 vCPU
and 8 GiB with a 40 GiB root and one 50 GiB virtual SSD for its OSD, as in
[lab-3-nodes-ceph](../lab-3-nodes-ceph/runbook.md), trimmed to what Data
Foundation needs: MON x3, MGR x2, one OSD per guest, a CephFS filesystem `files`
with two MDS and the `csi` subvolume group, the replicated RBD pool `rbd-data`,
the CephFS metadata and data pools, the `ssd-hosts` placement policy and one
`prometheus` service, which the `CephConsumer` admission requires. There is no
object gateway, NFS, SMB, NVMe-oF, observability stack or canary client. The
`CephConsumer` `sno-storage` names the RBD pool and the filesystem, no gateway,
and mints its CephX principals at `fdf.cephxKeyType` (`aes256k`). The
`ClusterAddonBinding` `sno-data-foundation` installs the `fdf` package on `sno`.
Beside them the controller runs managed DNS, NTP, proxy and artifact services.
Context `lab-sno-ceph-df`, 4 guests, 25 host CPUs.

It rehearses the `fdf` path of [B85](../../specs/milestones/m4.md#b85) and
[B761](../../specs/milestones/m4.md#b761) on a single node, with the Ceph side of
[B86](../../specs/milestones/m4.md#b86). B761's own profile is addons-df, a
compact cluster with a TLS object gateway, so this case accepts none of them: a
run here is an observation, not the gate. It also produces evidence for the
Data Foundation client question that
[B628](../../specs/milestones/m2.md#b628) names the root of: whether the client
and the node's kernel accept the monitors' `aes256k` keys. Without an object
gateway, the `ceph-rgw` class and the gateway trust parts of B761 are not
exercised.

Run every numbered step in order in one Bash session from the checkout under
test. A successful run includes every check, the destroy and cleanup. An
unexpected failure fails the run; retain its private records for cleanup. Run
another input variant from the beginning.

## Inputs

| `--set` | Default | Effect |
| --- | --- | --- |
| `fdf.subscription.channel`, `fdf.subscription.startingCSV` | none | the `odf-operator` OLM channel and its exact CSV; the render fails with `required` while either is empty |
| `fdf.subscription.source` | `redhat-operators` | `redhat-operators` installs Red Hat OpenShift Data Foundation; `ibm` installs IBM Fusion Data Foundation through the owned catalog source `isf-data-foundation-catalog` |
| `fdf.catalog.image`, `fdf.catalog.priority` | none, `100` | with `ibm`: the IBM catalog image the operator chooses (required) and the catalog source's priority |
| `fdf.cephxKeyType` | `aes256k` | the type of every key the consumer mints; see [Key types](#key-types) |
| `fdf.defaultStorageClass` | `false` | make the RBD class the cluster's default StorageClass |
| `fdf.timeout` | `1h` | how long Bootwright waits for the operator and the StorageCluster |
| `openshift.version` | `4.21.15` | the OpenShift release |
| `sno.cpu`, `sno.memoryMiB`, `sno.diskGiB` | `12`, `32768`, `120` | the node's size; Data Foundation's NooBaa pods request about 10.75 GiB and 2.8 CPU beside the platform, so 16 GiB leaves them Pending |
| `cephNodes.cpu`, `.memoryMiB`, `.diskGiB`, `.osdDiskGiB` | `4`, `8192`, `40`, `50` | the Ceph guests' size |
| `ceph.*` | IBM Storage Ceph 9.9.2.0 | the release, package versions, image and prometheus digest, which move together |

The channel and CSV have no default. Read them from a cluster of the same
release and catalog:

```sh
oc get packagemanifest odf-operator -n openshift-marketplace -o json
```

(`status.channels[].name` and `.currentCSV`). Set `startingCSV` to the
channel's `currentCSV`, its head, read on the day of the run. With an older
CSV, odf-operator may approve an InstallPlan that also upgrades it to the head;
the installed CSV then never equals `startingCSV` and the apply fails only when
`fdf.timeout` expires ([B1130](../../specs/milestones/m4.md#b1130)). The first run therefore needs a
cluster to read from, such as a finished [lab-sno](../lab-sno/runbook.md) run
of the same `openshift.version` (its packagemanifest lists the Red Hat catalog),
or a cluster that serves the IBM catalog image you chose. Use a Data Foundation
release that reads AES256K keys: 4.21.11 or later on 4.21, 4.22.4 or later on
4.22.

The release and the other Ceph coordinates are those of
[lab-3-nodes-ceph](../lab-3-nodes-ceph/runbook.md#releases-b626); the other
Ceph releases there carry no recorded prometheus digest, so replace
`ceph.images.prometheus` with the digest of the same build, a linux/amd64
platform manifest. You set `openshift-pull-secret` and `ibm-registry`;
Bootwright generates the machine key, `ceph-cluster-key`, the BMC credentials
and TLS, the artifact server certificate and the cluster administrator key.

### Key types

The IBM Storage Ceph 9.9.x monitors allow only `aes256k`, and the consumer
refuses a key type the monitors disallow before it mints a principal
([evidence](../../.agents/knowledge/cephx-key-types.md#current-native-key-evidence)).
Bootwright never widens the monitors' cipher policy. The Rook and Data
Foundation documentation keeps the CSI key type `aes` until every node runs a
kernel of Linux 7.0 or later, because the kernel RBD and CephFS clients cannot
read `aes256k` keys before it. RHCOS 4.21 ships an older kernel, so the PVC
checks of step 6 can fail at the mount or map even with the StorageCluster
`Ready`. Record `uname -r` of the node (step 6) and the pod events; such a
failure is a finding for B628 and B761, not a mistake of this runbook, and
nothing in this case changes the cipher policy to avoid it.

## 1. Prepare the host

Complete [host preparation](../README.md#prepare-the-host), including the RHEL
9.8 boot and DVD ISOs as media and the IBM entitlement key file for
`cp.icr.io`. Prepare the OpenShift pull secret at `~/openshift-pull-secret`
with mode `0600`. With `fdf.subscription.source=ibm` that pull secret must also
carry `cp.icr.io` with user `cp` and the IBM entitlement key, beside the Red
Hat registries the bundle's images name: Bootwright never merges a registry
credential into a running cluster, so add the entry before `secret set`. The
rendered Entitlement carries `license.accept: true`; accepting the IBM license
terms is your act. Install an `oc` matching `openshift.version`, independent
of Bootwright, and require `oc version --client` to succeed. Require `python3`
and `aws` from the host administrator's approved packages:

```sh
command -v python3
command -v aws
oc version --client
```

[Capacity](../../specs/substrates.md#capacity-admission): the guests plus the
provider's `capacityReserve` (1 CPU, 2 GiB, 10 GiB) need 25 host CPUs not
reserved by other libvirt domains (12 + 3 x 4 + 1), 58 GiB of `MemAvailable`
(32 + 3 x 8 + 2; swap never counts) and, on the filesystem under
`/var/lib/libvirt`, 280 GiB for the disks (120 + 3 x (40 + 50) + 10). Require
400 GiB free: the installer's image copies, the RHEL package tree and the
cluster's growth come on top. Check with `nproc`, `grep MemAvailable
/proc/meminfo` and `df -h /var/lib/libvirt`. Keep `198.51.100.11` to
`198.51.100.13` and `198.51.100.31` unused and the ports of the host
preparation free. Reserve emulated BMC ports from 8000 for the four guests.

Record the build commit and require `Source clean` in `bootwright version`.

## 2. Render and initialize

Set the choices; the channel and CSV come from the packagemanifest above:

```sh
PULL_SECRET_FILE="$HOME/openshift-pull-secret"
IBM_REGISTRY_FILE="$HOME/ibm-ceph-registry.txt"
FDF_CHANNEL=""
FDF_CSV=""
FDF_SOURCE="redhat-operators"   # or ibm
FDF_CATALOG_IMAGE=""            # needed only with ibm
FDF_KEY_TYPE="aes256k"
```

Render once:

```sh
(
    set -e
    : "${FDF_CHANNEL:?Set FDF_CHANNEL from the same-release package manifest}" \
      "${FDF_CSV:?Set FDF_CSV to currentCSV for the selected channel}"
    if [ "$FDF_SOURCE" = "ibm" ]; then
        : "${FDF_CATALOG_IMAGE:?Set FDF_CATALOG_IMAGE to the IBM catalog image}"
    fi
    e2e/render.sh lab-sno-ceph-df \
      --set "fdf.subscription.channel=$FDF_CHANNEL" \
      --set "fdf.subscription.startingCSV=$FDF_CSV" \
      --set "fdf.subscription.source=$FDF_SOURCE" \
      --set "fdf.catalog.image=$FDF_CATALOG_IMAGE" \
      --set "fdf.cephxKeyType=$FDF_KEY_TYPE"
)
```

Import the context and set both `contextStore` Secrets before the first apply,
which freezes them:

```bash
CONTEXT="lab-sno-ceph-df"
INPUT_DIR="$HOME/bootwright-e2e/lab-sno-ceph-df"
umask 077
RUN_DIR=$(mktemp -d)
printf 'Run records: %s\n' "$RUN_DIR"
CONTROLLER_ADDRESS="$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')"
./bin/bootwright validate -f "$INPUT_DIR"
./bin/bootwright context init --name "$CONTEXT" --input-dir "$INPUT_DIR"
./bin/bootwright secret generate --context "$CONTEXT"
./bin/bootwright secret set --context "$CONTEXT" --name openshift-pull-secret --value-file "${PULL_SECRET_FILE:?Set PULL_SECRET_FILE to the private pull-secret JSON file}" --yes
install -m 0600 /dev/null "$RUN_DIR/ibm-password"
cut -d: -f2- "${IBM_REGISTRY_FILE:?Set IBM_REGISTRY_FILE to the IBM entitlement key file}" > "$RUN_DIR/ibm-password"
./bin/bootwright secret set --context "$CONTEXT" --name ibm-registry --username cp --password-file "$RUN_DIR/ibm-password" --yes
rm -f "$RUN_DIR/ibm-password"
./bin/bootwright secret check --context "$CONTEXT"
./bin/bootwright plan --context "$CONTEXT"
```

## 3. Prepare the controller and services

```sh
./bin/bootwright apply --context "$CONTEXT" --stage controller --yes
FIREWALL_PORTS=(53/tcp 53/udp 123/udp 3128/tcp 8080/tcp 8443/tcp)
```

Complete [Prepare listener firewall](../README.md#prepare-listener-firewall)
in this shell, then require controller preflight to report `ready`:

```sh
./bin/bootwright preflight controller --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --stage infrastructure-services,substrates --yes
```

The services answer on `$CONTROLLER_ADDRESS`:

```sh
curl -sk "https://$CONTROLLER_ADDRESS:8443/" -o /dev/null -w '%{http_code}\n'   # 404 on an empty root
dig @"$CONTROLLER_ADDRESS" +short api.sno.e2e.example.test test.apps.sno.e2e.example.test   # both 198.51.100.31
dig @"$CONTROLLER_ADDRESS" +tcp +short rhel-01.e2e.example.test
chronyd -Q -t 10 "server $CONTROLLER_ADDRESS iburst port 123"
```

Create this run's resolver drop-in so the controller resolves the cluster
names. The initial check must succeed so an existing file is never overwritten:

```sh
(
    set -e
    test ! -e /etc/systemd/resolved.conf.d/bootwright-e2e.conf
    test ! -L /etc/systemd/resolved.conf.d/bootwright-e2e.conf
    sudo install -d -m 0755 /etc/systemd/resolved.conf.d
    printf '[Resolve]\nDNS=%s\nDomains=~e2e.example.test\n' "$CONTROLLER_ADDRESS" | sudo tee /etc/systemd/resolved.conf.d/bootwright-e2e.conf
    sudo systemctl restart systemd-resolved
)
```

## 4. Create the guests and install both clusters

Create the four guests, install RHEL on the Ceph guests and check them:

```sh
./bin/bootwright apply --context "$CONTEXT" --stage machines --yes
./bin/bootwright machine list --context "$CONTEXT" --power-status
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-01,rhel-02,rhel-03 -- lsblk -b -o NAME,TYPE,SIZE,SERIAL,ROTA,FSTYPE,MOUNTPOINTS
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-01,rhel-02,rhel-03 -- getent hosts cp.icr.io public.dhe.ibm.com
./bin/bootwright preflight storage-cluster --context "$CONTEXT"
./bin/bootwright preflight container-cluster --context "$CONTEXT"
```

Every `resolution.*` and `capacity.*` check must be `passed`. Each OSD disk is
nonrotational (`ROTA=0`) at `/dev/sda` with the root at `/dev/vda`, and the IBM
hosts resolve on every guest. The OpenShift guest `sno-01` is powered and has
no operating system yet: the agent ISO installs it in the next stage.

Realize both clusters. The stage installs Ceph and the Ceph resources, the
consumer's principals among them, and the OpenShift installation; allow over an
hour for the image build, boot and installation waits, and record each phase's
duration as the progress lines print it (`./bin/bootwright status --detail full
--context "$CONTEXT"` repeats them). The apply names a private operation-log
directory before effects start; keep it as evidence:

```sh
./bin/bootwright plan --context "$CONTEXT" --stage clusters
./bin/bootwright apply --context "$CONTEXT" --stage clusters --yes
./bin/bootwright status --context "$CONTEXT"
```

Then install Data Foundation. The apply waits for the `odf-operator` InstallPlan
and approves exactly one, waits for the CSV at `startingCSV`, places the
consumer's details and creates the StorageCluster; allow up to `fdf.timeout`:

```sh
./bin/bootwright plan --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --yes
./bin/bootwright status --context "$CONTEXT"
```

If an apply is interrupted or stops, read `status`, fix the cause and repeat
the same command; the apply continues its operation and creates nothing twice.
This case defines no deliberate interruption drills: B761's belong to the
compact profile.

## 5. Verify the Ceph side

Take the administrator paths from `cluster info`, which prints them in
`cephCommand`, and use only files beneath the cluster's directory in
`/var/lib/bootwright/ceph/`. Never print or save raw keys:

```sh
./bin/bootwright cluster info --context "$CONTEXT" --name ceph-e2e
CEPH_CONF='' # Configuration path printed by cluster info.
CEPH_KEYRING='' # Administrator keyring path printed by cluster info.
: "${CEPH_CONF:?Set CEPH_CONF from cluster info}"
: "${CEPH_KEYRING:?Set CEPH_KEYRING from cluster info}"
ceph_admin() {
    ./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- \
        sudo -n ceph --conf "${CEPH_CONF:?}" --keyring "${CEPH_KEYRING:?}" "$@"
}
ceph_admin -s
ceph_admin pg stat
ceph_admin orch ps --refresh
ceph_admin osd pool ls detail
ceph_admin osd pool application get rbd-data
ceph_admin fs status files
ceph_admin fs subvolumegroup info files csi
ceph_admin mgr services
ceph_admin mon dump | grep -i cipher
```

Require:

- `ceph -s` is `HEALTH_OK`, or only `CHA_ERROR_SENDING_REPORT` while Call Home
  cannot reach IBM, with 3 mons, an active and a standby mgr and 3 OSDs up and
  in; `pg stat` all `active+clean`.
- `orch ps` shows the prometheus daemon on `rhel-03` and no `rgw` daemon;
  `mgr services` lists a `prometheus` endpoint.
- `rbd-data` is replicated `size=2`, `min_size=2` on rule `ssd-hosts` with the
  `rbd` application; `cephfs-metadata` and `cephfs-data` carry `cephfs`.
- Rank 0 of `files` is `active` with one standby (two MDS); the group `csi`
  exists with mode `750`.
- The cipher fields of the MON map read `aes256k` only; record them.

The consumer's principals exist, and only their names are printed. The script
filters on the guest, so no key leaves it:

```sh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n bash -s -- "${CEPH_CONF:?}" "${CEPH_KEYRING:?}" <<'CHECK'
set -euo pipefail
ceph --conf "$1" --keyring "$2" auth ls --format json | python3 -c '
import json, sys
names = sorted(e["entity"] for e in json.load(sys.stdin)["auth_dump"] if e["entity"].startswith("client.bw-"))
print("\n".join(names))
print("consumer principals:", len(names))
sys.exit(0 if len(names) == 5 else 1)'
CHECK
```

Require exit 0 and five `client.bw-<digest>-<suffix>` names sharing one
digest, the consumer's health, RBD node, RBD provisioner, CephFS node and CephFS
provisioner principals. There is no RGW user, because the consumer names no
gateway.

## 6. Verify OpenShift and Data Foundation

Export a private kubeconfig and use `oc`, the independent client:

```sh
umask 077
KUBECONFIG="$RUN_DIR/kubeconfig"
export KUBECONFIG
./bin/bootwright cluster kubeconfig --context "$CONTEXT" --name sno > "$KUBECONFIG"
oc version; oc get nodes; oc get clusterversion; oc get co
oc debug node/master-0 -- chroot /host uname -r
```

Require one Ready node and the release equal to `openshift.version`. Record the
node's kernel. Then read the operator, the plan and the StorageCluster:

```sh
FDF_NAMESPACE="openshift-storage"
oc get packagemanifest odf-operator -n openshift-marketplace -o jsonpath='{range .status.channels[*]}{.name}{"\t"}{.currentCSV}{"\n"}{end}'
oc get catalogsource -n openshift-marketplace
oc get subscription,csv,installplan -n "$FDF_NAMESPACE"
oc get installplan -n "$FDF_NAMESPACE" -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.approval}{"\t"}{.spec.approved}{"\t"}{.spec.clusterServiceVersionNames}{"\n"}{end}'
oc get storagecluster ocs-external-storagecluster -n "$FDF_NAMESPACE" -o jsonpath='{.status.phase}{"\n"}{.status.conditions[?(@.type=="ReconcileComplete")].status}{"\n"}'
oc get storageclass
oc get noobaa noobaa -n "$FDF_NAMESPACE" -o jsonpath='{.status.phase}{"\n"}'
oc get pods -n "$FDF_NAMESPACE"
```

Require:

| Object | Required result |
| --- | --- |
| CSV | the `odf-operator` CSV equals `fdf.subscription.startingCSV` and is `Succeeded` |
| InstallPlan | exactly one InstallPlan approved by Bootwright, whose CSV list is `startingCSV` alone; odf-operator approves its own later plans, so record any others and their CSVs |
| Catalog | with `ibm`, the CatalogSource `isf-data-foundation-catalog` is `READY` in `openshift-marketplace`; with `redhat-operators`, none is owned |
| StorageCluster | `ocs-external-storagecluster` phase `Ready` and `ReconcileComplete` `True` |
| StorageClasses | `ocs-external-storagecluster-ceph-rbd`, `ocs-external-storagecluster-cephfs` and `openshift-storage.noobaa.io`; no `-ceph-rgw` class, since no gateway is declared |
| NooBaa | `noobaa` phase `Ready` |
| Pods | none Pending; the NooBaa pods run on the 32 GiB node |

Operator-run consumer acceptance. These are `oc` steps in a namespace
Bootwright does not own, never Bootwright effects. Each claim must bind and each
pod must write a file and read it back; a pod stuck in `ContainerCreating`
with a map or mount error is the failure described under [Key types](#key-types):
record `oc describe pod`, the node kernel and the CSI plugin logs before
cleanup.

```sh
oc create namespace fdf-canary
for class in ocs-external-storagecluster-ceph-rbd ocs-external-storagecluster-cephfs; do
    case "$class" in
        *rbd) name=rbd-canary; mode=ReadWriteOnce ;;
        *) name=cephfs-canary; mode=ReadWriteMany ;;
    esac
    oc apply -n fdf-canary -f - <<MANIFEST
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: $name
spec:
  accessModes:
    - $mode
  storageClassName: $class
  resources:
    requests:
      storage: 1Gi
---
apiVersion: v1
kind: Pod
metadata:
  name: $name
spec:
  containers:
    - name: canary
      image: registry.access.redhat.com/ubi9/ubi-minimal
      command: ["sh", "-c", "echo bootwright > /data/canary && cat /data/canary && sleep 3600"]
      volumeMounts:
        - name: data
          mountPath: /data
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: $name
MANIFEST
done
oc -n fdf-canary wait pvc/rbd-canary pvc/cephfs-canary --for=jsonpath='{.status.phase}'=Bound --timeout=300s
oc -n fdf-canary wait pod/rbd-canary pod/cephfs-canary --for=condition=Ready --timeout=300s
oc -n fdf-canary logs rbd-canary
oc -n fdf-canary logs cephfs-canary
oc -n fdf-canary exec rbd-canary -- cat /data/canary
oc -n fdf-canary exec cephfs-canary -- cat /data/canary
```

Both logs and reads print `bootwright`. Then a snapshot of each claim, through
the VolumeSnapshotClasses ocs-operator builds from the consumer's details. Each
class must name the details' provisioner Secret, and each snapshot must become
ready ([B1128](../../specs/milestones/m4.md#b1128)):

```sh
oc get volumesnapshotclass -o custom-columns='NAME:.metadata.name,SECRET:.parameters.csi\.storage\.k8s\.io/snapshotter-secret-name'
oc get secret -n "$FDF_NAMESPACE" -o name | grep -E 'rook-csi-(rbd|cephfs)-provisioner'
for claim in rbd-canary:ocs-external-storagecluster-rbdplugin-snapclass cephfs-canary:ocs-external-storagecluster-cephfsplugin-snapclass; do
    oc apply -n fdf-canary -f - <<MANIFEST
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: ${claim%%:*}-snap
spec:
  volumeSnapshotClassName: ${claim#*:}
  source:
    persistentVolumeClaimName: ${claim%%:*}
MANIFEST
done
oc -n fdf-canary wait volumesnapshot/rbd-canary-snap volumesnapshot/cephfs-canary-snap --for=jsonpath='{.status.readyToUse}'=true --timeout=300s
```

Each class's secret is one of the listed `rook-csi-*-provisioner-*` Secrets of
`openshift-storage`. Then an ObjectBucketClaim on the
NooBaa class, with an S3 put and get through NooBaa. The route's certificate is
the lab router's, so the check skips verification and states that. The
credentials stay in the shell's environment and are never printed:

```bash
oc apply -n fdf-canary -f - <<'MANIFEST'
apiVersion: objectbucket.io/v1alpha1
kind: ObjectBucketClaim
metadata:
  name: fdf-bucket
spec:
  generateBucketName: fdf-bucket
  storageClassName: openshift-storage.noobaa.io
MANIFEST
oc -n fdf-canary wait obc/fdf-bucket --for=jsonpath='{.status.phase}'=Bound --timeout=300s
(
    set -e
    umask 077
    OBJECT_DIR=$(mktemp -d)
    trap 'rm -rf -- "$OBJECT_DIR"' EXIT
    BUCKET=$(oc -n fdf-canary get configmap fdf-bucket -o jsonpath='{.data.BUCKET_NAME}')
    AWS_ACCESS_KEY_ID=$(oc -n fdf-canary get secret fdf-bucket -o jsonpath='{.data.AWS_ACCESS_KEY_ID}' | base64 -d)
    AWS_SECRET_ACCESS_KEY=$(oc -n fdf-canary get secret fdf-bucket -o jsonpath='{.data.AWS_SECRET_ACCESS_KEY}' | base64 -d)
    export AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY
    S3_HOST=$(oc -n openshift-storage get route s3 -o jsonpath='{.spec.host}')
    head -c 1048576 /dev/urandom > "$OBJECT_DIR/in"
    aws --no-verify-ssl --endpoint-url "https://$S3_HOST" s3api put-object --bucket "$BUCKET" --key canary --body "$OBJECT_DIR/in"
    aws --no-verify-ssl --endpoint-url "https://$S3_HOST" s3api get-object --bucket "$BUCKET" --key canary "$OBJECT_DIR/out"
    cmp "$OBJECT_DIR/in" "$OBJECT_DIR/out"
    aws --no-verify-ssl --endpoint-url "https://$S3_HOST" s3api delete-object --bucket "$BUCKET" --key canary
)
```

Remove the canaries, which releases their volumes and bucket, before settling
the state:

```sh
oc delete namespace fdf-canary
```

Finally, from the original shell, the state settles without work and shows no
drift:

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright machine list --context "$CONTEXT" --power-status
./bin/bootwright cluster list --context "$CONTEXT"
./bin/bootwright cluster info --context "$CONTEXT" --name sno
./bin/bootwright apply --context "$CONTEXT" --yes
./bin/bootwright diff --context "$CONTEXT" --observe
```

The repeated `apply` must report every block unchanged and contact nothing,
and `diff --observe` must report no drift. Cephadm keeps an offline host's
daemons in its cache for up to ten minutes; if `diff` reports a Ceph resource
partially realized, wait until `orch ps --refresh` lists every daemon running
and repeat it.

## 7. Destroy

Destroy the verified installation. With the guests running, destroy pauses
before removal and names the stop; the repeated destroy continues the same
operation. The consumer retires with its cluster, so this destroy removes the
Data Foundation instance, the consumer's principals and both clusters:

```sh
./bin/bootwright plan --context "$CONTEXT"
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright machine stop --context "$CONTEXT" --name sno-01,rhel-01,rhel-02,rhel-03 --yes
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright status --context "$CONTEXT"
virsh --connect qemu:///system list --all
virsh --connect qemu:///system net-list --all
```

Require the refusal or pause to name the guests, and no domain, disk, network,
installer work area or managed service of the context to remain. Unset the
kubeconfig, which no longer reaches a cluster:

```sh
unset KUBECONFIG
```

## 8. Preserve evidence and clean up

Preserve the operation logs and final status before deleting the context:

```sh
EVIDENCE_CONTEXT="$CONTEXT"
```

Complete [Preserve evidence before context purge](../README.md#preserve-evidence-before-context-purge).
Require its archive integrity and operation-log digest checks to pass, then
delete the context:

```sh
./bin/bootwright context delete --name "$CONTEXT" --purge --yes
```

Complete [Restore listener firewall](../README.md#restore-listener-firewall).
Then remove the resolver drop-in created in step 3:

```sh
sudo rm -- /etc/systemd/resolved.conf.d/bootwright-e2e.conf
sudo systemctl restart systemd-resolved
```

Complete [Remove diagnostic tools](../README.md#remove-diagnostic-tools). Only
after both cleanup procedures succeed, remove this run's private files:

```sh
rm -rf -- "${RUN_DIR:?Keep RUN_DIR until cleanup succeeds}" "${INPUT_DIR:?Keep INPUT_DIR until destroy completes}"
```

Shared media and the controller installation remain for later cases.

## 9. Record the result

Record the build commit, the OpenShift release, the Ceph release, the catalog
source (`redhat-operators` or the IBM catalog image), the channel, `startingCSV`
and the CSVs odf-operator approved after it, the node kernel, the MON cipher
fields, the phase durations and the outcome of every check in step 6 in the
[acceptance ledger](../../docs/acceptance.md) as an observation, not an
acceptance: this case accepts neither B85 nor B761. A run is successful only
when all steps pass from preparation through cleanup; report a failed PVC mount
under `aes256k` as the finding it is.
