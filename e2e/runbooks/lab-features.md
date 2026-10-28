# Installation-feature rehearsal

This synthetic preparation profile covers [B728](../../specs/milestones/m3.md#b728)
and the [acceptance matrix](../../specs/openshift-coverage.md#acceptance-profiles).
No live run or owner acceptance is claimed. Complete the prerequisite accepted
lab-sno and lab-compact rows before the consuming M3d acceptance run.

3 schedulable masters, each 8 vCPU, 16 GiB memory, 120 GiB disk and an emulated TPM. Including the authored reserve, provide at least 28 logical CPUs, 56 GiB available memory and 400 GiB pool capacity, plus media space.

The cluster selects TPM2 disk encryption, the managed installation proxy, and an external mirror with an explicit trust bundle. Replace both authored image source mappings with mappings that actually exist on the mirror; the release and component repositories are separate. Bind `mirror-ca` as a CA bundle and `mirror-credentials` as Docker configuration JSON containing the selected mirror host. A conflicting pull-secret entry refuses.

For separate rows, remove `security.diskEncryption`, `install.proxy`, or `install.registries` in fresh private copies to isolate the selected feature. Keep mirror trust only where consumed. Serving certificates remain B723’s live definition probe and are deliberately absent. FIPS and disconnected choices also remain outside this profile. The complete IPv6 variant is [lab-features-ipv6](lab-features-ipv6.md).

## Prepare and validate

Copy the whole directory before editing. Replace documentation addresses,
zone, controller service binds, certificate SANs, upstream DNS/NTP sources,
and exact release together. Use an unused context and bridge names; do not
edit a running installation’s desired state. Secrets belong in private files
outside the input tree and are bound after context initialization.

```sh
m3_input=$(mktemp -d /tmp/bootwright-lab-features.XXXXXX)
cp -R examples/openshift-clusters/lab-features/. "$m3_input/"
make build
./bin/bootwright validate -f "$m3_input"
```

After customization, follow the [compact staging sequence](lab-compact.md#stage-and-install)
with context name `lab-features` and this input copy. Initialize that context,
bind the pull secret and every context-store Secret this profile declares,
generate the generated Secrets, then validate, plan and stage the controller
and infrastructure components. Configure the controller’s conditional DNS
route for the chosen cluster zone before container-cluster preflight.
Review the plan’s destructive authorization before the full apply.

## Observe and record

Use the [acceptance ledger](../../docs/acceptance.md) to record the build,
exact native release, phase durations, fresh apply, interrupted boot and wait
continuations, settled replay, Ready nodes and feature-specific observations.
Keep addresses, identities, credentials and private media links out of shared
records. Repeat the [compact interruption drills](lab-compact.md#acceptance-and-interruption-drills)
for this topology; a profile that validates is not an accepted row.

```sh
./bin/bootwright status --context lab-features
./bin/bootwright machine list --context lab-features --power-status
./bin/bootwright cluster info --context lab-features --name features
./bin/bootwright diff --context lab-features --observe
```

For cleanup, stop every running guest or physical target explicitly, then run
`bootwright destroy` for this context and follow its exact authorization and
quiescence remediation. Keep the input, build and context until removal is
proved. Restore any controller resolver changes made for the rehearsal.
