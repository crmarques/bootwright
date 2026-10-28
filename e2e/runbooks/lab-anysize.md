# Any-size OpenShift rehearsal

This synthetic preparation profile covers [B728](../../specs/milestones/m3.md#b728)
and the [acceptance matrix](../../specs/openshift-coverage.md#acceptance-profiles).
No live run or owner acceptance is claimed. Complete the prerequisite accepted
lab-sno and lab-compact rows before the consuming M3d acceptance run.

3 masters (8 vCPU, 16 GiB each), 6 workers and 1 infra node (4 vCPU, 8 GiB each), all with 120 GiB root disks. Every node has an active-backup bond on the machine network and an addressless third NIC on a second managed network. The authored reserve adds 4 CPUs, 8 GiB memory and 40 GiB pool disk: provide at least 56 logical CPUs, 112 GiB available memory and 1240 GiB pool capacity, plus media space.

The infra node uses the native worker pool during installation. Infra labels and taints remain B716’s owner-defined post-installation row; this profile does not claim them.

For the 5-master/no-compute and 4-master/2-worker variants, edit a private copy: keep exactly five or six Machine files respectively, assign the first five or four cluster nodes role `controlPlane`, and set each such node’s `substrate.libvirt.profileRef: master`. Remaining nodes use role `worker` and `substrate.libvirt.profileRef: compute`. Remove unused node records and Machines together, retain unique addresses, then rerun validation and planning. Do not add an arbiter until B717 is defined.

## Prepare and validate

Copy the whole directory before editing. Replace documentation addresses,
zone, controller service binds, certificate SANs, upstream DNS/NTP sources,
and exact release together. Use an unused context and bridge names; do not
edit a running installation’s desired state. Secrets belong in private files
outside the input tree and are bound after context initialization.

```sh
m3_input=$(mktemp -d /tmp/bootwright-lab-anysize.XXXXXX)
cp -R examples/openshift-clusters/lab-anysize/. "$m3_input/"
make build
./bin/bootwright validate -f "$m3_input"
```

After customization, follow the [compact staging sequence](lab-compact.md#stage-and-install)
with context name `lab-anysize` and this input copy. Initialize that context,
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
./bin/bootwright status --context lab-anysize
./bin/bootwright machine list --context lab-anysize --power-status
./bin/bootwright cluster info --context lab-anysize --name anysize
./bin/bootwright diff --context lab-anysize --observe
```

For cleanup, stop every running guest or physical target explicitly, then run
`bootwright destroy` for this context and follow its exact authorization and
quiescence remediation. Keep the input, build and context until removal is
proved. Restore any controller resolver changes made for the rehearsal.
