# Ansible dynamic role dispatch: imported role sees undefined vars

**Symptom:** A play fails at parse time with `bootwright_current_cluster is undefined`, `bootwright_current_provider is undefined`, or a dynamic role name such as `cluster_substrate_{{ ... }}` cannot resolve.

**Root cause:** `import_role` is evaluated while Ansible parses the play. Bootwright provider and substrate role names are derived from facts set in `pre_tasks`, so the role name exists only at task runtime.

**Fix:** Use `include_role` when the role name contains a runtime fact. Keep explicit no-op roles such as `provider_bmc_none` and `ocp_boot_none` so every structural provider case resolves to a real role.

