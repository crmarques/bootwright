# Git Integration

[code-implementation](../SKILL.md) owns when to commit and what to verify. This
reference covers the mechanics.

## Worktrees and parallel work

- Create one task branch and worktree from the inspected destination and reuse it
  for the whole task, including specs, tests and skill edits. Confirm before the
  first edit that the tree you change is that worktree. An urgent fix, a one-line
  correction and a follow-up after delivery are tasks like any other.
- Parallel workers may share the task worktree when their files and mutable
  resources are disjoint; the coordinator alone stages, commits, rebases, merges
  or changes branches there. Give a worker its own worktree only when overlapping
  edits or mutable build or test resources require isolation, and accept its
  single handoff commit or patch serially.
- Worktrees share one check cache through `scripts/cache-dir`, so a new worktree
  does not rebuild the Ansible tool environment.
- Agent parallelism never authorizes concurrent Bootwright lifecycle execution.

## Contract line

`scripts/check-commits` refuses a commit whose diff touches a contract path
unless its body carries exactly one `Contract:` line, the author's answer to
[whether the change alters what an effect is](go.md#contract-versions-and-content-digests):
`Contract: unchanged`, or `Contract: <implementation>, ...` naming each
implementation whose contract it moved, whose
`cmd/bootwright/testdata/contracts/<implementation>.golden` the same commit
changes. It also refuses `Contract: unchanged` on a commit that changes a
`content-digest:` line of a contract golden. The contract paths, listed in the
script as `contract_paths`, are the collection's `meta/`, `playbooks/`,
`plugins/` and `roles/` under
`ansible/collections/ansible_collections/bootwright/core/`, and the packages
that define a contract version's implementation, request or evidence type, or
a value a content digest hashes: `internal/addons/instance/`,
`internal/addons/clusterobjects/`, `internal/containercluster/agentinstall/`,
`internal/containercluster/networking/`, `internal/controller/clients/`,
`internal/infrastructureservices/` `artifactserver/`, `managedservice/`,
`proxy/`, `dnsserver/` and `ntpserver/`, `internal/machine/power/`,
`internal/managedos/installation/`, `internal/storage/ceph/` and
`internal/substrate/`, each with its subdirectories.
`TestCheckCommitsContractPathsCoverEveryImplementation` holds the list to the
implementations the contract goldens pin.

## Integration variants

- **Local fast-forward (default).** Under
  [D168](../../../../specs/milestones/backlog.md#d168) a slice that passed
  its checkpoint and is feature complete integrates into local main as soon as
  it is done, without asking. Rebase the task branch onto the destination tip,
  rerun `make check` and any safety additions, run
  `scripts/check-commits <destination> HEAD`, and advance the destination by
  fast-forward only. If the destination has
  unrelated uncommitted changes or cannot move safely, keep the tested task
  branch and report the blocker; never bypass it with a merge. This applies
  with or without a remote; pushing still waits for the owner.
- **Pull request.** When the owner or harness asks for one, push the task
  branch, open or update the pull request, and keep CI green. Later fixes are
  new commits on the same branch; do not rewrite pushed history.
- **Cloud session.** The harness-designated branch is the task branch and its
  instructions decide when to push.

Amend only a commit you created for this same change that has not been pushed
or shared and that no unrelated commit follows; otherwise make a new commit.
Squash temporary task history to the minimum coherent commits before
integration, and check the range with `git diff --check` and its log. A
squashed commit carries one `Contract:` line merging those of the commits it
replaces: the implementations any of them named, or `unchanged` when none
named one.

## Cleanup

After the final commit is integrated and its gates pass there, remove each clean
task-created worktree and then its branch, after confirming the final commit
represents its content. Leave unrelated Git state untouched.
