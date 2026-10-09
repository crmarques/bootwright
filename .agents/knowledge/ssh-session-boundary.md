# Direct SSH sessions: descriptors, configuration and the terminal

Observed 2026-09-16 building `machine rsh`/`machine exec` on OpenSSH 10.0p2
(this host) against the RHEL 9 controller's 8.7p1. Required behavior stays in
[the session contract](../../specs/cli.md#machine-ssh-sessions) and
[security](../../specs/security.md#direct-ssh-sessions).

## Material stays on the launcher's held descriptors

`internal/machine/sshlocal` creates each file the client reads — the private
key, the pinned `known_hosts` line and the client configuration — under a
`0700` scratch directory and unlinks its name before writing. The launcher
holds each actual descriptor until the waited client exits. The client names
`/proc/<launcher-pid>/fd/<fd>` and reopens it under the same credentials.

OpenSSH closes descriptors above standard error before processing its
arguments ([OpenSSH 10.0p2 startup](https://github.com/openssh/openssh-portable/blob/V_10_0_P2/ssh.c)).
`exec.Cmd.ExtraFiles` therefore cannot make `/proc/self/fd/<n>` work: the
client closes those inherited handles before reading configuration or keys.
Shell stubs that preserve descriptors hid this defect. Parent-held paths
survive that startup cleanup, and their descriptor numbers come from each
`file.Fd()`, never a synthetic child slot. The launcher and client share local
credentials; this is not a handoff across an identity change.

OpenSSH's private-key permission check follows the procfs handle to the real
unlinked `0600` inode. Completion, launch failure and cancellation close the
launcher's handles, leaving no named plaintext. Pinned by
`TestTheClientReadsItsMaterialThroughParentDescriptors`,
`TestSSHMaterialSurvivesClientDescriptorClosure` and
`TestCancelledSSHSessionReleasesParentDescriptors`.

## An offered key is copied, never handed on

`--ssh-id-file` is opened by the invoking account's opener
(`internal/workspace/invokerfs`), so a key in a network home that squashes
root is readable at all. Handing the client that descriptor is not enough: the
client runs as root in the elevated child, and reopening a procfs descriptor makes
it **reopen** the file through the magic link, an open that runs with root's
credentials, which a server squashing root maps to its anonymous account and
so refuses for a `0600` key. The session therefore reads the key through the
received descriptor, bounded at 64 KiB, into an unlinked `0600` copy, exactly
like a stored private key, and the client reads the copy. Pinned by
`TestTheClientReadsACopyOfTheOfferedKey`, which compares the device and inode
the client sees with the key file's.

## `-F` disables both configuration files, which is the point

`ssh -F <file>` skips `/etc/ssh/ssh_config` **and** `~/.ssh/config` (ssh(1)).
That is what keeps a personal `ProxyCommand`, `IdentityFile` or `Match` block
out of a session — and it is also why the crypto policy has to be copied in by
hand: Red Hat's system config is what normally `Include`s
`/etc/crypto-policies/back-ends/openssh.config`, so a session that skips it
would silently drop site and FIPS algorithm selection. `policy.go` copies only
the algorithm-selecting directives and **refuses** the policy outright if it
carries an identity, command, forwarding or host rule, rather than dropping it
quietly.

`IdentitiesOnly=yes` already confines the client to the `-i` keys given, so the
undocumented `IdentityFile=none` the old branch used is unnecessary.

## Pin RSA by its signature algorithms, not by the key type

A `known_hosts` entry records an RSA host key as `ssh-rsa`, but the server may
sign with `rsa-sha2-256`/`rsa-sha2-512`, and many now refuse the SHA-1 form
entirely. `HostKeyAlgorithms=ssh-rsa` would therefore refuse a key the record
already trusts, so `trust.HostKey.Algorithms` expands that one type to
`rsa-sha2-512,rsa-sha2-256,ssh-rsa`. Every other type is its own name.

## Observe with the client, never `ssh-keyscan`

Host-key observation runs the same pinned client with
`StrictHostKeyChecking=accept-new` against a scratch `known_hosts`, all
authentication methods off, and its exit status **ignored**: authentication is
expected to fail, and the key is recorded during the handshake that precedes
it. `ssh-keyscan` cannot be used — it does not honor the controller's crypto
policy, still offers curve25519 under FIPS, and therefore records nothing at
all on a FIPS host. The recorded file is the only source of truth; if it is
absent the endpoint is unreachable, which is unknown rather than untrusted.

## The elevated child had no `TERM`

`machine rsh` runs in the sudo child, and
[the supervisor](../../internal/controller/privilege/sudo.go) gives sudo only
`PATH`, `LANG` and `LC_ALL`. A remote shell with no terminal type cannot draw
itself, so `SudoOptions.Terminal` now forwards the caller's `TERM` for an
interactive invocation, validated against a conservative character set first so
an attacker-chosen value cannot become a second environment entry. Everything
else ambient still stops at that boundary, and the session's own environment
allow-list is `TERM`, `COLORTERM`, `NO_COLOR` alone — which is what keeps a
caller-selected askpass helper, agent socket or `LD_PRELOAD` out of the client.

Pinned by `TestSupervisorForwardsOnlyASafeTerminalIdentity`,
`TestTheClientReadsItsMaterialThroughParentDescriptors`,
`TestTheClientInheritsOnlyTerminalEnvironment` and
`TestAPolicyThatWouldRedirectTheSessionRefusesIt`.

## A context's state directory is bounded, and the bound is load-bearing

Adding `state/trust/` made five entries under a context's state.
`verifyContextLayout` enumerated at most four, so every later secret and
lifecycle mutation would have failed with "state directory entry count exceeds
its limit" once a single host key was trusted. The bound is now the shared
`maxContextStateEntries`, used by both the layout check and the deletion walk.
Any future `state/` area must raise it in the same place; pinned by
`TestAContextThatTrustsAHostKeyStaysUsableAndDeletable`.
