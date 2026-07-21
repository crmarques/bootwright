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

## Known fixtures

- **`env/`** — a parameterized template for a two-cluster bare-metal OpenShift
  estate whose KubeVirt-hosted hubs consume one Bootwright-managed community
  (OSS) Ceph cluster through IBM Fusion Data Foundation external mode. The RHEL
  Ceph nodes install from a bastion-hosted DVD tree (`packageSource.hostedTree`)
  and register to a corporate Red Hat Satellite day-2; the storage cluster itself
  runs upstream Ceph from `quay.io/ceph` with no Ceph subscription. `env.vars`
  holds the identity; `render.sh` expands `template/` into `rendered/`. See
  `examples-wip/env/README.md` for the full build order and secret list.
- **`env/ceph-validate.sh`** — a bastion-run diagnostic that checks a live Ceph
  cluster's desired-vs-actual state via `bootwright cluster exec`, kept beside the
  `env` fixture it targets.

Other short-lived WIP trees (for example a nested OCP-on-OCP fixture) may come
and go here; treat the two above as the durable ones and re-derive anything else
from its own `render.sh`.
