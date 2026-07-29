# Gitignored work-in-progress fixtures live under `examples-wip/`

**What it is:** `examples-wip/` is a git-ignored sibling of `examples/` (see the
`.gitignore` entry) that holds parameterized, work-in-progress input trees used
to reproduce real estates without committing their identity. It is deliberately
kept out of the repo: the trees embed environment-specific values, and they are
too rough or too special-purpose to ship as canonical `examples/`. Because it is
git-ignored, **this file is the only in-repo record that it exists** — losing the
working copy or the machine loses the fixtures unless they are regenerated.

The example-scoped repo checks skip `examples-wip/` precisely because it is not
under `examples/`; do not move a WIP tree into `examples/` until it is
identity-free and validates clean, or the canonical-example guards will gate it.

## Convention

Each fixture is **self-regenerating**: it carries a values file plus a
`render.sh` that expands a placeholder `template/` tree into a `rendered/`
directory, and only `rendered/` is what `bootwright` consumes. The template is
generic and identity-free; the values file is the only thing carrying real
domains, IPs, MACs, VLANs, and hostnames. `render.sh` substitutes literally (not
via regex) and fails if a template uses a `{{VAR}}` the values file does not
declare, warning if a declared variable is unused — so the template/values
contract cannot silently drift.

To regenerate a fixture's consumable tree:

```sh
cd examples-wip/<fixture>
./render.sh                    # values + template/ -> rendered/
bin/bootwright validate -f examples-wip/<fixture>/rendered
```

`render.sh` accepts an alternate values file and output directory
(`./render.sh dc3.vars /tmp/dc3`) to render a second environment from the same
template.

No per-fixture inventory is kept here. The repo cannot detect when such a list
goes stale — it already had — so a fixture's identity, build order, and secret
list belong in that fixture's own `README.md`, beside the `render.sh` that
regenerates it.
