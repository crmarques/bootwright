# Synthetic multi-datacenter platform

This directory is a non-operational, synthetic desired-state example. It
preserves the resource kinds, field shapes, and reference patterns of a larger
multi-datacenter platform while using fictional identities and documentation
addresses throughout.

All domains use `example.com`. IPv4 infrastructure addresses use the
documentation blocks `192.0.2.0/24`, `198.51.100.0/24`, and
`203.0.113.0/24`. MAC addresses are locally administered examples, software
versions include deliberately fictional coordinates, and digest values are
non-secret sentinels. The topology, sizing, host names, site names, VLANs,
device paths, endpoints, and service policy are illustrative rather than a
record of a deployed environment.

Secret descriptors live under `secret-descriptors/`, but their referenced
payloads are intentionally absent from the reserved `secrets/` directory.
Never add real credentials, keys, certificates, pull secrets, entitlement
tokens, or other private material to this example.

The repository does not yet contain the M1 compiler, so only syntax checks are
available. Once validation is implemented, supply disposable test secret
payloads outside Git and validate the directory as a whole:

```text
bootwright validate -f examples/multidc-platform
```
