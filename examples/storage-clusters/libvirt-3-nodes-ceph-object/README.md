# Three-node Ceph object gateway on libvirt and sushy

This example declares three managed RHEL guests and an IBM Storage Ceph cluster with one object gateway: the native `rgw` service `rgw.objects` of two daemons answering plain HTTP on port 8080, and the StorageObjectGateway `objects` that binds it to its own realm, zone group and zone, all named `objects`. Addresses and names are synthetic. Copy the input to an operator-owned directory before adjusting it; keep real inputs and credentials outside tracked files.

The gateway declares no pools, so apply creates the eight derived zone pools `objects.rgw.meta`, `.control`, `.dedup`, `.log`, `.otp`, `.buckets.index`, `.buckets.data` and `.buckets.non-ec` at the cluster's 2/2 defaults before the gateway block runs. The gateway block then claims the shared `.rgw.root`, creates the realm, zone group and zone without `--default`, binds the zone's `default-placement` to the frozen pools, commits the period and only then applies `rgw.objects`. It declares no endpoint, TLS binding, ingress or object user: TLS and an ingress arrive with their own lane, and an rgw service with `spec.ssl: true`, a binding on it or an ingress that fronts it refuses at plan until then.

## Budget and setup

The budget, host prerequisites, `setup`, `preflight controller`, media imports and entitlement are the [three-node example's](../libvirt-3-nodes-ceph/README.md#initial-setup); this example authors the same release coordinates, guests and disks. Review [cluster.yaml](storage/cluster.yaml), which owns the native MON, MGR, OSD and `rgw.objects` services, and [object-gateway.yaml](storage/object-gateway.yaml), which owns the gateway.

## Create the context and apply

Replace `/path/to/adjusted/libvirt-3-nodes-ceph-object` with your completed copy.

```sh
./bin/bootwright validate -f /path/to/adjusted/libvirt-3-nodes-ceph-object
./bin/bootwright context init --name ceph-object --input-dir /path/to/adjusted/libvirt-3-nodes-ceph-object
./bin/bootwright context use --name ceph-object
./bin/bootwright secret generate
./bin/bootwright secret set --name ibm-registry --username cp --password-stdin --yes
./bin/bootwright secret check
./bin/bootwright render effective
./bin/bootwright plan --stage controller
./bin/bootwright apply --stage controller
./bin/bootwright preflight controller --context ceph-object
./bin/bootwright apply --stage infra-components,substrates
./bin/bootwright apply --stage machines
./bin/bootwright plan --stage clusters
./bin/bootwright apply --stage clusters
./bin/bootwright status
```

The clusters stage plans the image, host and cluster blocks of `ceph-lab`, the eight derived zone pool blocks `ceph-gateway-objects-pool-<suffix>` and the gateway block `ceph-gateway-objects`.

## Operator acceptance

Print the admin node and its Ceph command as the three-node example's [validation](../libvirt-3-nodes-ceph/README.md#validate-the-cluster) does, then run each check as that printed command followed by the arguments shown, written here as `<cephCommand>`:

```text
<cephCommand> -s
<cephCommand> orch ls rgw --refresh
<cephCommand> orch ps --daemon_type rgw --refresh
<cephCommand> osd pool ls detail
```

Require `HEALTH_OK`, or only the tolerated `CHA_ERROR_SENDING_REPORT` warning, two running `rgw.objects` daemons and the eight `objects.rgw.*` pools with the `rgw` application beside `.rgw.root`. Then request the RGW port of each host that runs a daemon, here `rhel-01`, and require an HTTP status line:

```text
./bin/bootwright cluster exec --name ceph-lab --node rhel-01 -- curl -sS -i http://rhel-01.lab.example.test:8080/
```

Bootwright's readiness proves the same status line from the bootstrap host; S3 requests with object users and buckets are the operator's acceptance, not Bootwright's.

## Cleanup

This profile retires Ceph and its gateway together with the three owned virtual hosts; removal while hosts survive belongs to [B491](../../../specs/milestones/m2.md#b491). While the hosts run, destroy refuses with `lifecycle.live`:

```sh
./bin/bootwright plan
./bin/bootwright machine stop --name rhel-01,rhel-02,rhel-03
./bin/bootwright destroy --authorize data-loss
./bin/bootwright status
./bin/bootwright cluster info --name ceph-lab
virsh --connect qemu:///system list --all
./bin/bootwright context delete --name ceph-object --purge
```

After an interrupted destroy, read status and repeat destroy with the same authorization.
