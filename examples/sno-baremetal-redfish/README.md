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

```text
bootwright validate -f <input-dir>
bootwright context init --name lab -f <input-dir>
bootwright secret generate
bootwright bastion setup --yes
bootwright preflight all
bootwright plan
bootwright apply --yes
bootwright status --watch
```
