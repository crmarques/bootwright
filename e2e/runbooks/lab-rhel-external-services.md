# Managed RHEL guest with external network services

The [lab-rhel](lab-rhel.md) guest, installed against a DNS resolver,
a time source and an HTTP proxy that Bootwright does not run. The controller
Machine still hosts libvirt and the managed artifact server that serves the
installer media, because an installation needs a managed artifact server; the
three network services are declared `management: external`, so Bootwright
plans no block for them and neither starts nor removes them. The scenario
installs one 4-vCPU, 8-GiB RHEL 9.8 guest with a 60-GiB disk through an
emulated Redfish BMC.

The input is split like lab-rhel's: [environment](../../examples/machines/lab-rhel-external-services/environment.yaml),
[services](../../examples/machines/lab-rhel-external-services/infra/components), [Machines](../../examples/machines/lab-rhel-external-services/infra/machines),
[provider](../../examples/machines/lab-rhel-external-services/infra/providers/lab-libvirt.yaml),
[network](../../examples/machines/lab-rhel-external-services/infra/networkconfigs/lab-guests.yaml), [OS profiles](../../examples/machines/lab-rhel-external-services/infra/os) and
[Secret descriptors](../../examples/machines/lab-rhel-external-services/secret-descriptors). Descriptors contain no credentials;
every Secret is generated locally in its context.

## What it teaches

| Declaration | Consumer | How it is chosen |
| --- | --- | --- |
| External `Proxy` `lab-proxy` | The controller Machine's [proxy choice](../../specs/api/infrastructure-services.md#proxy-choice), which carries the controller's own acquisition | Authored on the controller Machine |
| The same Proxy | The `MachineInstallProfile` `rhel-9-8`, and through it the installation and installed-OS policy of the guest | Environment `spec.defaults.MachineInstallProfile.proxy`; the profile authors nothing |
| External `NTPServer` `lab-ntp` | The same profile's `ntp` list | Environment `spec.defaults.MachineInstallProfile.ntp` |
| External `DNSServer` `lab-dns` | The `NetworkConfig` `lab-guests`, so the guest resolves through it | Environment `spec.defaults.NetworkConfig.dns` |

[Environment kind defaults](../../specs/api/environment.md#kind-defaults)
fill only what an object leaves absent, so an object that authors a choice
replaces the default as a whole. An external service is reached at its declared
`address` or `connection` and forbids `endpointRef`; it adds no readiness edge,
so a wrong address surfaces when the guest installs, not at plan time.

The controller's route is limited to what its stage can realize. The Proxy here
is external, unauthenticated and trusts only the system store, with both
`httpProxy` and `httpsProxy` set; each other shape is refused before
registration by the
[controller refusal table](../../specs/api/environment.md#refusal-table).
The controller stage cannot acquire through a managed Proxy;
[lab-rhel](lab-rhel.md) runs all three as managed services.

## Host prerequisites

Those of [lab-rhel](lab-rhel.md#host-prerequisites): a Linux/amd64
controller that meets the
[controller admission requirements](../../specs/controller.md#supported-hosts),
hardware virtualization, entitled RHEL 9.8 x86_64 boot and DVD ISOs that you
supply, and an unused guest subnet and bridge. Run one of these labs at a time:
other examples reuse the guest network, the artifact server and BMC sockets.
Free `8443/tcp` and `8080/tcp` on the controller address for the artifact
server and `8000/tcp` on loopback for the BMC; no DNS, NTP or proxy socket is
needed. The services below must already exist and answer from the controller
and, for DNS and NTP, from the guest network.

## Lab conventions

Copy and customize the input outside Git. Every address and name here is a
public documentation placeholder, not a usable host configuration.

| Input | Customize together |
| --- | --- |
| Proxy `192.0.2.8:3128` | `httpProxy` and `httpsProxy` of `lab-proxy`: a proxy the controller and the guest can reach, with no authentication and a certificate the system trust store verifies. |
| Resolver `192.0.2.53`, time source `192.0.2.123` | The `address` of `lab-dns` and `lab-ntp`: servers the guest network can reach. |
| Controller `192.0.2.1` and `controller.lab.example.test` | The controller Machine, the artifact server bind address and the artifact certificate's IP and DNS names. The IP must already exist on the host. |
| Guest `198.51.100.11/24`, subnet `198.51.100.0/24`, gateway `198.51.100.1` | The guest Machine, NetworkConfig and provider attachment. The provider creates the NAT bridge `virbr-lab`; do not pre-create it. |
| `lab.example.test` and the `noProxy` entries | Environment base domain, Machine FQDNs, search domain and every bypass list: both `noProxy` lists must keep the controller, the guest subnet and loopback direct. |
| Loopback BMC `127.0.0.1:8000` | Keep loopback binding; its generated certificate covers that address. |

On a FIPS-mode controller choose `rsa` or `ecdsaP256` for
`bootwright-machine-key`; see the
[FIPS guidance](../../docs/operator-guide.md#a-fips-mode-controller).

## Run it

From the repository root, copy the example and make the substitutions above:

```sh
external_input=$(mktemp -d /tmp/bootwright-lab-rhel-external.XXXXXX)
cp -R examples/machines/lab-rhel-external-services/. "$external_input/"
```

Use a current `./bin/bootwright` executable (`make build` builds it from this
checkout). Replace the ISO paths and run the commands separately. Mutation
commands present their plan and confirmation; sudo authorization may also be
needed.

```sh
./bin/bootwright version
./bin/bootwright validate -f "$external_input"
./bin/bootwright setup
./bin/bootwright media add --name rhel-9.8-x86_64-boot.iso --from-file /path/to/rhel-9.8-x86_64-boot.iso
./bin/bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file /path/to/rhel-9.8-x86_64-dvd.iso
./bin/bootwright media list --checksums
./bin/bootwright context init --name lab-rhel-external --input-dir "$external_input"
./bin/bootwright secret generate
./bin/bootwright secret check
./bin/bootwright plan
./bin/bootwright apply --stage controller
./bin/bootwright preflight controller --context lab-rhel-external
./bin/bootwright apply
./bin/bootwright status
./bin/bootwright machine exec --name rhel-01 cat /etc/os-release
./bin/bootwright diff --observe
./bin/bootwright apply
```

Review the full `plan` after `context init` and before the first
`apply --stage`: while a staged operation is paused, `context update` refuses
until it completes or is destroyed. The plan lists no block for the proxy, DNS
or NTP service. The unscoped apply starts the artifact server, creates the
provider and the guest, publishes the package tree and installs RHEL. Confirm
from the guest that it uses the external services:

```sh
./bin/bootwright machine exec --name rhel-01 cat /etc/resolv.conf
./bin/bootwright machine exec --name rhel-01 chronyc sources
```

The final apply settles an unchanged completed operation without contacting
hosts; `diff --observe` reports drift. Changed input requires destroy before
another apply: after destroy, import edits with
`context update --name lab-rhel-external --input-dir "$external_input"`.
For a failed or interrupted apply, follow
[lab-rhel's interruption guidance](lab-rhel.md#interrupting-an-apply).

## Undo

```sh
./bin/bootwright machine stop --name rhel-01
./bin/bootwright destroy --authorize data-loss
./bin/bootwright status
./bin/bootwright context delete --name lab-rhel-external --purge
```

Stop the guest only if its realization exists. Destroy removes the artifact
server, provider network, guest and guest disks; disk deletion requires
`data-loss` independently of confirmation. It leaves the external proxy, DNS and
NTP services as they were. Verify removal before deleting the context.

Media is host-wide and stays reserved until every consuming context is
destroyed. Keep it for other examples, or remove it afterward:

```sh
./bin/bootwright media delete --name rhel-9.8-x86_64-boot.iso
./bin/bootwright media delete --name rhel-9.8-x86_64-dvd.iso
```

Restore any host daemon or firewall configuration changed for the lab, and
remove the temporary input copy when no longer needed.
