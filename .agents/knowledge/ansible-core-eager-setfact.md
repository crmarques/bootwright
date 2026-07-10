# ansible-core 2.21 eagerly finalizes set_fact Jinja

**Symptom:** A `set_fact` whose value is a Jinja template that embeds another
host's gathered facts (`{{ hostvars[...] }}`, `ansible_*` from a node) resolves
to empty/undefined or the wrong value, even though the same expression works
when evaluated later in the play. The break appeared on the ansible-core 2.21
bump.

**Root cause:** ansible-core 2.21 finalizes `set_fact` values eagerly — the
template is rendered at the moment the task runs, not lazily on first use. If
the node facts the template depends on have not been gathered yet (the gather
task runs later in the play, or on a different host in a `run_once`/`delegate`
flow), the template captures the not-yet-populated values and freezes them.

**Fix:** Order the play so every fact a `set_fact` template embeds is already
gathered before the `set_fact` task runs. Gather node facts first, then
`set_fact` the Jinja that references them; do not rely on lazy evaluation to
defer the resolution until the facts exist.
