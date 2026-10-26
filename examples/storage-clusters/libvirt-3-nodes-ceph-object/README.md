# Three-node Ceph object gateway on libvirt and sushy

This example declares three managed RHEL guests and an IBM Storage Ceph cluster with one object gateway: the native `rgw` service `rgw.objects` of two daemons that terminate TLS themselves on port 443, and the StorageObjectGateway `objects` that binds it to its own realm, zone group and zone, all named `objects`, with the endpoint `s3.lab.example.test`. Addresses and names are synthetic. Copy the input to an operator-owned directory before adjusting it; keep real inputs and credentials outside tracked files.

The gateway declares no pools, so apply creates the eight derived zone pools `objects.rgw.meta`, `.control`, `.dedup`, `.log`, `.otp`, `.buckets.index`, `.buckets.data` and `.buckets.non-ec` at the cluster's 2/2 defaults before the gateway block runs. The gateway block then claims the shared `.rgw.root`, creates the realm, zone group and zone without `--default`, binds the zone's `default-placement` to the frozen pools, commits the period and only then applies `rgw.objects`. The service binds `/spec/ssl_cert` and `/spec/ssl_key` to the generated `tlsCertificate` Secret `rgw-objects-tls`, whose only DNS name is the endpoint; before a fresh apply registers, Bootwright refuses a bound certificate that does not name it. The certificate reaches the daemons only through the gateway block, never a request, plan or log, and readiness connects to each daemon with the endpoint as the TLS server name and requires the served certificate to be the bound one. The example declares no ingress and no object user: an ingress plans only once its haproxy and keepalived images are authored under `spec.ceph.images`, which this example does not do, and S3 users and buckets are not realized yet.

## Budget and setup

The budget, host prerequisites, `setup`, `preflight controller`, media imports and entitlement are the [three-node example's](../libvirt-3-nodes-ceph/README.md#initial-setup); this example authors the same release coordinates, guests and disks. Review [cluster.yaml](storage/cluster.yaml), which owns the native MON, MGR, OSD and `rgw.objects` services and the TLS binding, [object-gateway.yaml](storage/object-gateway.yaml), which owns the gateway and its endpoint, and [rgw-objects-tls.yaml](secret-descriptors/rgw-objects-tls.yaml), the generated certificate. To serve a certificate from your own authority instead, replace its `source` and store it with `secret set --name rgw-objects-tls --certificate-file <path> --private-key-file <path>`; it must name the endpoint.

The tracked `storage/entitlement.yaml` sets `license.accept: true` as a placeholder. Accepting the IBM license is your explicit act: review its terms before you use your copy.

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

Require `HEALTH_OK`, or only the tolerated `CHA_ERROR_SENDING_REPORT` warning, two running `rgw.objects` daemons and the eight `objects.rgw.*` pools with the `rgw` application beside `.rgw.root`.

The endpoint name is not published to DNS. Save the bound certificate, then, from the controller, request each daemon's host through the endpoint name and require an HTTP status line; `--connect-to` keeps `s3.lab.example.test` as the TLS server name and the name the certificate is checked against. The addresses are this example's `rhel-01` and `rhel-02`; use the hosts `orch ps` printed:

```sh
./bin/bootwright secret show --name rgw-objects-tls --part certificate > rgw-objects.pem
curl -sS -i --cacert rgw-objects.pem --connect-to s3.lab.example.test:443:198.51.100.11:443 https://s3.lab.example.test/
curl -sS -i --cacert rgw-objects.pem --connect-to s3.lab.example.test:443:198.51.100.12:443 https://s3.lab.example.test/
curl -sS -i --cacert rgw-objects.pem https://198.51.100.11/
```

The last request must fail the certificate's name check: the certificate names only the endpoint. Bootwright's readiness proves the same served certificate and status line from the bootstrap host; S3 requests with object users and buckets are the operator's acceptance, not Bootwright's.

Then stop one daemon, written here as `<rgwDaemon>` from the `orch ps` output, require the other host to keep answering through the endpoint name, and start it again. Stop the daemon, not its guest: with 2/2 pools a stopped guest pauses I/O on every placement group with a replica there.

```text
<cephCommand> orch daemon stop <rgwDaemon>
<cephCommand> orch ps --daemon_type rgw --refresh
<cephCommand> orch daemon start <rgwDaemon>
```

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
