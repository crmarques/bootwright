# SNO Libvirt Redfish — Corporate TLS

The smallest connected single-node OpenShift lab, extended to show **corporate
TLS**: replacing the default cluster-URL certificates with corporate-issued
serving certificates, and trusting a corporate CA. Libvirt provides the VM and
an emulated Redfish BMC. The substrate is identical to
[`sno-libvirt-redfish`](../sno-libvirt-redfish/) — only `environment.yaml` and
`cluster.yaml` differ. See
[Corporate TLS](../../docs/advanced/corporate-certificates.md) for the full
walkthrough.

## What this adds

- **Corporate serving certificates** — `cluster.yaml`
  `spec.install.servingCertificates` overrides the default self-signed
  certificates served at `api.sno-libvirt.bootwright.test` and the
  `*.apps.sno-libvirt.bootwright.test` wildcard. Bootwright renders these as
  day-2 `APIServer` and `IngressController` manifests with their TLS secrets.
- **Corporate trusted CA** — `cluster.yaml`
  `spec.install.additionalTrustBundleRefs` adds `corporate-ca` to the cluster's
  install trust (install-config `additionalTrustBundle`, policy `Always`). Trust
  the same CA fleet-wide instead with `Environment.spec.installTrust.caBundleRefs`.

The three TLS secrets are declared as `generated: selfSignedCertificate` so the
example is self-contained and validates with no external material. In a real
deployment you would issue the serving certificates from your corporate CA and
load the bytes yourself (see the `secret set` commands below) rather than
generate them.

!!! note
    Declaring a serving certificate does **not** automatically trust its issuer.
    When internal components must trust the corporate-issued URL certificates,
    add the issuing CA to `additionalTrustBundleRefs` (or `installTrust`) too —
    which is exactly what this example does with `corporate-ca`.

## Edit First

- `environment.yaml`: base domain, secret names, and managed DNS selection.
  `openshift-pull-secret` and `bmc-credentials` are context secrets you set out
  of band (see below); `sno-libvirt-cluster-admin-ssh-key` and the three TLS
  secrets are generated, and `bastion-host-ssh` points at a local key file.
- `cluster.yaml`: OpenShift release, install endpoints, the corporate
  `additionalTrustBundleRefs` and `servingCertificates`, and node binding.
- `service-machine.yaml`: controller/libvirt host addresses and SSH key reference.
- `provider.yaml`: libvirt URI, VM sizing, BMC emulation credentials, and
  bridge name.
- `networkconfig.yaml`: machine CIDR, resolver, route, and NMState interface.
- `infra-component.yaml`: managed dnsmasq — bind address, upstream forwarders,
  and the ingress hostnames it publishes.
- `cluster-machines.yaml`: per-machine IP, root device hints, and platform render mode.

## Validate And Apply

`secret generate` materializes the generated entries (the SSH key, BMC password,
and the three self-signed TLS certificates); you must set the context secrets
(`openshift-pull-secret`, `bmc-credentials`) yourself. To use **real** corporate
material instead of the generated certificates, drop the `generated:` blocks for
`corporate-ca`, `api-serving-tls`, and `ingress-serving-tls` and supply the
bytes with `secret set` (shown commented below). After each step, run
`bootwright status` — it prints the suggested next command.

```text
bootwright validate -f <input-dir>
bootwright context init --name lab -f <input-dir>
bootwright secret set --name openshift-pull-secret --pull-secret <path>
printf '%s\n' "${BMC_PASS}" | bootwright secret set --name bmc-credentials --username "${BMC_USER}" --password-stdin
# Real corporate material (replaces the generated: declarations):
# bootwright secret set --name corporate-ca       --raw-file ./corp-ca.pem
# bootwright secret set --name api-serving-tls     --tls-cert ./api.crt     --tls-key ./api.key
# bootwright secret set --name ingress-serving-tls --tls-cert ./ingress.crt --tls-key ./ingress.key
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
