# A context owns a copy of its input

**Init copies; the source then floats free.** `context init` copies the source
into the context's `input/` directory, so editing or deleting the source has no
effect until `context update`. `context init` also switches the current context
to the one just created. `context init --yes` over an existing context drops it
entirely (prior state does not survive) — but the replacement source is
validated *before* any destructive step, so an invalid `-f` never drops an
existing context.

**Update replaces wholesale, snapshotting the old input first.**
`context update` (`internal/cli/context_update.go`) routes through the
centralized `workspace.ReplaceInput` so the current input is snapshotted into
the context's input history before being discarded (exactly like
`diff --adopt`), making the pre-update input recoverable. It replaces the input
wholesale but preserves secrets, runs, rendered output, clusters, and the
current-context selection; a declined prompt leaves the existing input
untouched. Update keeps the current pointer, so the registry is not synced back
(unlike `init`/`use`).

**Paths resolve before sudo re-exec.** `context init`/`update` resolve the `-f`
source path *before* any sudo re-exec, so relative paths and `~` expand against
the calling user's environment, not root's; the recorded snapshot source is the
caller's resolved workspace path, so the sudo child receives the original
directory, never a staged copy. `context update` confirms interactively as the
calling user before re-exec (so the prompt reaches a real terminal) and passes
`--yes` to the child.

**All input mutation goes through one component.** Every mutator of a context's
input tree — `context update` (`ReplaceInput`), `diff --adopt` rewriting
individual objects (`ApplyInputEdits`), and any future writer — must go through
`internal/workspace`'s input-mutation component so one rule holds: the prior
input is snapshotted into the input-history sibling directory before any change,
and writes are confined to `InputDir`. History entries are named
`NNNN-<slugified-reason>` (sequence numbers, not wall clock, give chronological
order) and split into `tree/` + `snapshot.yaml`; retention is capped at 20,
pruned oldest-first. `ApplyInputEdits` is additive-only (never removes files,
matching the storage domain's philosophy), resolves every target path first for
all-or-nothing validation, rejects any path escaping `InputDir`, and expects
the caller to render the file bytes itself (e.g. a comment-preserving
`yaml.Node` round-trip) — the component only commits them.

**Crash-safe replace.** `ReplaceInputDir` (`internal/workspace/input.go`) builds
the new input tree in a sibling staging dir under `ctx.BaseDir` (same
filesystem, so installs are renames); the existing tree is renamed aside first
because `rename(2)` cannot replace a populated directory (the aside move frees
the target); the old tree is deleted only after the new one is installed, and
on install failure the aside tree is renamed back. A crash therefore leaves
either the old or the new tree present; the rare interruption between the two
renames is caught by `ValidateInputDir`, whose error names the context and gives
the `context update` / `context init --yes` repopulate remediation.

**A missing owned input is a named error.** A missing or unreadable owned input
directory means a corrupted/half-created context and is a hard, named error
(not the generic "context not ready"): it names the context, the input
directory, and the repopulate remediation. The context-setup checks behind
`bootwright status` are the surface that replaced the retired `context validate`
command. Deleting a context whose shared directory another user already removed
must clean the caller's local registry instead of failing on the missing shared
directory (`ContextBaseDirPresent` distinguishes "no shared files to remove"
from a present-but-unmanaged directory, which deletion still refuses to touch).

**The input copy filter mirrors the loader.** `skipInputDir` (dot directories
including `.git`, plus `node_modules` and `vendor`) must mirror the
desired-state loader's directory traversal — those dirs are never part of the
authored input set. If the loader's traversal rules change, this copy filter
must change in lockstep or the context copy and the loaded set diverge.
