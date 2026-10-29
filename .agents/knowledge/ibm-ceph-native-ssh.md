# IBM Ceph native SSH qualification

Observed while reviewing M2b against the latest IBM release on 2026-10-11.
This page records artifact findings; [storage](../../specs/ceph.md) owns
trust requirements and [B472](../../specs/milestones/m2.md#b472) owns promotion.

IBM's [release mapping](https://www.ibm.com/support/pages/what-are-red-hat-and-ibm-storage-ceph-releases-and-corresponding-ceph-package-versions)
then selected Storage Ceph 9.9.1.2, package 20.2.1-405.el9cp and container tag
v9.9.1-25074. That public source inspection did not acquire or execute an
entitled container. As of 2026-10-18 the same table lists v9.9.1-27200 as the
newer 9.9.1 tag.

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
SHA-256 is `a07ee53ec6ca44d91738e92869fff5c236e392285379a227bd8ab3ee7933feb2`
(historical 9.9.1.2 inspection evidence, not a gate).
[AsyncSSH's options contract](https://asyncssh.readthedocs.io/en/latest/api.html#asyncssh.SSHClientConnectionOptions)
states that this disables verification and that explicit arguments take
precedence over configuration. Supplying `StrictHostKeyChecking yes` through
the native configuration therefore does not prove manager-to-host verification.

This is a concrete failure of the inspected source path, not a missing
entitlement key alone. It does not qualify a running container, a workaround
or a negative wrong-key test. Ordinary apply admits only `verifiedTunnel`
for the operator-selected, digest-frozen image, and `native` stays refused (D143); the host block's
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
synthetic loopback inside the operator-selected, digest-frozen image on each host. A
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

## Provider runtime and package imports

The public [RHEL 10 AsyncSSH source RPM](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel10/source/python-asyncssh-2.17.0-1.el10cp.src.rpm)
publishes version 2.17.0. The route probe previously required exactly 2.13.2
before either synthetic preparation or the behavioral checks. That check
produced `route-runtime-refusal` for an otherwise importable SDK, independently
of its SSH behavior. The probe now reaches its behavioral checks with the SDK
in the selected image; an import failure still refuses at the runtime phase.

The downstream changes archive authenticated by the signed header of IBM's
[Ceph 20.2.2 source RPM](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel10/source/ceph-20.2.2-349.el10cp.src.rpm)
adds a relative import of `is_fips_enabled` and `get_default_ssh_config` from
`cephadm.utils`. Its SSH manager also reads cached target FIPS facts and the
manager's public key. A standalone `ssh.py` module has no package context for
that import. Load its actual utility source with the SSH source in an isolated
package, registering modules before execution because the utilities define a
dataclass. This avoids running the daemon package initializer or substituting
the provider's FIPS behavior. The synthetic manager supplies its generated
public key and unknown target FIPS facts; it does not invent host observations.

These findings concern public artifacts, not acceptance of an image or a real
SSH route. Header and archive checks authenticate the inspected source; they
do not claim a complete source-RPM payload verification.

## Listener replacement and socket reuse

The route probe closes its synthetic outer listener after the positive case,
waits for it to close, then binds the same endpoint with a replacement host
key for the wrong-key case. Both [AsyncSSH 2.13.2](https://raw.githubusercontent.com/ronf/asyncssh/v2.13.2/asyncssh/connection.py)
and [2.17.0](https://raw.githubusercontent.com/ronf/asyncssh/v2.17.0/asyncssh/connection.py)
default `listen(reuse_address=False, reuse_port=False)`; `create_server`
forwards those options. The public IBM AsyncSSH source RPM above confirms
that behavior for its 2.17.0 source. An explicit `False` overrides
[asyncio's Unix address-reuse default](https://docs.python.org/3.12/library/asyncio-eventloop.html#asyncio.loop.create_server),
which applies when its argument is `None`.

`SSHAcceptor.close` stops listening and `wait_closed` delegates to the asyncio
server; closing the listener does not clear TCP `TIME_WAIT` left by accepted
connections. A synthetic reproduction using the authenticated public SDK on
ephemeral loopback endpoints reproduced `EADDRINUSE` after an active server
close with the default options. Setting `reuse_address=True` on both the
initial and replacement listeners allowed the rebind. Keeping
`reuse_port=False` still refused a duplicate bind while the initial listener
was active. Both listener generations need the address-reuse option; retain
the exact bound-address proof, direct-contact listeners, event counters and
all negative SSH cases.

A bare `route-listener-refusal` covers listener creation and the subsequent
socket proof. It cannot establish which listener or operation failed, or
whether the cause was address reuse, permissions or another error. Use a
bounded diagnostic with a fixed listener label, `create` or `prove` operation,
and fixed `errorClass`/`socketError` categories; keep native exception text and
endpoint details private. Source inspection and synthetic rebinding establish
this probe lifecycle requirement. Actual-image and host acceptance still
require the complete behavioral route proof for the operator's selection.

The upstream Tentacle v20.2.1 connection
([ssh.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/ssh.py#L161))
carries the same disabled verification as the downstream archive.
