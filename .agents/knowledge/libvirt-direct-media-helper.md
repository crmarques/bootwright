# Libvirt direct media helper: namespace prefix, hashed path, eject rc capture

**Constraint (preserve the metadata namespace prefix):**
`container_cluster_media_libvirt/files/insert_libvirt_media.py` calls
`ET.register_namespace("bootwright", "https://bootwright.io/libvirt/metadata/1.0")`
before its parse/serialize round-trip of the domain XML. Without it,
ElementTree re-emits the foreign namespace with a generated prefix (`ns0:`),
mangling the `<bootwright:...>` ownership marker that the destroy guard
matches. Registering the prefix keeps the marker byte-for-byte intact after
`virsh define`. Any new tool that rewrites domain XML must do the same or
destroys start refusing Bootwright-owned domains as foreign.

**Constraint (content-hashed helper path):** The insert helper is staged on
the provider host at
`/var/tmp/bootwright-libvirt-media-insert-<sha1-of-file>.py`, hashed from the
helper's own content. Two bootwright versions converging concurrently on one
provider host then never race the shared copy/execute window — a fixed shared
path would let a later copy overwrite the helper a running task is about to
exec, running the other version's code.

**Constraint (eject script exit semantics):** In
`files/eject_libvirt_media.sh`, only a successful `virsh` call that reports a
non-running domain state is the clean "nothing to eject" skip; a
connection/lookup failure exits `2` rather than masquerading as "not
running". The script captures `rc=$?` at the command itself, not after the
`fi`: a bare `if cmd; then ...; fi` whose condition is false leaves `$?` at
the if-statement's own status (`0`), so capturing after the `fi` would mask a
failed `virsh domblklist` as success.

**Semantics (eject modes):** The eject script's last argument selects the
cleanup mode: `source` acts only on cdrom devices that currently hold media
(eject the medium, keep the drive — used by the pre-insert clean and the
post-installer-boot persistent clean, so the drive the installer still needs
is never torn down); `all` acts on every cdrom device — eject any medium,
then detach the drive itself, leaving the provisioned guest with no leftover
`/dev/sr0` (the final cleanup). In `all` mode the drained drive is still
listed after the eject pass, so a second pass detaches the empty drive too.
The mode is derived from `bootwright_libvirt_media_detach_drive` in
`tasks/eject.yml`.
