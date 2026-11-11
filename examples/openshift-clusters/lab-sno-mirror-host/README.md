# Single-node OpenShift with a managed artifact mirror

Prepares the [lab-sno](../lab-sno/README.md) cluster with its controller
stage's public-origin clients served by a managed artifact mirror on a second,
OS-ready Machine reached over SSH. This is a synthetic preparation profile for
the [mirror host](../../../specs/infrastructure-services.md#mirror-host): it
claims no live run, and the [controlled-network
profile](../../../specs/milestones/backlog.md#b1250) owns its acceptance.

## Worth noting

- `mirror` (`mirror.lab.example.test`, `203.0.113.20`) is an OS-ready Machine
  reached as root over SSH with a bound host key. It reaches the publishers
  directly and runs `podman`; nothing else on it is managed.
- `mirror-artifacts` is a managed ArtifactServer on `mirror` serving HTTPS on
  `443`, the one port the controller stage reads a mirror on. The Environment
  names it in `spec.mirror.artifacts.serverRef` and `endpointRef`, so the
  controller stage reads Helm from
  `https://203.0.113.20/mirror/lab-sno-mirror-host/<kind>/<release>/<file>`
  by the SHA-256 the build pins, never from their publishers. The OpenShift
  clients and installer stay on their vendor origin.
- Its serving certificate `mirror-tls` is issued by the generated authority
  `mirror-ca`, which the controller stage trusts beside its system trust store;
  no `trustBundleRef` is authored, so the mirror's anchor is that issuer.
- `apply` first starts the mirror's server and fills it on `mirror`, each file
  fetched from its publisher and published only at its pinned SHA-256, then
  runs the controller stage, then every other block. `preflight` reports a
  file the fill has not yet published as pending.
- Everything else is [lab-sno](../lab-sno/README.md): its guest, network,
  managed services on the controller and operator inputs. Supply the mirror
  host's SSH key pair (`mirror-access-key`) and its host key
  (`mirror-known-hosts`) as well.
