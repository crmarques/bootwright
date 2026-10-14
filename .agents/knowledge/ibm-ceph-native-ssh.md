# IBM Ceph native SSH qualification

Observed while reviewing M2b against the latest IBM release on 2026-10-11.
This page records artifact findings; [storage](../../specs/storage.md) owns
trust requirements and [B472](../../specs/milestones/m2.md#b472) owns promotion.

IBM's [release mapping](https://www.ibm.com/support/pages/what-are-red-hat-and-ibm-storage-ceph-releases-and-corresponding-ceph-package-versions)
then selected Storage Ceph 9.9.1.2, package 20.2.1-405.el9cp and container tag
v9.9.1-25074. That public source inspection did not acquire or execute an
entitled container.

The public [downstream source RPM](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/source/ceph-20.2.1-405.el9cp.src.rpm)
contains a signed header verified against the official
[IBM signing key](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/RPM-GPG-KEY-IBM-CEPH),
fingerprint `005F07B2F3838AEDC2C0A4D91E1850467EAA5B6E`. Its complete downstream
changes archive has SHA-256
`a984c10875622c6511f547d73271ea8f35e2fec3fbb31bccaeb5867d9696be3c`, matching
the digest authenticated by that header. The entire source RPM was not
downloaded, so no whole-RPM checksum result is claimed.

The archive's `src/pybind/mgr/cephadm/ssh.py`, lines 179–180, passes
`known_hosts=None` explicitly to `asyncssh.connect`. The extracted file's
SHA-256 is `a07ee53ec6ca44d91738e92869fff5c236e392285379a227bd8ab3ee7933feb2`.
[AsyncSSH's options contract](https://asyncssh.readthedocs.io/en/latest/api.html#asyncssh.SSHClientConnectionOptions)
states that this disables verification and that explicit arguments take
precedence over configuration. Supplying `StrictHostKeyChecking yes` through
the native configuration therefore does not prove manager-to-host verification.

This is a concrete failure of the inspected source path, not a missing
entitlement key alone. It does not qualify a running container, a workaround
or a negative wrong-key test. Ordinary apply admits only `verified-tunnel`
for the pinned release, and `native` stays refused (D143); the host block's
route probe proves the provider's SSH class on each host before that release's
cluster block starts.

The owner approved an outer verified SSH `ProxyCommand` design in D135. It is
still a candidate transport: it must
authenticate the intended host and forward only to that same host's loopback
SSH endpoint, without direct fallback. Bootstrap staging, protected mounts,
permissions, restart/failover and alternate connection paths need their own
qualification. Do not replace one unqualified assumption with another.

The in-tree tunnel and staging helpers retain separate native/tunnel gates,
and the host block's route probe exercises the provider's SSH class on
synthetic loopback inside the pinned image on each host. A
configuration-parser success alone does not prove refusal when the native
config disappears, and rootless inventory or parser probes do not exercise
live host authentication; neither qualifies guest policy or the complete MGR
lifecycle.
AsyncSSH 2.13.2's
[`_ProxyCommandTunnel.pipe_connection_lost`](https://raw.githubusercontent.com/ronf/asyncssh/v2.13.2/asyncssh/connection.py)
closes the SSH connection on EOF from any proxy pipe, including stderr.
Replacing fd 2 with `/dev/null` therefore terminates the native channel before
forwarding can start. Keep that inherited pipe open and use OpenSSH's fixed
`-E /dev/null` log destination instead. A transport test's banner controls
must also refuse early stderr EOF; a successful standalone OpenSSH command alone
does not prove native proxy compatibility.
OpenSSH key options alone cannot forbid remote or Unix-socket listeners; the
candidate also owns and proves a restrictive guest sshd policy. Conditional
foreign configuration and unproved crypto-policy symlink layouts refuse.
The exact provider image and guest layout remain required qualification.

The downstream MGR also caches `cephadm.<SHA256>` beneath its FSID data root.
Native remote writes can retain the dedicated SSH account as owner with mode
0744. A root-only daemon observer would therefore reject a legitimate cache;
the candidate observer accepts that metadata only when the account comes from
the original owned SSH-authorization record. The provider executes an existing
cache path before its missing-command retry and does not independently compare
its bytes. Filename, owner and mode establish neither authenticated code nor
permission to execute it. Live continuation must prove the expected image
payload and held cache descriptor's exact bytes before enrollment effects.

The same authenticated source's `cephadmlib/container_engines.py` implements
registry login with `-p` followed by the password, even when bootstrap receives
a private registry JSON file. Bootstrap also stores that JSON through a native
command argument. For Podman, its image-pull command reads
`/etc/ceph/podman-auth.json` when present. The candidate therefore prepares an
exclusively owned protected authfile and supplies no registry credential to
bootstrap; it checks that the native stored registry key is absent before
enrollment. This route still needs actual package/runtime and removal
qualification. Never adopt a foreign authfile or use a secret-derived digest
as ownership evidence.

The signed downstream `ceph-common` binary package's `ceph` CLI reads `-i -`
from standard input. The candidate generated-client path retains its original
protected key/caps before sending a keyring through that input. It uses
[`auth add`](https://docs.ceph.com/en/tentacle/api/mon_command_api/#auth-add),
whose upstream [20.2.1 implementation](https://raw.githubusercontent.com/ceph/ceph/v20.2.1/src/mon/AuthMonitor.cc)
creates or verifies an exact key/caps match, rather than `auth import`, which
can replace an existing identity. The fixed generator is
[`ceph-authtool --gen-print-key`](https://docs.ceph.com/en/tentacle/man/8/ceph-authtool/).
Signed-package inspection and a synthetic generator probe do not qualify
authorization, interrupted continuation or client removal in a running cluster.
