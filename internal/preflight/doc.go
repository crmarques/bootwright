// Package preflight checks environmental readiness before mutation: bastion
// tooling, declared secrets, SSH trust records, installer media reachability,
// and storage prerequisites. Checks return plain result data for the CLI to
// present and converge nothing themselves.
package preflight
