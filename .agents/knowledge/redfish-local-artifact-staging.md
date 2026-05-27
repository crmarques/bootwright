# Redfish Local Artifact Staging

Symptom:

- `boot_redfish : Stage agent ISO at the BMC's fetch location` fails with
  `UNREACHABLE`
- `destroy infra` fails during `Gathering Facts`
- Ansible reports `Failed to connect to the host via ssh`
- The failure line looks like `localhost -> bastion`
- The SSH target shows a BMC or unrelated user as `ansible_user`

Cause:

`boot_redfish` must place the generated agent ISO where the BMC can fetch it.
For bare-metal Redfish installs this path is derived from the selected
`InfraProvider.spec.artifactPublishers[].http` capability. The artifact route
address is for BMC HTTP fetches, while `hostRef` is the host running the
artifact server.

When the artifact server is on the same controller/bastion running Bootwright,
the ISO should be copied locally into the existing artifact state directory.
SSH to `hostRef` is only needed for genuinely remote artifact hosts.

Investigation:

- Inspect `rendered/ansible/vars.yaml` for
  `bootwright_clusters[].components[].boot.agentIso`.
- Inspect `rendered/ansible/inventory.yaml` for the `stageHost` host entry.
- If `ansible_user` is present, it came from explicit `Host.spec.ssh.user`.
  Omit `spec.ssh.user` unless the provider host really needs a forced SSH
  login name.
- If `stageHost` is the local controller, confirm the artifact directory exists
  under `bootwright_managed_dir` after `bootwright apply infra`.
