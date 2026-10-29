# Managed RHEL guest with external network services

The [lab-rhel](../lab-rhel/README.md) guest, installed against a DNS resolver,
a time source and an HTTP proxy that Bootwright does not run. The controller
Machine still hosts libvirt and the managed artifact server that serves the
installer media, because an installation needs a managed artifact server; the
three network services are declared `management: external`, so Bootwright
plans no block for them and neither starts nor removes them. The scenario
installs one 4-vCPU, 8-GiB RHEL 9.8 guest with a 60-GiB disk through an
emulated Redfish BMC.

The input is split like lab-rhel's: [environment](environment.yaml),
[services](infra/components), [Machines](infra/machines) and
[Secret descriptors](secret-descriptors).

## Worth noting

- Environment [kind defaults](../../../specs/api/environment.md#kind-defaults)
  route the external Proxy `lab-proxy` and NTPServer `lab-ntp` into the
  `MachineInstallProfile`, and the external DNSServer `lab-dns` into the
  `NetworkConfig`; the controller Machine authors the same Proxy as its
  [proxy choice](../../../specs/api/infrastructure-services.md#proxy-choice).
- An external service is reached at its declared `address` or `connection`, adds
  no readiness edge, and a wrong address surfaces when the guest installs, not
  at plan time. Placeholders: proxy `192.0.2.8:3128`, resolver `192.0.2.53`,
  time source `192.0.2.123`; the services must answer from the controller and,
  for DNS and NTP, from the guest network.
- The controller's proxy route is limited to what its stage can realize: an
  external, unauthenticated Proxy trusting only the system store, with both
  `httpProxy` and `httpsProxy` set; other shapes are refused by the
  [controller refusal table](../../../specs/api/environment.md#refusal-table).
  Every `noProxy` list keeps the controller, the guest subnet and loopback direct.
- Topology: controller `192.0.2.1` runs only the artifact server (`8443`,
  `8080`); guest `rhel-01` at `198.51.100.11/24` on the NAT bridge `virbr-lab`;
  BMC on loopback `8000`.
- Operator inputs: entitled RHEL 9.8 x86_64 boot and DVD ISOs added as local
  media, and the three external services. Run one lab at a time; other examples
  reuse the guest network, artifact server and BMC sockets. On a FIPS-mode
  controller choose `rsa` or `ecdsaP256` for `bootwright-machine-key`.
