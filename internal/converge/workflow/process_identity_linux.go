//go:build linux

package workflow

import (
	"os"
	"strconv"
	"strings"
)

// processStartToken returns a stable identity token for a live PID: its start
// time (clock ticks since boot) from /proc/<pid>/stat field 22. A process is
// uniquely identified by (PID, start time) across the machine's uptime, so a
// lease can distinguish its own still-running process from an unrelated process
// that later reused the same PID. Returns ok=false when the process is gone or
// /proc is unreadable, so the caller falls back to the heartbeat-age rule rather
// than trusting a bare PID.
func processStartToken(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", false
	}
	stat := string(data)
	// Field 2 (comm) is parenthesized and may itself contain spaces or ')',
	// so skip past the last ')' before splitting the remaining space-separated
	// fields, which then line up with the documented proc(5) layout.
	end := strings.LastIndexByte(stat, ')')
	if end < 0 || end+2 >= len(stat) {
		return "", false
	}
	fields := strings.Fields(stat[end+2:])
	// After comm, fields[0] is field 3 (state); starttime is field 22, i.e.
	// index 22-3 = 19 in the post-comm slice.
	const starttimeIndex = 19
	if len(fields) <= starttimeIndex {
		return "", false
	}
	return fields[starttimeIndex], true
}
