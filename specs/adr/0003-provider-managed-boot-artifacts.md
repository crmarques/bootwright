# Derived Artifact Publication

## Status

Accepted. Replaces the earlier Environment-level controller-local HTTP fields
and the per-cluster `ClusterInfra.spec.components.artifacts` declaration.

## Context

Bare-metal Redfish virtual media needs an HTTPS URL the BMC can fetch when
`boot_redfish` inserts the agent ISO. Disconnected agent installs also need a
cluster-facing `bootArtifactsBaseURL` for minimal ISO flows.

Earlier designs made operators declare that serving path either on
`Environment` or on each `ClusterInfra`. Both shapes leaked a runtime
publication detail into authored cluster intent.

## Decision

Declare artifact publication as a reusable provider capability:
`InfraProvider.spec.artifactPublishers[].http`.

The renderer derives concrete publication consumers from install requirements:

- Bare-metal Redfish machines publish the generated agent ISO for the BMC
  audience.
- Disconnected agent installs publish boot artifacts for the cluster audience.
- `ClusterInfra.spec.components.artifacts` is not part of the authored API.
- HTTP `hostRef` lives on the provider capability.
- HTTP `port` lives on the provider capability and defaults to Bootwright's
  artifact publication port.
- BMC-specific and cluster-specific artifact routes reference neutral
  `Host.spec.addresses[].name` values from
  `artifactPublishers[].http.routes`.
- The renderer derives the HTTPS `bindAddress` from Bootwright defaults.

## Consequences

- `Environment` remains provider-neutral and cluster-neutral.
- `ClusterInfra` describes durable cluster infrastructure, not generated file
  serving.
- Operators whose bastion serves artifacts model it as a normal `Host` with
  `container-runtime` plus an `artifactPublishers[].http` capability.
- v1alpha1 has no compatibility alias for `components.artifacts`.
