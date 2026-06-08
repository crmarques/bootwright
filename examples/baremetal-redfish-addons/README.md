# Bare-Metal Redfish Addons

Bare-metal OpenShift with ordered add-on profiles, OLM operators, readiness
checks, and a manifest-set add-on.

## Edit First

- `environment.yaml`: base domain, `resources`, secret names, and artifact
  server catalog.
- `infra/`: service host, bare-metal provider, artifact server, and network
  definitions.
- `clusters/container/metal-ocp/cluster-machines.yaml`: VIPs, artifact endpoint,
  machine selection, and platform render mode.
- `clusters/container/metal-ocp/cluster.yaml`: release, cluster networking, and
  node bindings.
- `add-ons/*.yaml`: OLM packages, channels, optional `startingCSV`, readiness
  checks, manifest paths, and profile order.
- `clusters/container/metal-ocp/add-on-binding.yaml`: selected add-on profile
  for this cluster.

## Validate And Apply

`secret generate` only materializes the `generated:` entries; set the operator
secrets this example declares (`openshift-pull-secret`, `bmc-credentials`)
yourself first. `bastion-host-ssh` points at a local key file. After each step,
run `bootwright status` for the suggested next command. See
[getting started](../../docs/getting-started.md) for the full secret and
host-trust workflow.

```text
bootwright validate -f <input-dir>
bootwright context init lab -f <input-dir>
bootwright secret set openshift-pull-secret --pull-secret <path>
printf '%s\n' "${BMC_PASS}" | bootwright secret set bmc-credentials --username "${BMC_USER}" --password-stdin
bootwright secret generate
bootwright bastion setup --yes
bootwright check all
bootwright plan
bootwright apply --yes
bootwright status --watch
```

Use `bootwright apply --stage clusters --clusters metal-ocp --yes` when only
cluster install, add-on apply, or add-on recovery should run.
