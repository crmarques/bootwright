# Agent ISO publish directory permissions and token secrecy

**Symptom:** A libvirt guest that should boot the directly-attached agent ISO
fails to start with `Could not open <iso>: Permission denied`, even though the
staged ISO exists and root can read it.

**Root cause:** The per-token publish directory created by
`container_cluster_agent_install/tasks/iso/publish_target.yml` was too
restrictive. The libvirt-direct media backend (`qemu:///system`) attaches the
staged ISO straight into the guest domain, and libvirt's dynamic ownership
chowns only the ISO **file** to the qemu user — not the parent directory. With
the directory at `0700`, the unprivileged qemu process cannot traverse it to
reach the ISO.

**Fix:** The per-token publish directory is created with mode `0711`, not
`0700`. The execute-only bits let qemu traverse while keeping the directory
non-listable — the unguessable publish token still gates discovery — and the
ISO file itself stays `0600` so its contents remain private. This matches
libvirt's own `/var/lib/libvirt/images` convention (`drwx--x--x`). Do not
"harden" the directory back to `0700`.

**Constraint:** The agent-ISO `stagePath` embeds the per-ISO publish token
(substituted in `container_cluster_agent_install` `boot_machine.yml`). Any
task that touches it — the `container_cluster_media_libvirt` `set_fact`s, the
Redfish backend equivalents — must run with `no_log`, or the full token is
written to stdout/logs and the token-gated path stops being unguessable.
