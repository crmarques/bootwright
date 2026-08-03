# Preflight and Hosts the Run Has Not Installed Yet

Symptom:

- `bootwright preflight all` fails with `exit status 4` on an estate that has
  never been applied
- `PLAY RECAP` shows `unreachable=1` for every `storage__<cluster>__<machine>`
  host while `bastion` and `localhost` pass
- The reported failing task is
  `TASK [bootwright.core.check_storage_preflight : Resolve storage nodes on this host]`,
  a task that succeeded on every host that ran it
- The reported failure is `Failed to connect to the host via ssh` for one host,
  usually not the one worth acting on
- `Name resolution` lists every managed-OS storage node but not the provided
  arbiter, even though the arbiter is a topology node

Cause:

`preflight` runs before `apply` by design. Until ADR 0046 the
`check_preflight.yml` "Check host prerequisites" play gathered facts from
`bootwright_storage_hosts` unconditionally, so a storage node with
`os.provided: false` — one whose OS the machines phase installs later in the
same run — had to answer SSH before the run that creates it. Ansible counted
those hosts `dark` and exited `4`.

The reporting compounded it. `summarizeFailure` attributed the failure to the
last `TASK [` banner in the log rather than the banner in force when the
`fatal:` was printed, and reported the first parseable `fatal:` — with several
unreachable hosts, whichever lost the race.

The missing arbiter is a separate gate. `nameResolutionChecks` built lookups
only for a machine referencing a name-resolution entry through
`spec.network.config`, and `validateMachineOS` forbids that field on an
`os.provided: true` machine. A provided node could never be checked.

Investigation:

- `PLAY RECAP` distinguishes the two classes. `ignored=` is a host preflight
  deferred; `unreachable=`/`dark` is one it refused.
- A deferred host prints `is not reachable yet` in
  `<context>/runs/preflight/<scope>/ansible-output.log`; a refused one prints
  `is unreachable at <address> as <user>`.
- `bootwright_os_provided` in `rendered/ansible/inventory.yaml` decides which:
  it is set on storage node entries only, and a host without it defaults to
  must-be-reachable.
- A provided host that answers `ssh-keyscan` (so `SSH host trust` records a key)
  but fails preflight with `Permission denied (publickey…)` has not authorized
  the declared login. `spec.access` omitted on an `os.provided: true` machine
  defaults to `ssh.auth.controllerIdentity` as `root` — the operator installs
  that public key, nothing in the run does.

Related: ADR 0046, ADR 0017, ADR 0035; `internal/preflight/name_resolution.go`;
`internal/converge/ansible/runner.go`.
