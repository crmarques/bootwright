//go:build linux && amd64

package privilege

import (
	"bytes"
	"io"
	"os"
	"strconv"
)

const (
	// maxSudoHops is sudo itself and the monitor it forks to run the command
	// in a pseudo-terminal (sudo(8), "Process model").
	maxSudoHops     = 2
	maxProcessStats = 4096
)

// SupervisedChild reports whether this process is the elevated child of a
// Bootwright supervisor: its parent is the qualified sudo, directly or through
// sudo's monitor, and the process above sudo runs this same executable, which
// the supervisor handed sudo as its own procfs executable.
func SupervisedChild() bool {
	sudo, err := QualifiedSudo()
	if err != nil {
		return false
	}
	return supervisedBy("/proc", os.Getppid(), sudo, "/proc/self/exe")
}

// supervisedBy walks from parent through at most maxSudoHops processes whose
// executable is sudo, needing at least one, and reports whether the next one
// runs self. Any process it cannot read proves nothing.
func supervisedBy(proc string, parent int, sudo, self string) bool {
	sudoFile, err := os.Stat(sudo)
	if err != nil {
		return false
	}
	selfFile, err := os.Stat(self)
	if err != nil {
		return false
	}
	pid := parent
	for hops := 0; ; hops++ {
		executable, err := os.Stat(proc + "/" + strconv.Itoa(pid) + "/exe")
		if err != nil {
			return false
		}
		if !os.SameFile(executable, sudoFile) {
			return hops != 0 && os.SameFile(executable, selfFile)
		}
		if hops == maxSudoHops {
			return false
		}
		var found bool
		if pid, found = parentProcess(proc, pid); !found {
			return false
		}
	}
}

// parentProcess reads the parent process ID, the second field after the last
// closing parenthesis of procfs stat; the command name before it may itself
// contain parentheses and spaces.
func parentProcess(proc string, pid int) (int, bool) {
	file, err := os.Open(proc + "/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxProcessStats+1))
	if err != nil || len(data) > maxProcessStats {
		return 0, false
	}
	end := bytes.LastIndexByte(data, ')')
	if end < 0 {
		return 0, false
	}
	fields := bytes.Fields(data[end+1:])
	if len(fields) < 2 {
		return 0, false
	}
	parent, err := strconv.Atoi(string(fields[1]))
	if err != nil || parent <= 0 {
		return 0, false
	}
	return parent, true
}
