// Package workflow owns convergence orchestration: planning the cross-cluster
// apply/destroy task graph (activity_graph.go, apply_plan.go), scheduling tasks
// under run leases and resource locks (apply_scheduler.go, apply_runner.go),
// persisting durable run ledgers, per-cluster install state, and
// converge-safety records (ledger.go, install_state.go, apply_classify.go),
// and reconciling recorded evidence against desired state for skip, resume,
// state-check, and orphan reporting. Ansible executes the tasks; this package
// decides what runs, in which order, and what is recorded.
package workflow
