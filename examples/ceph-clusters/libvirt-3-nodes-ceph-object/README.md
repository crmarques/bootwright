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

The gateway also signs S3 users in through the Active Directory
[`LDAPServer`](../../../specs/api/infrastructure-services.md#oidcprovider-and-ldapserver)
`lab-directory` ([authentication](../../../specs/ceph-resources.md#cephobjectgateway-field-record)):
RGW binds as `CN=svc ceph` with the password stored in the context Secret
`lab-directory-bind` and verifies `dc-01.lab.example.test` against the image's
system trust store. Local CephObjectUser keys keep working while the directory
is down, though requests can wait out the 3-second LDAP timeouts first. Keep
directory names disjoint from every RGW user ID, `dashboard` included.

Its RGW also trusts web identities through STS: the key comes from the context
Secret `rgw-objects-sts-key`, and the gateway lists the Keycloak
[`OIDCProvider`](../../../specs/api/infrastructure-services.md#oidcprovider-and-ldapserver)
`lab-sso`, whose issuer chains to a public CA. Bootwright registers neither the
provider nor any role: after handoff, register both through RGW's IAM API under
an account ([STS](../../../specs/ceph-resources.md#cephobjectgateway-authentication)).

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
- Required inputs: RHEL 9.8 boot and DVD ISOs from your Red Hat subscription,
  the IBM entitlement key for Secret `ibm-registry` and the directory bind
  password for Secret `lab-directory-bind`, stored with `bootwright secret set
  --name lab-directory-bind --value-stdin`, and exactly 16 letters and digits
  for Secret `rgw-objects-sts-key`, stored the same way without a final
  newline. Point `lab-directory` and `lab-sso` at your own services, or remove
  `spec.authentication` from the gateway and the `LDAPServer` and
  `OIDCProvider` with their Secrets. The Entitlement
  `license.accept: true` is a placeholder; review IBM's terms before use.
- Release coordinates are the three-node example's authored choices.

Run it with the e2e [`lab-3-nodes-ceph`](../../../e2e/lab-3-nodes-ceph/runbook.md) case.
