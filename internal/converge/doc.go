// Package converge is the orchestration facade: it resolves command scopes
// to apply/destroy work, selects workflow and task playbooks from the
// internal/roles registry, prepares the embedded Ansible bundle, and hands
// planned task graphs to internal/converge/workflow for execution. It is the
// only package that constructs workflow.RunOptions (guard-tested), so every
// mutating run enters the engine through this facade.
package converge
