# Single-node OpenShift with a managed artifact mirror on an installed Machine

Prepares [lab-sno-mirror-host](../lab-sno-mirror-host/README.md) with its mirror
host a libvirt guest that Bootwright installs with RHEL, rather than an
OS-ready Machine. This is a synthetic preparation profile for the
[mirror host](../../../specs/infrastructure-services.md#mirror-host): it claims
no live run, and the [controlled-network
profile](../../../specs/milestones/backlog.md#b1250) owns its acceptance.

## Worth noting

- `mirror` (`mirror.lab.example.test`, `198.51.100.20`) is a `lab-libvirt`
  guest installed from `rhel-9-8-mirror`, which adds `podman` and disables the
  host firewall, because no managed service opens a firewall port yet. It
  authors no `access.ssh`: the server and the fill reach it as `bootwright`
  with the Environment's fleet key `bootwright-machine-key`, become root
  through the sudo rule its installation wrote, and accept only the host key
  its installation proved, so no known-hosts Secret is supplied.
- Its installation uses `lab-artifacts`, `lab-dns` and `lab-ntp` on
  `controller`, never a service on `mirror` itself.
- `apply` first installs the controller stage's host packages
  (`controller-prerequisites`), then the provider host, the `mirror` guest, its
  package tree and its installation (`os-install-mirror`), then the mirror's
  server and its fill, then the stage's clients
  (`controller-mirrored-clients`), then every other block.
- Everything else is [lab-sno-mirror-host](../lab-sno-mirror-host/README.md)
  and [lab-sno](../lab-sno/README.md). Supply the RHEL boot and DVD media the
  profile names with `bootwright media add`.
