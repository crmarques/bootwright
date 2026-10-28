# Secondary-network and MetalLB rehearsal

This synthetic preparation profile covers [B729](../../specs/milestones/m3.md#b729)
and the [acceptance matrix](../../specs/openshift-coverage.md#acceptance-profiles).
No live run or owner acceptance is claimed. Complete the prerequisite accepted
lab-sno and lab-compact rows before the consuming M3d acceptance run.

3 masters and 2 workers, each 8 vCPU, 16 GiB memory and a 120 GiB root disk. Including reserve, provide at least 44 logical CPUs, 88 GiB available memory and 640 GiB pool capacity, plus media space. Each node has its machine NIC, an addressless localnet uplink and a statically addressed third NIC.

The cluster declares a localnet secondary network, OVN host routing and global forwarding. Its binding selects NMState and MetalLB, with one pool on the machine network and one on the third NIC. Replace the conspicuous `example-channel` and `*.v0.0.0` OLM pins with exact Subscription channel/CSV identities proved by the selected release’s catalog before a live run; validation of the example is not proof those native versions exist. Reserve pool addresses so no other host or DHCP service allocates them.

Provide routes to the machine-network and third-interface service pools from the test client. Label only the intended workload namespace `network.example.test/workloads: enabled`. Use independent clients to prove pod-to-LAN traffic and both MetalLB pools. The forwarding-variant choice and native operator rows remain the owning B729/M4 gates; no undeclared forwarding variant is selected here.

## Prepare and validate

Copy the whole directory before editing. Replace documentation addresses,
zone, controller service binds, certificate SANs, upstream DNS/NTP sources,
and exact release together. Use an unused context and bridge names; do not
edit a running installation’s desired state. Secrets belong in private files
outside the input tree and are bound after context initialization.

```sh
m3_input=$(mktemp -d /tmp/bootwright-lab-networks.XXXXXX)
cp -R examples/openshift-clusters/lab-networks/. "$m3_input/"
make build
./bin/bootwright validate -f "$m3_input"
```

After customization, follow the [compact staging sequence](lab-compact.md#stage-and-install)
with context name `lab-networks` and this input copy. Initialize that context,
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
./bin/bootwright status --context lab-networks
./bin/bootwright machine list --context lab-networks --power-status
./bin/bootwright cluster info --context lab-networks --name networks
./bin/bootwright diff --context lab-networks --observe
```

For cleanup, stop every running guest or physical target explicitly, then run
`bootwright destroy` for this context and follow its exact authorization and
quiescence remediation. Keep the input, build and context until removal is
proved. Restore any controller resolver changes made for the rehearsal.
