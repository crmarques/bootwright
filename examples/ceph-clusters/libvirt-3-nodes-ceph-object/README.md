# Three-node Ceph object gateway on libvirt and sushy

This example declares three managed RHEL 9.8 guests and an IBM Storage Ceph
cluster with one object gateway: the native `rgw.objects` service of two
daemons that terminate TLS themselves on port 443, and the CephObjectGateway
`objects`, with its own realm, zone group and zone all named `objects` and the
endpoint `s3.lab.example.test`.

The gateway declares no pools, so apply derives eight `objects.rgw.*` zone
pools at the cluster's 2/2 defaults. The service binds `/spec/ssl_cert` and
`/spec/ssl_key` to the generated `tlsCertificate` Secret `rgw-objects-tls`,
whose only DNS name is the endpoint.

## Worth noting

- Topology: `rhel-01` to `rhel-03`, each 4 vCPU, 8 GiB RAM, 60 GiB root and
  one 80 GiB SSD data disk (the three-node example's guests). Reserve at least
  4 GiB further host memory for the controller.
- The example declares no ingress, object user or bucket: an ingress plans
  only once its haproxy and keepalived images are authored under
  `spec.images`; CephObjectUser and CephObjectBucket are
  [realized](../../../specs/ceph.md) but this example does not declare them.
- The endpoint name is not published to DNS. To use your own certificate,
  replace the Secret's `source`; it must name the endpoint.
- Required inputs: RHEL 9.8 boot and DVD ISOs from your Red Hat subscription
  and the IBM entitlement key for Secret `ibm-registry`. The Entitlement
  `license.accept: true` is a placeholder; review IBM's terms before use.
- Release coordinates are the three-node example's authored choices.

Runbook: [e2e/runbooks/libvirt-3-nodes-ceph-object.md](../../../e2e/runbooks/libvirt-3-nodes-ceph-object.md).
The templated e2e [`ceph` case](../../../e2e/README.md#case-ceph) covers the same
scenario with its own inputs.
