# Internal Architecture Map

Bootwright's desired-state pipeline flows through these internal packages:

- `state/desired`: load YAML, strict-decode resources, normalize defaults, and validate ownership and references.
- `state/view`: read-only selectors over a loaded `v1alpha1.State`.
- `state/graph`: resolve shared host services, consumers, host placement, merge fields, and scoped-apply conflicts.
- `infra/support`: dispatch and host-service support registry, including exact Ansible role contracts.
- `render`: deterministically project desired state into installer inputs, Ansible inventory, Ansible vars, manifests, and locks.
- `storage/topology`: storage selectors and topology-derived facts shared by render and storage apply.
- `storage/datafoundation`: Data Foundation external-cluster-details JSON, imported secret loading, and generated credential contracts.
- `storage`: persist runtime storage apply results and Data Foundation attachment records.
- `converge/workflow`: plan and run cross-cluster DAG tasks, leases, ledgers, resource locks, and install records.
- `converge/ansible`: execute rendered Ansible playbooks.
- `converge/bundle`: materialize the embedded Ansible bundle and collection tree.
- `addons`: plan, render, apply, and record cluster-bound post-install addons.
- `runtime`: context state, root-managed filesystem access, secret resolution, local privilege boundaries, and PTY handling.
- `cli`: Cobra commands and user-facing output adapters.

Import direction is one-way: schema and state packages do not import render or converge; storage runtime packages do not import render; render does not import CLI; workflow does not import CLI; all human output stays under `cli/output`.
