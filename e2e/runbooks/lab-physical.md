# Physical-machine OpenShift rehearsal

This synthetic preparation profile covers [B730](../../specs/milestones/m3.md#b730)
and the [acceptance matrix](../../specs/openshift-coverage.md#acceptance-profiles).
No live run or owner acceptance is claimed. Complete the prerequisite accepted
lab-sno and lab-compact rows before the consuming M3d acceptance run.

3 physical masters and 2 physical workers, with an explicit root-device path and complete NIC inventory per machine. Provide the actual release’s native CPU, memory and disk requirements on each server. The controller requires at least 4 vCPU, 16 GiB memory and 100 GiB free storage for tools and artifacts.

Every BMC name, MAC and disk path here is synthetic. Replace them in a private copy with the exact target identities and all physical NICs. Supply `lab-bmc-credentials` and `lab-bmc-ca` through context Secret bindings. BMC transport and virtual-media delivery must verify TLS; keep the default certificate-import policy or prove established trust. An explicit `/dev/...` root-device selector is mandatory. The controller must reach every node’s installation address and BMC, and every BMC must fetch the private artifact service.

Complete the emulated physical-path rehearsal and the owning M1 physical prerequisites before the real-hardware row. Power each target off explicitly before installation. For an encryption variant, confirm TPM2 availability, clear leftover TPM keys according to the hardware runbook before reinstalling, then add `security.diskEncryption.unlock.tpm2: {}` in the private copy. Removal ejects media and deletes Bootwright-owned delivery state; it retains the operating system and data on physical disks.

## Prepare and validate

Copy the whole directory before editing. Replace documentation addresses,
zone, controller service binds, certificate SANs, upstream DNS/NTP sources,
and exact release together. Use an unused context and bridge names; do not
edit a running installation’s desired state. Secrets belong in private files
outside the input tree and are bound after context initialization.

```sh
m3_input=$(mktemp -d /tmp/bootwright-lab-physical.XXXXXX)
cp -R examples/openshift-clusters/lab-physical/. "$m3_input/"
make build
./bin/bootwright validate -f "$m3_input"
```

After customization, follow the [compact staging sequence](lab-compact.md#stage-and-install)
with context name `lab-physical` and this input copy. Initialize that context,
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
./bin/bootwright status --context lab-physical
./bin/bootwright machine list --context lab-physical --power-status
./bin/bootwright cluster info --context lab-physical --name physical
./bin/bootwright diff --context lab-physical --observe
```

For cleanup, stop every running guest or physical target explicitly, then run
`bootwright destroy` for this context and follow its exact authorization and
quiescence remediation. Keep the input, build and context until removal is
proved. Restore any controller resolver changes made for the rehearsal.
