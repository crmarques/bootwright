// Package nmstate deterministically merges NetworkConfig NMState templates
// with per-machine overrides and injects resolved interface addresses. The
// merge rules (deep map merge, by-name list merge, positional fallback,
// rejected mixed shapes) are specified in specs/state-model.md under
// NetworkConfig; validation and rendering share this one implementation.
package nmstate
