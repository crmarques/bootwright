# Render-internal vocabularies with no authored schema field

Spellings that appear in rendered vars, install-config, or emitted cephadm
input but are NOT authorable — grep bait when a value seems to come from
nowhere.

**`image.mediaType` (`dvd` | `boot`):** the schema has no `mediaType` field
(removed in the `packageSource` redesign); render derives it and emits
`image.mediaType` in the machine-OS-install vars so the
`machine_os_install_anaconda` role can pick the mkksiso path. `dvd` means the
packages ride on `bootMedia` itself; `boot` means a small boot ISO whose
packages come from `packageSource`.

**`machines` and `bmc` service kinds:** `ServiceKindMachines` and
`ProviderServiceKindBMC` are service kinds rendered for Ansible (the
per-cluster machines service and the provider BMC service). They are not
authored `InfraComponent` slots; do not look for them in the component slot
vocabulary.

**`DefaultArtifactsHTTPPort = 8443`:** the default port of the artifact
server's HTTPS listener (8443 is the HTTPS convention). The "HTTP" in the
constant name — like the `artifactServer.http` key in the
`Environment.spec.componentImages` catalog — is historical and does not
imply a plaintext listener.

**Entitlement `provider`/`product` spellings:** emitted values only, derived
from `spec.type` — see
[entitlement-resolution-vars.md](entitlement-resolution-vars.md).

**`networkSubnetCidr` (lowercase `Cidr`):** `VSphereNetworkSubnet` mirrors
the openshift install-config vSphere `nodeNetworking` subnet 1:1 —
`networkSubnetCidr` is the upstream key verbatim, deviating from the house
`CIDR` casing on purpose, and renders unchanged into install-config.
Likewise `baremetal` (`PlatformTypeBareMetal`) is the install-config
platform key used verbatim as both the type value and the arm key.

**KubeVirt VM network wiring:** whatever kind `networkRef` names
(ClusterUserDefinedNetwork, UserDefinedNetwork, NetworkAttachmentDefinition),
the VM always attaches via multus `networkName: <namespace>/<name>` —
a (C)UDN's OVN-derived NetworkAttachmentDefinition shares the object's own
name, so no kind-specific wiring exists in the renderer.
