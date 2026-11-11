# Single-node OpenShift installed from a managed mirror host

Prepares the [lab-sno-mirror-host](../lab-sno-mirror-host/README.md) cluster
with the mirror host also serving the Environment's image mirror, a managed
Registry into which that host copies the cluster's release before the cluster
is built. This is a synthetic preparation profile for the
[mirror host](../../../specs/infrastructure-services.md#mirror-host): it claims
no live run, and the [controlled-network
profile](../../../specs/milestones/backlog.md#b1250) owns its acceptance.

## Worth noting

- `mirror-registry` is a managed Registry on `mirror` serving HTTPS on `5000`
  with the same `mirror-tls` certificate, so its anchor is `mirror-ca` as the
  artifact mirror's is. The Environment names it in `spec.mirror.registry`, so
  every managed service and emulated BMC pulls its image from it.
- The cluster selects no registry mirror, so the Environment's is its mirror:
  `mirror-registry-credentials` joins the install pull secret for
  `203.0.113.20:5000`, `mirror-ca` joins its trust bundles, and both release
  repositories map beneath `203.0.113.20:5000/` with `NeverContactSource`.
- `mirror-release-sno` copies release `4.21.15` into that Registry on `mirror`
  after the Registry starts, with the release's own `oc` read from
  `mirror.openshift.com` and checked against its published `sha256sum.txt`. The
  copy is lent an auth document holding the pull secret's `quay.io` entry and
  the Registry's credential alone, in a private run directory it removes. The
  cluster's image build and installation wait for it.
- Everything else is [lab-sno-mirror-host](../lab-sno-mirror-host/README.md).
