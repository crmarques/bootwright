// Package storage persists managed-Ceph and Data Foundation apply results under
// the current Bootwright context. The other apply-result owners are
// internal/converge/workflow (the durable run ledger and per-cluster install
// state) and internal/addons/records (add-on apply state).
package storage
