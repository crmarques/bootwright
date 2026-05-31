# Environment-Bound Artifact Server

## Status

Accepted. Replaces the earlier Environment-level bastion-local HTTP fields,
per-cluster `ClusterInfra.spec.components.artifacts`, and provider-declared
artifact publishers.

## Context

Bare-metal Redfish virtual media needs an HTTPS URL the BMC can fetch when
`boot_redfish` inserts the agent ISO. Disconnected agent installs also need a
cluster-facing `bootArtifactsBaseURL` for minimal ISO flows.

Those consumers may need different addresses for the same artifact service:
BMC firmware might require an IP on an out-of-band network, while cluster
nodes may need an in-band address or hostname. Hosts are the durable owners of
addresses; the artifact server is a reusable service hosted on one of those
hosts; the environment chooses which service endpoint each consumer audience
uses.

## Decision

Declare artifact publication as an `InfraComponent` with
`spec.artifactServer`. The component selects:

- `hostRef` for service placement.
- `listeners[]` for protocol and port.
- `endpoints[]` for named routable service addresses, each backed by a
  listener and `hostAddress` value that matches a
  `Host.spec.addresses[].name`.

Bind consumer audiences in
`Environment.spec.infraComponents.artifactServers[].routes`:

- `redfishVirtualMedia.endpoint` selects the endpoint rendered into BMC ISO
  fetch URLs.
- `containerClusterInstall.endpoint` selects the endpoint rendered into disconnected
  `bootArtifactsBaseURL`.

The renderer derives concrete publication consumers from install
requirements:

- Bare-metal Redfish machines publish the generated agent ISO for the BMC
  audience.
- Disconnected agent installs publish boot artifacts for the cluster audience.
- `ClusterInfra.spec.components.artifacts` is not part of the authored API.
- `InfraProvider.spec.artifactPublishers` is not part of the authored API.

## Consequences

- `Host` remains the single owner of routable addresses.
- `InfraComponent` owns artifact server placement, listener, and endpoint
  definition.
- `Environment` owns route binding for consumer audiences.
- `ClusterInfra` describes durable cluster infrastructure, not generated file
  serving.
- Operators whose bastion serves artifacts model it as a normal `Host` with
  `container-runtime` plus an `InfraComponent.spec.artifactServer`.
- v1alpha1 has no compatibility alias for provider artifact publishers.
