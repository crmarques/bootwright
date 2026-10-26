# OpenShift agent install features

How FIPS, disk encryption, a proxy, registry mirrors and install-time manifests
reach the agent installer's inputs, and what each does when it is wrong.
[Container clusters](../../specs/container-clusters.md) owns the required
behavior; this page records native facts for
[B719](../../specs/milestones/m3.md#b719) (install-time manifests),
[B720](../../specs/milestones/m3.md#b720) (TPM2 disk encryption),
[B721](../../specs/milestones/m3.md#b721) (installation proxy),
[B722](../../specs/milestones/m3.md#b722) (FIPS) and
[B724](../../specs/milestones/m3.md#b724) (registry mirrors).

Provenance: `R<n>:C<m>` is a claim id of the M3 research, `RV` marks a claim the
review re-verified, and "observed by the v1 implementation (main-v1)" marks a
fact to re-verify on this tree. Release labels name where a fact was read; they
are not supported or required versions (D163). The ids name the M3 definition's
research and review notes of 2026-10-20, which this repository does not track;
each statement here carries its fact, and an id only records where it was read.
Assisted-service, assisted-installer and assisted-installer-agent behavior was
read at master, not per release.

## FIPS

- `fips: true` in the install configuration reaches the agent as an
  `AgentClusterInstall` install-config override and as the `fips=1` kernel
  argument of the ISO (R6:C4).
- The installer refuses `fips: true` unless the binary is FIPS-capable and the
  host is in FIPS mode. A static binary always refuses; the refusal names the
  FIPS-capable installer (R6:C5).
- A documented bypass exists. It downgrades the check to a warning and marks the
  install configuration (R6:C6). Bootwright never sets it.
- The FIPS-capable binary is extracted from the release payload or downloaded
  from the client mirror (R6:C7). With `fips: true` an `ssh-ed25519` key is
  rejected, and `ssh-rsa` or an ECDSA key is accepted (R6:C8).

### Public artifact verification, 2026-10-23

The official exact-release checksum lists for
[4.20.0](https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.20.0/sha256sum.txt),
[4.21.0](https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.21.0/sha256sum.txt),
[4.21.10](https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.21.10/sha256sum.txt)
and [4.22.0](https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.22.0/sha256sum.txt)
name `openshift-install-rhel9-amd64.tar.gz`. These observations establish exact
release availability, not availability for every version or mirror.

The downloaded 4.21.10 archive matched its published SHA-256,
`b4034fc964b266045858603e40a309f655ae4fe477dc77855b57e3c4072d5981`.
Its only tar member is `openshift-install-fips`. Read-only Go build metadata
inspection (`./scripts/go version -m` on the extracted file) reported
`-tags=baremetal,fipscapable,release,strictfipsruntime`, `CGO_ENABLED=1`,
`GOOS=linux`, `GOARCH=amd64` and installer revision
`dbc7727dfd8bec0bf5c26a103ff30bda012d48b7`. The executable was not run.
The [upstream bare-metal build recipe](https://github.com/openshift/installer/blob/080653d812dc25b2c624ff2a5b4108046276b0b2/images/baremetal/Dockerfile.ci)
and [build script](https://github.com/openshift/installer/blob/080653d812dc25b2c624ff2a5b4108046276b0b2/hack/build.sh)
at the inspected public release-4.21 snapshot explain the FIPS-capable build
and CGO dependency; they are not the downstream artifact revision. This verifies artifact
identity and build capability; it proves neither runtime compatibility with
a particular host nor a FIPS installation or certification.

Bootwright selects a separate exact-release `openshift-install-fips` tool for
FIPS clusters, verifies its RHEL 9 archive against publisher checksums, and
requires a positive host FIPS observation before planning or apply admission.
The bound administration key is checked at media apply. These independent
checks do not qualify execution on a real FIPS-enabled host; that acceptance
row remains [B722](../../specs/milestones/m3.md#b722).

## Disk encryption

- The only route is `diskEncryption` in `agent-cluster-install.yaml`. There is
  no install-config or agent-config field (R6:C12, RV).
- `enableOn` is `none`, `all`, `masters` or `workers`. The newer CRD enum also
  lists `arbiters` and combinations, but the installer's own validation accepts
  only those four in 4.20 to 4.23: an arbiter-only or combined selector cannot
  be built through `openshift-install`, and the research claim that the valid
  values differ by release (R6:C13) is refuted (RV).
- `agent create cluster-manifests` deletes `install-config.yaml` and
  `agent-config.yaml` and replaces them with generated manifests, so any later
  edit is made to those (R6:C45).
- Leftover TPM keys from a previous operating system can leave the deployment
  stuck; the documented remedy is a TPM reset in the BIOS before booting the ISO
  (R6:C17).

## Proxy

- `proxy` in the install configuration becomes `AgentClusterInstall.spec.proxy`
  and `InfraEnv.spec.proxy`, which configures the live ISO. The machine network
  that contains the rendezvous address is appended to `noProxy` when a proxy and
  a rendezvous address exist (R6:C20).
- Install-configuration validation of the proxy: `httpProxy` or `httpsProxy`
  is required; `httpProxy`'s scheme is `http`, `httpsProxy`'s `http` or
  `https`; `noProxy` has no spaces and each comma-separated element is an IP
  address, a CIDR or a domain without wildcards, except the single value `*`;
  a proxy address inside the cluster or service networks, or the OVN internal
  join subnet, is refused (R6:C21). The cluster `Proxy` object, including the automatic in-cluster `noProxy`
  entries, is generated inside assisted-service (R6:C22).
- `additionalTrustBundlePolicy` only controls whether `Proxy.spec.trustedCA` is
  set (R6:C23).

## Registry mirrors and the trust bundle

- `user-ca-bundle` always reaches the nodes through the machine config
  operator, so `Always` is not needed for a private mirror CA (R6:C25, RV
  resolved). v1 rendered `Always` whenever a bundle was set because a
  disconnected install with a self-signed mirror and no proxy failed image pulls
  (L1 DT-2); the review supersedes that reading.
- In the ISO the CA bundle serves the agent's registry pulls (R6:C24).
- `imageDigestSources` becomes `registries.conf` in the ISO and an
  `ImageDigestMirrorSet` in the cluster. `sourcePolicy: NeverContactSource`
  appears in the 4.21 branch and later (R6:C26, C2).
- An offline image build without `oc` on the path silently falls back to a
  network download of the base image (R6:C29).
- With the single-repository mirror layout, release component images live under
  `quay.io/openshift-release-dev/ocp-v4.0-art-dev`. Mapping only `ocp-release`
  lets disconnected nodes resolve components past the mirror (observed by the v1
  implementation (main-v1); re-verify on this tree; L1 DT-1).

## Install-time manifests

- The installer loads only top-level `openshift/*.yaml` and `openshift/*.yml`
  files, sorted by name (R3:C31, RV).
- The bootstrap creates each manifest and skips one that exists. It retries an
  unknown kind for up to 60 minutes, then bootstrap fails with "timed out
  creating manifests" (R3:C32, RV).
- A manifest whose CRD arrives from an OLM Subscription in the same set succeeds
  if OLM installs the operator inside the window; otherwise bootstrap fails.
  The outcome depends on catalog reachability and is unproven (R3:C34, corrected
  by RV).
