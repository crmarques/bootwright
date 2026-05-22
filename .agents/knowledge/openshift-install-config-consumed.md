# OpenShift Agent Install Config Consumed

## Symptom

Split agent apply reaches a node boot task and fails in `install_agent` with:

```text
Missing install-config.yaml at <state-dir>/runtime/<cluster>/installer/install-config.yaml
```

The preceding `create-agent-iso` task may have completed successfully.

## Likely Cause

`openshift-install agent create image --dir <work-dir>` consumes installer
input into its state cache. Later split tasks must use generated runtime state
such as `agent-iso.path` and `.openshift_install_state.json`; they must not
require `install-config.yaml` to still exist after ISO generation.

## Fix

Keep `install-config.yaml` validation scoped to actions that create the ISO
(`run` and `create_iso`). For `boot_machine`, validate the recorded
`agent-iso.path` produced by the ISO task instead.
