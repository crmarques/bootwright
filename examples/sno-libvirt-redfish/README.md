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
- `infra-component.yaml`: managed dnsmasq — bind address, upstream forwarders,
  and the ingress hostnames it publishes.
- `cluster-machines.yaml`: per-machine IP, root device hints, and platform render mode.
- `cluster.yaml`: OpenShift release, install endpoints, networking, and node
  binding.

## Validate And Apply

`secret sync` only materializes the generated entries; you must set the
context secrets (`openshift-pull-secret`, `bmc-credentials`) yourself. After each
step, run `bootwright status` — it prints the suggested next command. See
[getting started](../../docs/getting-started.md) for the full secret and
host-trust workflow.

```text
bootwright validate -f <input-dir>
bootwright context init lab -f <input-dir>
bootwright secret set openshift-pull-secret --pull-secret <path>
printf '%s\n' "${BMC_PASS}" | bootwright secret set bmc-credentials --username "${BMC_USER}" --password-stdin
bootwright secret sync
bootwright bastion setup --yes
bootwright preflight all
bootwright plan
bootwright apply --yes
bootwright status --watch
```

## Controller DNS Is Wired Automatically

`openshift-install` polls the cluster API from the **controller** (this host),
so the controller must resolve the cluster endpoints (`api`, `api-int`,
`*.apps`) under `bootwright.test`. Because `networkconfig.yaml` points the node
network at the managed dnsmasq (`name-resolution`, bound to `192.168.132.1`),
bootwright wires the controller to it during `apply`: before the agent install
runs it installs a systemd-resolved drop-in routing `~bootwright.test` to
`192.168.132.1`, a controller-side gate verifies resolution before booting the
node, and `destroy` removes the drop-in again. The managed dnsmasq also forwards
non-lab names (`infra-component.yaml` `forwarders`), so the SNO node can reach
the release registry.

Nothing to configure — this follows from declaring a managed `nameResolution`
component that the cluster's network uses.

### If the controller does not run systemd-resolved

Auto-wiring goes through systemd-resolved. On a controller that uses something
else, the gate will fail listing the missing names; point the host resolver at
`192.168.132.1` for `bootwright.test` yourself — simplest is `/etc/hosts`:

```bash
sudo tee -a /etc/hosts >/dev/null <<'EOF'
192.168.132.20 api.sno-libvirt.bootwright.test api-int.sno-libvirt.bootwright.test
192.168.132.20 console-openshift-console.apps.sno-libvirt.bootwright.test oauth-openshift.apps.sno-libvirt.bootwright.test
EOF
```
