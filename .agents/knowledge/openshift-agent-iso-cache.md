# openshift-install reuses stale agent ISO cache

**Symptom:** Re-running `bootwright apply clusters` or `bootwright apply container-cluster` after generated `install-config.yaml` or `agent-config.yaml` changed fails with `cannot generate ISO image due to configuration errors`, or the agent ISO does not reflect the latest rendered inputs.

**Root cause:** `openshift-install` caches asset state in `.openshift_install_state.json`. After a successful run, the cached Agent Installer ISO asset can be replayed even when Bootwright rewrote the config files.

**Fix:** When the effective install or agent config changes, remove `.openshift_install_state.json` and generated ISO outputs before running `openshift-install agent create image` again.
