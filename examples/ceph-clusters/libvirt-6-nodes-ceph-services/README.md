# Six-host Ceph services qualification (P2)

This synthetic profile exercises CephFS, S3, NFS and SMB. All operator runs
below are **unrun**; a passing offline plan is not live qualification. M2c
remains open until its owner accepts the applicable ledger rows.

## Budget and prerequisites

Seven guests declare 26 vCPU, 83 GiB RAM, 400 GiB root and 360 GiB SSD data.
Storage hosts have four CPUs, 60 GiB root and one 60 GiB SSD each. RAM is
16/16/11/12/12/12 GiB for rhel-01 through rhel-06; the client has two CPUs,
4 GiB RAM and 40 GiB root. Reserve another 4 GiB for the controller.
IBM base per-guest service budgets are h1/h2 14 GiB, h3 9 GiB, h4/h5 at least
5 GiB and h6 6 GiB; the declared headroom includes ingress and SMB, whose
actual peaks must be measured. OSD memory targets are 4 GiB. Never reduce
replication or RAM to make a qualification run fit.

Provide licensed RHEL 9.8 DVD media and IBM registry entitlement. Replace the
four synthetic auxiliary image coordinates in `storage/cluster.yaml` with
operator-selected authenticated images of the chosen release; record their
resolved digests. Placeholder images are offline fixtures, not pullable images.
Qualify clustered SMB and all gateway features against those exact images.
Configure guest firewalls for the declared listeners and VRRP before applying.
Use independently allocated VIPs, addresses and DNS in the adjusted input.

The declared `client` Machine is a Bootwright-installed Machine that is a node
of no cluster, so the storage list retains it and this context installs it
beside the six hosts. To use an operator-owned RHEL client instead, remove the
`client` Machine. A context created under the earlier guidance already
realizes the client Machine, so remove the client Machine from one of the two
inputs before applying. Adapt the NFS allowed-client entry to the client's address.
Install `ceph-common`, `nfs-utils`, `cifs-utils`, `samba-client`, `curl` and an
S3 client there.

## Initial setup

Follow the [base setup](../libvirt-3-nodes-ceph/README.md#initial-setup), copy
this directory to a private input location, and adjust synthetic addresses,
media, image coordinates and controller allocation before freezing it.
Keep captures and credentials in an ignored, private directory, never Git.

## IBM entitlement

Follow the [IBM entitlement](../libvirt-3-nodes-ceph/README.md#ibm-entitlement)
procedure. Bind `ibm-registry` through standard input; never put credentials
in YAML, process arguments or evidence. The SMB password is also contextStore
material; RGW generates its S3 pair under produced-material custody.

The tracked `storage/entitlement.yaml` sets `license.accept: true` as a placeholder. Accepting the IBM license is your explicit act: review its terms before you use your copy.

## Create the context

The unchanged synthetic input has 45 files seen and 45 objects decoded. The acceptance log must retain the
adjusted counts too.

```sh
./bin/bootwright validate -f /path/to/adjusted/libvirt-6-nodes-ceph-services
./bin/bootwright context init --name ceph-services --input-dir /path/to/adjusted/libvirt-6-nodes-ceph-services
./bin/bootwright context update --name ceph-services --input-dir /path/to/adjusted/libvirt-6-nodes-ceph-services
./bin/bootwright context use --name ceph-services
./bin/bootwright secret generate
./bin/bootwright secret set --name ibm-registry --username cp --password-stdin --yes
./bin/bootwright secret set --name files-smb-user --value-stdin --yes
./bin/bootwright secret check
./bin/bootwright render effective
```

## Apply by stage

```sh
./bin/bootwright plan
./bin/bootwright apply --stage controller
./bin/bootwright preflight controller --context ceph-services
./bin/bootwright apply --stage infrastructure-services,substrates
./bin/bootwright apply --stage machines
./bin/bootwright plan --stage clusters
```

Before clusters, inspect every guest's `free -m`, `lsblk -o NAME,SIZE,ROTA,SERIAL`
and `ip -br address` read-only. Require exactly the owned root and data devices,
SSD presentation and correct addresses; prove the client separately.
Start the memory sampler below, then run:

```sh
./bin/bootwright apply --stage clusters
./bin/bootwright status
```

## Validate the cluster

```sh
./bin/bootwright cluster info --name ceph-lab
```

Use the returned admin node and Ceph command as `<cephCommand>`. Record these
read-only results, with resource IDs and key fields redacted where applicable:

```text
<cephCommand> -s
<cephCommand> health detail
<cephCommand> orch ls
<cephCommand> orch ps
<cephCommand> osd tree
<cephCommand> osd pool ls detail
<cephCommand> fs status files
<cephCommand> fs subvolumegroup ls files
<cephCommand> fs subvolume ls files --group_name apps
<cephCommand> fs subvolume ls files --group_name ec
<cephCommand> nfs cluster ls
<cephCommand> nfs export ls shares
<cephCommand> smb cluster ls
<cephCommand> smb share ls files
```

Require MONs h1-h3, MGRs h1/h2, six SSD OSDs, one active MDS plus a replay
standby on h1/h2, RGW h3/h6, two active NFS daemons among h4-h6, ingress h4/h5
and two clustered SMB daemons h4/h5. Prove the filesystem's two data pool layouts, both SMB shares' distinct backing
subvolumes, and `/docs` and `/objects` NFS exports. Require healthy data and
placement; record any tolerated IBM Call Home warning explicitly. Never capture
`radosgw-admin user info`, NFS export details or secret-bearing native exports.

## Client access

Run all consumer I/O on the independent client. Use fresh unique names under
only the owned canary subvolume, bucket, exports and shares. Do not overwrite
existing names. Record SHA-256 before/after and delete only the created objects.
Bound every mount and I/O probe to 120 seconds and retain exit status.

For CephFS export the `cephfs-canary` scoped keyring through the supported
`cluster keyring` workflow in the [access guide](../../../specs/cli.md#client-keyring-export),
into a private mode-0600 file. Mount only its subvolume path using that identity;
write and read 1 MiB as uid/gid 2000 and require a mount outside its scope to
fail. Never use the admin keyring for consumer I/O.

On the controller, export to a private file and transfer it to the independent
client over its operator-verified channel. Keep raw output out of captures:

```sh
umask 077
./bin/bootwright cluster keyring --name ceph-lab --client cephfs-canary > "$ev/cephfs-canary.keyring"
```

On the client, extract the key to a mode-0600 `secretfile`, discover the canary
path with read-only `fs subvolume getpath files cephfs-canary --group_name ec`,
and mount that exact path with `name=cephfs-canary,secretfile=<private-file>`.
Do not pass the key value itself in a mount option or command argument.

For S3, use the private `cluster info --object-user` handoff. It reveals
only the named user's proved credentials whose original applied user, cluster and gateway scope
still match the selected graph; missing, changed or unproved scope refuses.
On the controller, keep the full JSON outside acceptance captures:

```sh
umask 077
set -eu
s3_private=$(mktemp -d)
./bin/bootwright cluster info --name ceph-lab --object-user services-canary --output json > "$s3_private/access.json"
jq -er '[.result.storage[] | select(.name == "ceph-lab") | .objectUsers[] | select(.name == "services-canary")] | if length == 1 then .[0] | "[canary]\naws_access_key_id = \(.accessKey)\naws_secret_access_key = \(.secretKey)" else error("expected one canary user") end' "$s3_private/access.json" > "$s3_private/credentials"
rm -- "$s3_private/access.json"
```

Transfer only the mode-0600 credentials file and the bound gateway certificate
(or its verified issuing CA) to the independent client over its verified private
channel. Remove the controller copy after verified transfer. Do not use native
admin credential retrieval. The operator must qualify and provision `aws` CLI,
`jq`, `timeout` and checksum tools for their RHEL client; Bootwright does not
install these client tools. Disable shell tracing and CLI debug output.

On that client, set `s3_private` to the protected transferred directory and
`AWS_CA_BUNDLE` to the verified gateway trust file. DNS must resolve
`s3.lab.example.test` to the adjusted ingress VIP (.50 in this synthetic input).
The following bounded probes use only a fresh unique key in the owned bucket;
credentials are read from the private file, never passed as arguments:

```sh
(
set -eu
umask 077
unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_SECURITY_TOKEN
export AWS_SHARED_CREDENTIALS_FILE="$s3_private/credentials"
export AWS_CONFIG_FILE="$s3_private/config"
export AWS_CA_BUNDLE="$s3_private/gateway-ca.pem"
export AWS_PROFILE=canary AWS_EC2_METADATA_DISABLED=true AWS_PAGER=
printf '[profile canary]\nregion = us-east-1\ns3 =\n    addressing_style = path\n' > "$AWS_CONFIG_FILE"
key="qualification-$(cat /proc/sys/kernel/random/uuid)"
printf '%s\n' "$key" > "$s3_private/owned-key"
head -c 1048576 /dev/urandom > "$s3_private/source"
timeout 120 aws --endpoint-url https://s3.lab.example.test s3api put-object --bucket services-canary-bucket --key "$key" --body "$s3_private/source" > "$s3_private/put.json"
timeout 120 aws --endpoint-url https://s3.lab.example.test s3api get-object --bucket services-canary-bucket --key "$key" "$s3_private/readback" > "$s3_private/get.json"
sha256sum "$s3_private/source" "$s3_private/readback"
cmp "$s3_private/source" "$s3_private/readback"
timeout 120 aws --endpoint-url https://s3.lab.example.test s3api delete-object --bucket services-canary-bucket --key "$key" > "$s3_private/delete.json"
)
```

Retain only exit statuses and matching checksums as acceptance evidence. If a
probe fails, retain `owned-key` privately and delete only that exact object after
restoring access; do not delete the bucket or other keys. Remove private client
files after successful cleanup. Also require a wrong-name TLS check and access
to an operator-designated existing foreign bucket to fail; only an authorization
refusal proves denied scope, while timeouts are inconclusive. Never disable TLS
verification. These operator checks remain unrun.

For NFS mount VIP .51 `/docs` using NFSv4/TCP. Write/read/delete as
`setpriv --reuid=2000 --regid=2000 --clear-groups`, because root is squashed.
Repeat on `/objects` using an owned bucket path and the export's supported
identity semantics. Require the explicitly denied rhel-03 client to fail.
Only treat a permission refusal as proof of denial; a timeout is inconclusive.

For example, on the independent client, use a fresh mountpoint and substitute
the adjusted VIP. Keep the mountpoint until all owned checks finish:

```sh
mount_dir=$(mktemp -d)
timeout 120 sudo mount -t nfs4 -o proto=tcp 198.51.100.51:/docs "$mount_dir"
```

After the owned I/O below, run `sudo umount "$mount_dir"` and
`rmdir "$mount_dir"`. Repeat with `:/objects`, selecting only the owned bucket
inside the mounted object export. Run the denied mount from rhel-03 through
`cluster exec --name ceph-lab --node rhel-03`, with its own fresh mountpoint.

For each CephFS or NFS mounted path, run the following as uid/gid 2000
through a 120-second timeout, passing that mount as the first argument. The
fresh directory prevents overwriting existing data; cleanup removes only its
one owned file and directory. Record the two matching hashes.

```sh
set -eu
owned_dir=$(mktemp -d "$1/qualification.XXXXXXXX")
trap 'rm -f -- "$owned_dir/probe"; rmdir -- "$owned_dir"' EXIT
head -c 1048576 /dev/urandom > "$owned_dir/probe"
sha256sum "$owned_dir/probe"
sync "$owned_dir/probe"
sha256sum "$owned_dir/probe"
```

For SMB use a mode-0600 credentials file holding `smbuser` and its bound
password; test `documents` and `archive` separately through both running
clustered daemons. Create/read/delete unique files, then require a wrong user
to fail. Record native clustered state and reconnection results. Unmount every
consumer and remove private credential files after the checks. The NFS and SMB
shares deliberately never share a subvolume.

## Failover

Keep a bounded canary reader running and stop RGW host h3:

```sh
./bin/bootwright machine stop --name rhel-03
```

Within 300 seconds record health, surviving RGW h6 and HTTPS/S3 through the VIP.
Recover with `machine start --name rhel-03` and wait for healthy placement.
Then stop rhel-04 to exercise NFS candidate replacement, ingress VIP movement
and SMB clustering. Record each protocol independently within 300 seconds;
NFS may require explicit unmount/remount after backend failure. Do not claim
seamless failover when remount was needed. Restart rhel-04 and prove recovery.
Finally stop the active MDS host, prove the replay standby takes over and the
scoped CephFS reader recovers, restart that host and restore healthy placement.

## Interruption and continuation

On a fresh run interrupt `apply --stage clusters` once with Ctrl-C after the
first Ceph block starts. Run `status`, repeat the identical apply command,
and prove the same operation and frozen plan continued. Record operation IDs
and evidence digests without secret values; an interruption is not success.

## Replay

Run `apply` on the completed context and require it to settle without effects.
Record the unchanged operation/resource identities and healthy client checks.

## Check that changed intent is refused

Change one subvolume quota in the private input and validate it. Run
`context update` and retain its completed-apply warning; the update itself is
allowed. Then run `apply` and require changed intent to be refused before any
native effects or writes to the live cluster.
Restore the exact frozen input and run `context update` again before
continuation or retirement. Do not interpret refusal as support for in-place reconfiguration.

## Measured budget

Record each [Ceph matrix row](../../../docs/acceptance.md#ceph-matrix-row)
field, including build/release and artifact digests, effective/plan digests,
prerequisites, per-feature result, health/placement, client checks, interruption,
continuation, failover/recovery, cleanup and operation-log digest. All are unrun.
Budget fields are: Guests (7), Declared vCPU (26), Declared RAM GiB (83),
Declared disk GiB (760), MemAvailable at (a)/(b)/(c), Minimum MemAvailable during clusters stage, Allocated vs apparent disk bytes, Per-guest balloon RSS,
Daemons by type, Clusters-stage wall time, OOM kills. Include the independent
client's allocation and measurements and the 4 GiB controller reserve.

At (a) before apply, (b) after clusters and (c) after clients/failover run:

```text
nproc
grep MemAvailable /proc/meminfo
sudo du -sb --apparent-size /var/lib/libvirt/images/bootwright/ceph-services
sudo du -sb /var/lib/libvirt/images/bootwright/ceph-services
virsh --connect qemu:///system domstats --balloon --vcpu
<cephCommand> orch ps --format json-pretty
<cephCommand> df
```

In a second terminal before clusters, with `ev` an existing private directory:

```sh
while sleep 10; do date +%s; grep MemAvailable /proc/meminfo; done > "$ev/memavailable-clusters.txt"
```

Stop the sampler with Ctrl-C when apply returns. Record guest/controller OOM
journals, stage elapsed time and both apparent and allocated disk totals.

## Cleanup

Unmount clients and remove only canary data first. While storage hosts run,
require destroy to refuse with `lifecycle.live`; then retire the whole cluster:

```sh
./bin/bootwright destroy --authorize data-loss
./bin/bootwright machine stop --name rhel-01,rhel-02,rhel-03,rhel-04,rhel-05,rhel-06
./bin/bootwright destroy --authorize data-loss
./bin/bootwright status
./bin/bootwright cluster info --name ceph-lab
virsh --connect qemu:///system list --all
```

Prove owned domains, disks, host reservations and produced credentials absent;
retain evidence before deleting the context. The independent client stays its
operator's responsibility. After an interruption, inspect status and repeat
destroy with the same authorization. Delete the retired context with
`context delete --name ceph-services --purge`, recreate from the same adjusted
input and repeat apply and client checks, proving new owned identities and no
adoption of old artifacts. Record whole-cluster retirement and recreate as
separate unrun acceptance results; resource-only removal is not this profile.
