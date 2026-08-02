# SNO Libvirt Redfish

Smallest connected single-node OpenShift lab. Libvirt provides the VM and an
emulated Redfish BMC.

## Edit First

- `environment.yaml`: base domain, secret names, and managed DNS selection.
  `openshift-pull-secret` is the one context secret you set out of band (see
  below); `bmc-credentials` (username `admin`, random password — Bootwright
  configures the emulated BMC with it) and `sno-libvirt-cluster-admin-ssh-key`
  are generated, and `bastion-host-ssh` points at a local key file.
- `service-machine.yaml`: controller/libvirt host addresses and SSH key reference.
- `provider.yaml`: libvirt URI, VM sizing, BMC emulation credentials, and
  bridge name.
- `networkconfig.yaml`: machine CIDR, resolver, route, and NMState interface.
- `infra-component.yaml`: managed dnsmasq — bind address and upstream forwarders.
- `cluster-machines.yaml`: the node `Machine` — `networkConfigRef`, per-machine
  `interfaceAddresses` (IP), and root device hints.
- `cluster.yaml`: OpenShift release, install endpoints, networking, and node
  binding. A single-node cluster has no VIP, so every endpoint slot takes
  `source.type: node` and resolves to the node's own install address — the
  `interfaceAddresses` entry above — instead of repeating that address.

## Validate And Apply

`secret generate` materializes the generated entries (the SSH key and the
emulated `bmc-credentials`); you set the one context secret
(`openshift-pull-secret`) yourself. After each step, run `bootwright status` — it
prints the suggested next command. See
[getting started](../../docs/getting-started/index.md) for the full secret and
host-trust workflow.

```text
bootwright validate -f <input-dir>
bootwright context init --name lab -f <input-dir>
bootwright secret set --name openshift-pull-secret --pull-secret <path>
bootwright secret generate
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
