# Managed RHEL guest with a managed image mirror

Prepares the [lab-rhel](../lab-rhel/README.md) guest with every public-origin
image its managed services and emulated BMC pull served by a managed image
mirror on a second, OS-ready Machine reached over SSH. This is a synthetic
preparation profile for the
[mirror host](../../../specs/infrastructure-services.md#mirror-host): it claims
no live run, and the [controlled-network
profile](../../../specs/milestones/backlog.md#b1250) owns its acceptance.

## Worth noting

- `mirror` (`mirror.lab.example.test`, `203.0.113.20`) is an OS-ready Machine
  reached as root over SSH with a bound host key. It reaches the image origins
  directly and runs `podman` and `skopeo`; nothing else on it is managed.
- `mirror-registry` is a managed Distribution Registry on `mirror` serving
  HTTPS on `5000`. The Environment names it in `spec.mirror.registry` with
  `endpointRef: ip`, so the proxy, resolver and time service on the controller
  and the emulated BMC pull, for example,
  `203.0.113.20:5000/quay.io/metal3-io/sushy-tools@sha256:…` with the
  `mirror-registry-credentials` user, never their origins. The artifact
  server's image is a vendor origin and stays on it, and the Registry pulls
  its own image from its origin.
- Its serving certificate `mirror-tls` is issued by the generated authority
  `mirror-ca`, which every consumer trusts; no `trustBundleRef` is authored,
  so the mirror's anchor is that issuer.
- `apply` runs the controller stage, starts the Registry on `mirror`, copies
  each pinned image into it by digest with `skopeo`, verifies the digest it
  holds, then runs every consumer.
- Everything else is [lab-rhel](../lab-rhel/README.md): its guest, network,
  managed services on the controller and operator inputs. Supply the mirror
  host's SSH key pair (`mirror-access-key`) and its host key
  (`mirror-known-hosts`) as well.
