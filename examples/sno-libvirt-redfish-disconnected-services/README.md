# SNO Libvirt Redfish Disconnected Services

Single-node OpenShift with external proxy and DNS plus managed mirror registry,
managed NTP, trust material, and installer artifact publication.

## Edit First

- `environment.yaml`: base domain, generated or file-sourced secrets, proxy,
  mirror registry, trust, NTP, and artifact defaults.
- `service-machine.yaml`: controller/service host addresses and SSH key reference.
- `provider.yaml`: libvirt URI, VM sizing, BMC emulation credentials, and
  bridge name.
- `networkconfig.yaml`: machine CIDR, resolver, route, and NMState interface.
- `cluster-machines.yaml`: API/app VIPs, per-machine IP, and platform render mode.
- `cluster.yaml`: OpenShift release, disconnected mode, install endpoints,
  networking, and node binding.
- `components/*.yaml`: managed artifact, mirror registry, and NTP service
  placement.

## Validate And Apply

```text
bootwright validate -f <input-dir>
bootwright context init lab -f <input-dir>
bootwright secret generate
bootwright secret materialize
bootwright host trust
bootwright apply bastion --yes
bootwright check all
bootwright plan
bootwright apply --yes
bootwright status --watch
```
