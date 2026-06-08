// Package stateview provides stateless pure lookups and derivations over a
// v1alpha1.State, including cross-kind joins. It does not filter or scope the
// State; selection and the machine-service graph live in internal/state/graph,
// which builds on this package.
package stateview
