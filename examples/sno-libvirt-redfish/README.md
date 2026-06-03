# SNO Libvirt Redfish

Smallest connected single-node OpenShift lab. Libvirt provides the VM and an
emulated Redfish BMC.

## Edit First

- `environment.yaml`: base domain, secret names, and managed DNS selection.
- `host.yaml`: controller/libvirt host addresses and SSH key reference.
- `provider.yaml`: libvirt URI, VM sizing, BMC emulation credentials, and
  bridge name.
- `networkconfig.yaml`: machine CIDR, resolver, route, and NMState interface.
- `cluster-infra.yaml`: API/app VIPs, per-machine IP, and platform render mode.
- `cluster.yaml`: OpenShift release, install endpoints, networking, and node
  binding.

## Validate And Apply

```text
bootwright validate -f <input-dir>
bootwright context init lab -f <input-dir>
bootwright secret generate
bootwright apply bastion --yes
bootwright check all
bootwright plan
bootwright apply --yes
bootwright status --watch
```
