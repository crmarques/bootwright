# Cluster node SSH access: identity ownership and private-key handoff

**Cluster ownership:** `cluster rsh` and `cluster exec` must not delegate to the
Machine-only SSH path. Canonical OpenShift node Machines intentionally omit
`spec.access.ssh`: the installed node identity is the cluster's normalized
`spec.install.nodeSSH` private material, the login user is `core`, and the
address is the effective agent network's primary IP with the declared node
hostname as fallback. A split `nodeSSH` with only `publicKeyRef` can install but
cannot support direct node access. A managed Ceph `StorageCluster` is the same
shape with different fields: the login is `spec.ceph.cephadm.clusterSSH.user`
and the credential is `clusterSSH.keyRef`, while the address, port and host-key
trust still come from the node `Machine` (ADR 0033). Only an unmanaged
orchestration account (`clusterSSH.user` resolving to `root`) leaves the whole
identity Machine-owned.

**The user name is not the identity.** Every regression in this area has been
one half of a pair moving without the other, and it always presents as
`Permission denied (publickey)` on a node that is otherwise healthy — the
inventory selector that switched `ansible_user` to `cephadm` without offering
`accountPrivateKeyPath` (`670dabaf`), and `machine rsh` resolving the cluster
account name while keeping the machine's key under `IdentitiesOnly=yes`
(ADR 0033). Validation *forbids* `clusterSSH.keyRef` from being the fleet key,
so on any valid configuration the two never coincide and the mismatch is a
guaranteed denial, not an intermittent one. Resolve a login and its credential
as one value; never set one field.

**Encrypted material:** Context-local and generated secret files are encrypted
envelopes, not files OpenSSH can consume. Direct SSH commands read the selected
private material through `secret.Resolver`, create a `0600` temporary file,
unlink its empty name before writing the decrypted bytes, and keep the
descriptor open while SSH reads `/proc/<bootwright-pid>/fd/<fd>`. This prevents
both ciphertext handoff and stale named plaintext after interruption. An
explicit known-hosts Secret uses the same parent-held descriptor pattern with
strict checking; managed trust stays in the context known-hosts file.
`IdentityFile=none` resets configured default identities before the selected
key is added. Direct access copies only an allowlist of cryptographic directives
from `/etc/crypto-policies/back-ends/openssh.config` into an anonymous SSH
configuration when that Red Hat policy backend exists; otherwise OpenSSH's
compiled crypto defaults apply. This retains Red Hat and FIPS crypto policy
without accepting identity, certificate, command, forwarding, or root-personal
host rules, and disables agent and non-public-key authentication fallback. Its
environment is an allowlist of terminal, locale, time-zone, and color values,
preventing caller-controlled askpass helpers, agents, dynamic loaders, or
crypto-provider configuration from executing across the root boundary.

**Process boundary:** Direct SSH runs as a waited child rather than replacing
Bootwright with `syscall.Exec`. Standard input, output, and error remain attached
to the terminal, cancellation first sends the child a termination signal with
a bounded wait before forced cleanup, and the child exit status or conventional
signal status is returned. Waiting is what lets Bootwright close the anonymous
secret descriptors after an interactive shell or one-off command ends.

**Host trust:** Managed direct access uses the context known-hosts file with
`StrictHostKeyChecking=ask` and unhashed host entries. An unknown host therefore
requires OpenSSH's explicit interactive confirmation and fails in
non-interactive use; a changed key remains a hard failure.
`HostKeyAlias=<effective-address>` with IP and DNS trust, global trust, and
host-key updates disabled keeps the on-disk lookup name deterministic. After
out-of-band verification of a changed container-node key, remove that exact
address with `ssh-keygen -R -f <context-known-hosts>` and reconnect
interactively; `machine trust` does not project cluster-owned node endpoints.
