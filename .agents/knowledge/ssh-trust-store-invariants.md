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
