# FIPS controller: ssh-keyscan never records a host key

**Symptom:** managed-OS host-key pinning retries forever on a FIPS-enabled
controller; the scan never records a key. Debug output shows
`Key exchange type c25519 is not allowed in FIPS mode`.

**Root cause:** `ssh-keyscan` does NOT honor the controller's system crypto
policy: on a FIPS controller it still offers curve25519 in its KEXINIT, the
managed OS prefers it, and the FIPS backend refuses — so no host key is ever
recorded and the scan loops.

**Fix:** `ssh` DOES honor the policy (it Includes
`/etc/crypto-policies/back-ends/openssh.config`) and negotiates a
FIPS-approved KEX (e.g. `ecdh-sha2-nistp256`). Host-key pinning
(`machine_os_install_anaconda/tasks/ssh_trust.yml`) therefore uses
`ssh -o StrictHostKeyChecking=accept-new` to record the key the connection
actually used — the same key the verify step re-encounters under
`StrictHostKeyChecking`. A reinstalled node's stale pin is dropped first
(`ssh-keygen -R`) so the fresh key is re-learned; the accept-new
connection's auth outcome is ignored because auth verification is the later
verify step's job.

The capture must not force a host-key type. Non-FIPS policy may negotiate
Ed25519, while FIPS or stricter site policy may exclude Ed25519, ECDSA, or
both and select a remaining RSA/SHA-2 key. The system OpenSSH client and its
active crypto policy own that choice; Bootwright records exactly the key they
negotiate and strictly reuses it. Do not FIPS-configure the node to work around
this.
