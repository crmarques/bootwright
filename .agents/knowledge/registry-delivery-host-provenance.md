# Registry Delivery Host Provenance

Where the delivery hosts of the [acquisition list](../../specs/cli.md#acquisition-list)
come from. The list is the contract; this page records what each name rests on.

The delivery hosts rest on what a registry showed an anonymous client, or Red
Hat and Docker publish, not on a run of this executable:

- A token realm and a blob redirect were read for `docker.io`, `quay.io`,
  `registry.access.redhat.com` and `icr.io`.
- The `cdn.quay.io` and `cdn02.quay.io` to `cdn06.quay.io` hosts, the
  `access.redhat.com` signature store, the `registry.redhat.io` delivery hosts
  and `api.openshift.com` come from Red Hat's OpenShift firewall list.
- The default catalog names come from Red Hat's Operator documentation, which
  lists `redhat-marketplace` only for the OLMv1 catalog.
- `cp.icr.io` answers only an entitled client, so its delivery host is not
  named; the Not yet met paragraph of the list tracks it.
