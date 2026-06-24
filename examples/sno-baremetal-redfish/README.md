# SNO Bare-Metal Redfish

Single-node OpenShift on real bare metal through Redfish virtual media.

## Edit First

- `environment.yaml`: base domain, secret names, and artifact server catalog.
- `service-machine.yaml`: service host addresses and SSH key reference.
- `provider.yaml`: server MACs, BMC Redfish URL, root device hints, and BMC
  credential reference.
- `networkconfig.yaml`: machine CIDR, resolver, route, and NMState interface.
- `cluster-machines.yaml`: API/app VIPs, Redfish artifact endpoint, per-machine
  IP, and platform render mode.
- `cluster.yaml`: OpenShift release, install endpoints, networking, and node
  binding.

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
