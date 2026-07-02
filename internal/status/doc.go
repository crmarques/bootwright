// Package status derives the observe-stage report: context setup checks,
// local readiness, per-cluster installer freshness, add-on record state, and
// the next-step spine. It reads desired state, rendered output, and the
// durable ledgers other packages write; it never mutates provider, cluster,
// or runtime state.
package status
