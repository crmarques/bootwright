# Managed-OS install with unstaged source ISO

**Symptom:** Without a guard, a managed-OS apply whose install ISO was
never staged fails deep into the machines phase with an opaque copy or
mkksiso `Could not find or access` error, long after the operator could
have cheaply staged the media.

**Root cause:** The install-media task copies the source ISO from the
controller (`remote_src: false`), and the controller-local
`sourceOnTarget` path reads `image.path` in place — either way the
source ISO must already exist on the controller (staged via
`bootwright media add`) before the machines phase runs. Nothing about
the copy step itself says "you forgot to stage the media".

**Fix:** `machine_os_install_anaconda/tasks/install_media.yml` fails
fast with an actionable message when the source ISO is missing on the
controller, before the copy/mkksiso step; the preflight installer-media
host check reports the same condition even earlier. If you still see the
opaque `Could not find or access` error, the media store entry or the
staged path is missing — stage the ISO with `bootwright media add` and
re-apply.
