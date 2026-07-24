# SSH trust store invariants: divergent keys, FIPS scans, scope

**One address, one key:** `Store.ValidateAddressConsistency`
(`internal/sshtrust/sshtrust.go`) fails closed when two trust records pin the
same address to different keys, because OpenSSH accepts ANY matching
known_hosts entry for a host: emitting two divergent lines for one address
would let a peer present EITHER key and still pass `StrictHostKeyChecking`,
silently weakening the trust-on-first-use pin from "exactly this key" to
"either key". The store is keyed by Machine name and two Machines may
legitimately resolve to one address (allowed when the keys are identical), so
the invariant is enforced in `Save` — the single point that writes
known_hosts. Guarded by `TestSaveRejectsDivergentKeysForOneAddress`.

**Host-key capture must honor FIPS:** `scanHostKeyCommand`/`ScanHostKeys`
(`internal/sshtrust/plan.go`) deliberately run
`ssh -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=<scratch> -o BatchMode=yes`
instead of `ssh-keyscan`: ssh-keyscan ignores the controller's system crypto
policy (it still offers curve25519 in KEXINIT under FIPS), so a FIPS backend
refuses the exchange and no key is ever returned. `ssh` includes
`/etc/crypto-policies/back-ends/openssh.config` and negotiates a FIPS-approved
KEX, recording the key the connection actually used. Auth is EXPECTED to fail
(no identity offered) — `accept-new` pins the host key before auth, so the ssh
exit code is deliberately ignored and the recorded known_hosts scratch file is
the source of truth. If that file does not exist afterward, the handshake
never reached key exchange (host unreachable) and the caller reports
"no SSH host keys returned". Mirrors
`roles/machine_os_install_anaconda/tasks/ssh_trust.yml`; pinned by
`TestScanHostKeyCommandUsesFIPSHonoringSSH`.

**A pinned managed-OS key is replaced only by an actual install:**
`machine_os_install_anaconda/tasks/ssh_trust.yml` keeps an existing
known_hosts entry for the address — the `accept-new` scan runs only when no
entry exists or `bootwright_os_ssh_keyscan_replace` is true, and the only
caller passing true is `wait.yml`, with
`bootwright_managed_os_install_required` (an install this run actually
performed, which mints a new host key by construction). Until 2026-07-23 the
task deleted-and-re-accepted the recorded key on EVERY probe, so a swapped
host was silently re-pinned and the pre-install ownership probe proved
nothing. Now a changed live key fails the probe's `StrictHostKeyChecking=yes`
auth and lands in the fail-closed unverifiable refusal (see
[managed-os-install-gates.md](managed-os-install-gates.md)); the deliberate
rotation path is `bootwright machine trust --replace <machine>`. The
`connectionAddress` alias entry is always re-derived from the address pin, so
the alias can never carry a different key than the pinned address.

**Ref-vs-managed resolution is the only centralized part:**
`MachineKnownHostsPath` (`internal/sshtrust/machine.go`) resolves what a
Machine verifies SSH host keys against: the explicit per-Machine
`spec.access.ssh.knownHostsRef` secret when set, otherwise a managed
trust-store known_hosts path that the CALLER supplies — render
(portable/live inventory vars) and cli (a live context) locate that store
through different roots, so only the ref-vs-managed decision is centralized
(the part that must not drift between them). Relatedly, `MachinesInScope` in
`plan.go` narrows the managed host-trust surface to machines a scoped run will
actually SSH to (nil scope = no restriction), so a trust record is required
only where a scheduled task connects.
