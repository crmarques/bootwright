# SNO Bare-Metal Redfish

Single-node OpenShift on real bare metal through Redfish virtual media.

## Edit First

- `environment.yaml`: base domain, secret names, artifact server catalog, and
  the default artifact access every bare-metal cluster inherits.
- `service-machine.yaml`: service host addresses and SSH key reference.
- `provider.yaml`: the bare-metal boot method and network attachments (physical
  inventory lives on the `Machine`, not here).
- `networkconfig.yaml`: machine CIDR, resolver, route, and NMState interface.
- `cluster-machines.yaml`: the node `Machine` — server MACs, BMC Redfish URL and
  credential reference, root device hints, per-machine IP, and network config.
- `cluster.yaml`: OpenShift release, API/ingress endpoint VIPs, networking, and
  node binding.

## Validate And Apply

`secret generate` only materializes the `generated:` entries; set the operator
secrets this example declares (`openshift-pull-secret`, `bmc-credentials`)
yourself first. `bastion-host-ssh` points at a local key file. After each step,
run `bootwright status` for the suggested next command. See
[getting started](../../docs/getting-started/index.md) for the full secret and
host-trust workflow.

```text
bootwright validate -f <input-dir>
bootwright context init --name lab -f <input-dir>
bootwright secret set --name openshift-pull-secret --pull-secret <path>
printf '%s\n' "${BMC_PASS}" | bootwright secret set --name bmc-credentials --username "${BMC_USER}" --password-stdin
bootwright secret generate
bootwright bastion setup --yes
bootwright preflight all
bootwright plan
bootwright apply --yes
bootwright status --watch
```
