# ansible-galaxy metadata resolver fails with 'results'

**Symptom:** `make build` or Docker `RUN make build` fails while installing
embedded collections with `Skipping Galaxy server`, `Unexpected Exception`, or
`'results'`, often while resolving `community.general`.

**Root cause:** Some `ansible-galaxy` versions fail against current Galaxy
metadata responses before artifact download. Exact version pins are not enough
because the resolver still asks Galaxy for available versions.

**Fix:** Install exact Galaxy artifact URLs from
`ansible/collections/requirements.yml` with `--no-deps`, and set the install
path as `ANSIBLE_COLLECTIONS_PATH`/`ANSIBLE_COLLECTIONS_PATHS`. Add any new
transitive collection as an explicit artifact URL instead of relying on Galaxy
dependency resolution during the build.
