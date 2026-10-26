# IPv6 installation-feature rehearsal

This synthetic preparation profile covers [B728](../../../specs/milestones/m3.md#b728)
and the [acceptance matrix](../../../specs/container-cluster-coverage.md#acceptance-profiles).
No live run or owner acceptance is claimed. Complete the prerequisite accepted
lab-sno and lab-compact rows before the consuming M3d acceptance run.

The same 3-master capacity as lab-features: at least 28 logical CPUs, 56 GiB available memory and 400 GiB pool capacity including reserve, plus media space. Cluster machine, pod and service networks are IPv6 only.

Use a controller reachable at an IPv6 address by the guests. The managed guest network uses `forward: none`; provide routing between it and the controller services before applying. This example does not assume that IPv4 NAT provides IPv6 connectivity. The proxy, mirror, DNS and NTP endpoints must be reachable from the IPv6-only guests. Replace the documentation IPv6 prefixes consistently, including certificate SANs, VIPs, gateway and Machine addresses. Retain explicit `dhcp: false` and `autoconf: false`.

The cluster name remains `features`. The mirror credentials and trust bindings are the same as [lab-features](../lab-features/README.md). This is an IPv6 row of B725/B728, not dual-stack support.

## Prepare and validate

Copy the whole directory before editing. Replace documentation addresses,
zone, controller service binds, certificate SANs, upstream DNS/NTP sources,
and exact release together. Use an unused context and bridge names; do not
edit a running installation’s desired state. Secrets belong in private files
outside the input tree and are bound after context initialization.

```sh
m3_input=$(mktemp -d /tmp/bootwright-lab-features-ipv6.XXXXXX)
cp -R examples/container-clusters/lab-features-ipv6/. "$m3_input/"
make build
./bin/bootwright validate -f "$m3_input"
```

After customization, follow the [compact staging sequence](../lab-compact/README.md#stage-and-install)
with context name `lab-features-ipv6` and this input copy. Initialize that context,
bind the pull secret and every context-store Secret this profile declares,
generate the generated Secrets, then validate, plan and stage the controller
and infrastructure components. Configure the controller’s conditional DNS
route for the chosen cluster zone before container-cluster preflight.
Review the plan’s destructive authorization before the full apply.

## Observe and record

Use the [acceptance ledger](../../../docs/acceptance.md) to record the build,
exact native release, phase durations, fresh apply, interrupted boot and wait
continuations, settled replay, Ready nodes and feature-specific observations.
Keep addresses, identities, credentials and private media links out of shared
records. Repeat the [compact interruption drills](../lab-compact/README.md#acceptance-and-interruption-drills)
for this topology; a profile that validates is not an accepted row.

```sh
./bin/bootwright status --context lab-features-ipv6
./bin/bootwright machine list --context lab-features-ipv6 --power-status
./bin/bootwright cluster info --context lab-features-ipv6 --name features
./bin/bootwright diff --context lab-features-ipv6 --observe
```

For cleanup, stop every running guest or physical target explicitly, then run
`bootwright destroy` for this context and follow its exact authorization and
quiescence remediation. Keep the input, build and context until removal is
proved. Restore any controller resolver changes made for the rehearsal.
