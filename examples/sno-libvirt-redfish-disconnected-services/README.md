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
- `cluster-machines.yaml`: per-machine IP and root device hints.
- `cluster.yaml`: OpenShift release, disconnected mode, install endpoints,
  networking, and node binding.
- `components/*.yaml`: managed artifact, mirror registry, and NTP service
  placement.

## Validate And Apply

```text
bootwright validate -f <input-dir>
bootwright context init --name lab -f <input-dir>
bootwright secret generate
bootwright machine trust
bootwright bastion setup --yes
bootwright preflight all
bootwright plan
bootwright apply --yes
bootwright status --watch
```
