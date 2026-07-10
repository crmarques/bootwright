# Managed-OS SSH trust: host-key re-pinning across reinstalls

SSH-trust traps in the managed-OS install flow
(`machine_os_install_anaconda/tasks/{ssh_trust,wait}.yml`). The FIPS
ssh-keyscan trap (why pinning uses `ssh -o StrictHostKeyChecking=accept-new`,
never `ssh-keyscan`) is in fips-ssh-keyscan-hostkey.md.

**Stale pin after a reinstall — StrictHostKeyChecking deadlocks.**
A freshly installed node regenerates its SSH host keys on first boot, while the
known_hosts trust store is durable across installs, so a key pinned before that
point (this run, or a previous install of the same node) goes stale and strict
checking then fails forever. When this run (re)installed the node, the verify
step re-pins the live host key on every attempt and then requires a key-based
login: a successful login proves we reached the node we provisioned (it holds
the injected authorized key), so the key it just presented is the one to trust.
A node that was already healthy keeps its durable pin untouched — a genuine key
change there must fail loudly, not be silently re-trusted.

**Parallel nodes share one known_hosts.**
`flock` serialises the shared known_hosts file across nodes converging in
parallel; the login itself runs unlocked because `ssh-keygen -R` removes only
this host's entry, so each node's freshly recorded key survives the other
nodes' rewrites.
