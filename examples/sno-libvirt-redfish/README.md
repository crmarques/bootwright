# SNO Libvirt Redfish

Smallest connected single-node OpenShift lab. Libvirt provides the VM and an
emulated Redfish BMC.

## Edit First

- `environment.yaml`: base domain, secret names, and managed DNS selection.
  `openshift-pull-secret` and `bmc-credentials` are context secrets you set out
  of band (see below); `sno-libvirt-cluster-admin-ssh-key` is generated and
  `bastion-host-ssh` points at a local key file.
- `service-machine.yaml`: controller/libvirt host addresses and SSH key reference.
- `provider.yaml`: libvirt URI, VM sizing, BMC emulation credentials, and
  bridge name.
- `networkconfig.yaml`: machine CIDR, resolver, route, and NMState interface.
- `cluster-machines.yaml`: API/app VIPs, per-machine IP, and platform render mode.
- `cluster.yaml`: OpenShift release, install endpoints, networking, and node
  binding.

## Validate And Apply

`secret generate` only materializes the generated entries; you must set the
context secrets (`openshift-pull-secret`, `bmc-credentials`) yourself. After each
step, run `bootwright status` — it prints the suggested next command. See
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
