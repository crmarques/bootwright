# Installation-feature rehearsal

A synthetic preparation profile for [B728](../../../specs/milestones/m3.md#b728)
and the [acceptance matrix](../../../specs/openshift-coverage.md#acceptance-profiles):
three masters with TPM2 disk encryption, the managed installation proxy and an
external mirror. No live run or owner acceptance is claimed, and the accepted
[lab-sno](../lab-sno/) and [lab-compact](../lab-compact/) rows are
prerequisites to the consuming M3d run.

## Worth noting

- Three schedulable masters `features-01` to `features-03`, each 8 vCPU, 16 GiB
  memory, 120 GiB disk and an emulated TPM. Host capacity including the
  reserve: at least 28 logical CPUs, 56 GiB available memory and 400 GiB pool
  disk, plus media space.
- The cluster selects `security.diskEncryption.unlock.tpm2`, the managed proxy
  (`lab-proxy`) and the external registry `lab-mirror` with `mirror-ca` as its
  trust bundle.
- Replace both authored `imageDigestSources` mappings with mappings that exist
  on the mirror; the release and component repositories are separate. Bind
  `mirror-ca` as a CA bundle and `mirror-credentials` as Docker configuration
  JSON containing the selected mirror host. A conflicting pull-secret entry
  refuses.
- To isolate one feature, remove `security.diskEncryption`, `install.proxy` or
  `install.registries` in a fresh private copy; keep mirror trust only where
  consumed.
- Out of scope: serving certificates (B723's live definition probe), FIPS and
  disconnected installs. The IPv6 variant is
  [lab-features-ipv6](../lab-features-ipv6/).

Runbook: [e2e/runbooks/lab-features.md](../../../e2e/runbooks/lab-features.md).
