# Agent hosts require at least one interface

**Symptom:** `openshift-install agent create image` fails with
`invalid Hosts configuration: hosts[0].interfaces: Required value: at least one
interface must be defined for each node`.

**Root cause:** The agent installer requires every host in `agent-config.yaml`
to include at least one interface with a MAC address. Explicit bare-metal
machines carry provider MAC inventory, but profile-backed libvirt machines need
Bootwright to generate and apply a deterministic virtual NIC MAC.

**Fix:** For libvirt profile-backed machines, render a deterministic
`52:54:00:*` MAC into the libvirt domain XML, `agent-config.yaml
hosts[].interfaces[]`, and the matching NMState interface.
