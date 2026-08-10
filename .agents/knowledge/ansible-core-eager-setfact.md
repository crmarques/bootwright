# Ansible eagerly finalizes nested set_fact Jinja

**Symptom:** A `set_fact` assigning a mapping that contains Jinja backed by
ungathered host facts fails with an undefined-variable error, or freezes an
empty or wrong value, even though the same expression works when evaluated
later in the play. The Ceph form is `'ansible_distribution_major_version' is
undefined` while resolving `bootwright_ceph_provider`. The exact behavior
reproduces on ansible-core 2.20.4; it is not specific to a 2.21 bump.

**Root cause:** Ansible finalizes `set_fact` values eagerly — the template is
rendered at the moment the task runs, not lazily on first use. Assigning a whole
mapping recursively materializes nested expressions in unrelated branches. A
leaf lookup that contains no unresolved Jinja remains safe, so extracting
`provider.image` succeeds while assigning the provider that also carries
templated repository ids fails before distribution facts exist.

**Fix:** Order the play so every fact a `set_fact` template embeds is already
gathered before the complete value is assigned. When an earlier gate needs only
a fact-independent leaf, extract that scalar and defer materializing the full
mapping until after its facts are gathered. Do not rely on lazy evaluation to
defer resolution until first use.
