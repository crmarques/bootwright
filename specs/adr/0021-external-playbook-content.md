# ADR 0021: External Playbook Content

## Status

Accepted

## Context

ADR 0005 established that operator Ansible content is **vendored**: it sits beside
the object and travels with the input tree through `context init`'s copy. That
made every phase after `context update` offline, and it deferred Galaxy
`requirements.yml` installs as network-dependent and air-gap-hostile.

Vendoring is a poor fit for content the operator already maintains elsewhere. A
site with one hardening repository shared by several environments had to copy a
sanitised subset into every input tree by hand, because a real Ansible repository
cannot simply be dropped in: the loader decodes every `.yaml`/`.yml` it walks, so
`group_vars/`, `molecule/`, and `tests/` files fail strict decode.

## Decision

`CustomPlaybook.spec.source` names where the Ansible content lives. Two arms:

- `path` — an absolute directory outside the input tree, used as-is.
- `git` — a repository (`https`, `ssh`, `file://`, or an absolute local path) at
  a `ref`, with an optional `subdir` and `secretRef`.

`playbook`, `rolesPath`, and `collectionsPath` resolve against the resolved root
instead of the object's directory, under the same containment rules. The
`playbooks/` directory rule does not apply to external content, because the
loader never walks it.

**Only `apply` fetches.** `plan`, `diff`, and `destroy` omit git-sourced
playbooks from their view rather than reaching the network. Content lands under
the run directory, keyed by resolved commit, so repeated objects sharing a
repository clone once.

**Authentication is explicit.** `secretRef` names a `Secret` whose type matches
the transport — `sshKeyPair` for `ssh`, `token` or `usernamePassword` for
`https`. Credentials reach `git` through a temporary `GIT_ASKPASS` helper or
`GIT_SSH_COMMAND`, never on argv, consistent with ADR 0005's rule that secrets do
not appear in process arguments. Nothing is inherited from the operator's
ssh-agent or `~/.gitconfig`, so a run behaves the same under `sudo` as without.

Fetches run with `core.hooksPath=/dev/null`, `protocol.ext.allow=never`, and
`--no-recurse-submodules`; `http://` URLs and transport helpers are rejected.

`source.git` is not available on `ClusterAddon.spec.steps[]`. A step's content is
part of its add-on package and is covered by the add-on's own content digest.

## Consequences

- ADR 0005's "vendored trees are the only supported delivery" no longer holds.
  Its deferral of **Galaxy `requirements.yml` installs in the apply path** stands
  unchanged; this ADR adds a distinct, pinned, operator-declared fetch.
- `apply` gains a network dependency for remote git sources, and `git` becomes a
  conditional preflight requirement when any object declares one. Air-gapped
  sites use a local repository (`url: /srv/git/…`), which resolves and checks out
  offline while still pinning a tag or commit — or `source.path`.
- A branch `ref` is permitted. Because `run: onChange` digests the fetched
  content, a playbook re-runs whenever that branch advances, with no change in
  the input files. Pinning a tag or commit keeps re-runs driven by the operator's
  own edits.
- External content is not part of the context snapshot, so `context init`'s
  self-contained guarantee covers the input tree only. A `source.path` directory
  must remain readable on the controller; a git source must remain reachable.
- A reviewer approving a `ref` bump approves bytes that are not visible in that
  diff.
