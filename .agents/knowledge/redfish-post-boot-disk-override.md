# Managed-OS node boots its old disk: post-boot Hdd override clobbers Once/Cd

**Symptom:** A managed-OS (Anaconda) install over Redfish virtual media never
runs the installer — the node boots its existing OS from disk instead. The play
proceeds and later fails at the SSH authentication/ownership check, where a
foreign or stale OS answers on port 22. The boot tasks themselves reported no
error.

**Root cause:** The BMC reports `PowerState=On` the instant chassis power
applies — minutes before UEFI reads the one-time `BootSourceOverrideEnabled=Once`
/ `BootSourceOverrideTarget=Cd` override during POST. Forcing the
subsequent-boot override to `Continuous`/`Hdd` is only safe once the live ISO
has positively booted. On the agent-ISO path this holds: readiness renders
`readiness.type=ssh`, and the SSH probe blocks until the live ISO's sshd
answers, so Once/Cd is long consumed before the disk-override PATCH lands. The
managed-OS path renders `readiness.type=none` (the Anaconda installer
environment has no reachable sshd), so the probe is a no-op and the PATCH would
fire ~1–2 s after power-on. An iBMC that reads BootSourceOverride live during
POST then overwrites the pending Once/Cd with Hdd: the node boots its existing
disk and Anaconda never runs.

**Fix:** Gate every disk-boot-override task on a fact that requires the SSH
readiness gate (`readiness.type == ssh`) to have proven the live ISO booted,
and on `setBootSource` — when the boot source is externally managed, Bootwright
never set Once/Cd and must not force Hdd either. On the managed-OS path the
override is skipped entirely and is not needed: Once/Cd self-clears after the
single install boot, the fresh install's own UEFI boot entry takes precedence,
and `wait.yml` ejects the media after SSH is ready. Fixed in 793f22bc; the
gating is pinned by `TestBootRedfishDispatchesMediaBackendBeforeInsert`
(internal/repo/checks/ansible_boot_redfish_test.go).
