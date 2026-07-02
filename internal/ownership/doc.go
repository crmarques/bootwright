// Package ownership reads and interprets the durable per-host resource and
// package ownership records that executing collection roles write through
// bootwright.core.ownership_record. Destroy scoping, host package removal
// gating, orphan reporting, and state-check all decide from these records:
// Bootwright tears down only what a record proves it created or configured.
package ownership
