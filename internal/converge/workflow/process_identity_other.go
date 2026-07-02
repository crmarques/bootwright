//go:build !linux

package workflow

// processStartToken has no portable non-Linux implementation, so process
// identity cannot be verified and the caller falls back to the heartbeat-age
// staleness rule (see localLeaseProcessAlive). This is the conservative
// direction: a lease whose identity cannot be confirmed is never treated as
// immortally alive.
func processStartToken(pid int) (string, bool) {
	return "", false
}
