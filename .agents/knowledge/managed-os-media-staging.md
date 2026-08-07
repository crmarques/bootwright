# Managed-OS install media staging and hosted-tree publish

Sharing, serialization, and integrity rules in
`machine_os_install_anaconda/tasks/{install_media,resolve,publish_install_tree}.yml`
and `internal/preflight/media.go`.

**Shared source, per-machine outputs:** every machine in an OS group installs
from the same source ISO; only the per-machine Kickstart differs. The renderer
owns the shared-source identity (`image.sourceId`) and the effective source
path (`image.effectiveSourcePath`): the staged shared ISO under `_source/`, or
the in-place media when the install runs where the source already lives
(`sourceOnTarget`). Bare-metal installs run on the controller, where the media
store already holds the ISO, so they render `sourceOnTarget: true` — without it
the role copies the full DVD once per node into per-machine paths and can
exhaust the controller's disk. Kickstart, TMPDIR, and the output ISO are
per-machine and fan out under the task's forks; each active mkksiso build needs
roughly one source ISO of scratch space.

**Controller-collapse throttles:** bare-metal-over-BMC, KubeVirt, and vSphere
machines all drive the install from the controller (localhost), so every
machine in a cluster collapses onto the same host. The ISO-tooling package
install is `throttle`d to one host (concurrent runs race the dnf transaction
test: `package ... is already installed`), and the shared-source copy is
throttled so parallel nodes never write the same destination file (copy /
get_url skip on a checksum match). The shared ISO is re-hashed only when a run
actually (re)staged it — get_url verified the checksum on download and copy
verified src==dest by hash, so re-hashing an unchanged multi-GB ISO once per
machine adds only minutes of serialized controller I/O.

**Preflight presence check:** `installerMediaChecks` (internal/preflight/
media.go) verifies every controller-local install ISO (`local-media:` store
entry or `file://` path) exists before the machines phase; `http(s)://` images
download on the provider host and are skipped, and the hostedTree DVD
(`fromMedia`) is checked alongside `bootMedia`. Symptom and remediation detail
in managed-os-install-media-missing.md. Replacing an existing media entry is
authorized by a single `--yes` (or an interactive y), matching `secret set` —
`media add` carries no second overwrite flag of its own.

**Hosted install tree identity and atomicity:** the tree
(`packageSource.hostedTree`) is extracted once per (cluster, image) with
run_once. Identity is the DVD `size:mtime` (mirroring the boot-source identity
check), so a re-apply is a cheap no-op and a changed DVD forces a verified
rebuild. The `.bootwright-tree.identity` file is written LAST inside the
building tree — a partially built tree never carries a matching identity, so an
interrupted run rebuilds cleanly. Publish is an atomic rename on the same
filesystem, so a concurrently fetching installer only ever sees a complete
tree. The DVD must be a media-store entry (or absolute file) already
sha256-verified by `bootwright media add` — validation rejects a url
`fromMedia` — and the copy is Red Hat's documented full-tree copy (`/.` keeps
dotfiles; skipping `.treeinfo` breaks the source). The trust model is
documented in docs/concepts/machines.md.

**rd.live.check is stripped:** mkksiso builds pass `--rm-args rd.live.check` to
drop the ISO media-check from every boot menu entry. RHEL's stock default entry
is "Test this media & install", which re-reads and checksums the whole ISO
before Anaconda starts — many minutes over BMC virtual media for no real gain:
the source ISO is already SHA-256 verified (when a checksum is set) and every
RPM is GPG-verified during install; rd.live.check only re-confirms the
just-built ISO was read off the virtual CD without corruption (integrity, not
authenticity; mkksiso re-implants its own md5 regardless). Do not re-add
(6907f43c).

**SELinux labels:** the install ISO is built directly into its publish
directory (`boot.agentIso.stagePath`) — for bare metal that is the artifact
server's nginx docroot, bind-mounted `:Z` (container_file_t). Align the staged
directory and ISO to the publish root's label (`chcon --reference`), never
`restorecon`: that resets to var_lib_t, the nginx container_t read AVC-denies,
nginx 404s, and the BMC reports `iBMC.1.0.ConnectionFailed` on the media fetch.
The hosted tree gets the same alignment (a mislabeled tree fails the node's
package fetch). For libvirt/vSphere media backends the publish root carries the
provider's own label and inheriting it stays correct. See also
redfish-virtual-media.md.
