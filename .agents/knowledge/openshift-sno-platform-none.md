# Single-node agent installs require platform none

**Symptom:** `openshift-install agent create image` fails with
`Only platform none and external supports 1 ControlPlane and 0 Compute nodes`
after Bootwright renders an SNO `install-config.yaml`.

**Root cause:** The agent installer rejects bare-metal and vSphere installer
platform blocks for single-node clusters. Bootwright may still use
`ClusterInfra.spec.platform.type: baremetal` to model the machine-control path,
but the generated installer input must not render `platform.baremetal` for SNO.

**Fix:** Render `platform.none` for single-node clusters unless
`ClusterInfra.spec.platform.type: external` is explicitly selected. Keep
endpoints, selected machines, and managed infra components on `ClusterInfra`;
they still drive DNS, load balancer, boot, and provider variables outside the
installer platform block.
