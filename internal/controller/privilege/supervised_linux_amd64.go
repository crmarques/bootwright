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
	_, found := supervisor(proc, parent, sudo, self)
	return found
}

// supervisor returns the process ID of the supervisor supervisedBy finds.
func supervisor(proc string, parent int, sudo, self string) (int, bool) {
	sudoFile, err := os.Stat(sudo)
	if err != nil {
		return 0, false
	}
	selfFile, err := os.Stat(self)
	if err != nil {
		return 0, false
	}
	pid := parent
	for hops := 0; ; hops++ {
		executable, err := os.Stat(proc + "/" + strconv.Itoa(pid) + "/exe")
		if err != nil {
			return 0, false
		}
		if !os.SameFile(executable, sudoFile) {
			return pid, hops != 0 && os.SameFile(executable, selfFile)
		}
		if hops == maxSudoHops {
			return 0, false
		}
		var found bool
		if pid, found = parentProcess(proc, pid); !found {
			return 0, false
		}
	}
}

// ForegroundOfOwnTerminal reports whether this process is a supervised child
// that is the foreground process group of a terminal other than its
// supervisor's, as sudo's use_pty leaves an interactive invocation. There each
// operator interrupt reaches the child once, from sudo's relay alone.
func ForegroundOfOwnTerminal() bool {
	sudo, err := QualifiedSudo()
	if err != nil {
		return false
	}
	return foregroundOfOwnTerminal("/proc", os.Getpid(), os.Getppid(), sudo, "/proc/self/exe")
}

func foregroundOfOwnTerminal(proc string, pid, parent int, sudo, self string) bool {
	supervising, found := supervisor(proc, parent, sudo, self)
	if !found {
		return false
	}
	own, found := terminalOf(proc, pid)
	if !found || own.tty == 0 || own.foreground != own.group {
		return false
	}
	theirs, found := terminalOf(proc, supervising)
	return found && own.tty != theirs.tty
}

type processTerminal struct{ group, tty, foreground int }

// terminalOf reads the process group, the controlling terminal and that
// terminal's foreground process group, the third, fifth and sixth fields after
// the last closing parenthesis of procfs stat (proc_pid_stat(5)).
func terminalOf(proc string, pid int) (processTerminal, bool) {
	fields, found := processStat(proc, pid)
	if !found || len(fields) < 6 {
		return processTerminal{}, false
	}
	var values [3]int
	for i, index := range []int{2, 4, 5} {
		value, err := strconv.Atoi(string(fields[index]))
		if err != nil {
			return processTerminal{}, false
		}
		values[i] = value
	}
	return processTerminal{group: values[0], tty: values[1], foreground: values[2]}, true
}

// parentProcess reads the parent process ID, the second field after the last
// closing parenthesis of procfs stat; the command name before it may itself
// contain parentheses and spaces.
func parentProcess(proc string, pid int) (int, bool) {
	fields, found := processStat(proc, pid)
	if !found || len(fields) < 2 {
		return 0, false
	}
	parent, err := strconv.Atoi(string(fields[1]))
	if err != nil || parent <= 0 {
		return 0, false
	}
	return parent, true
}

func processStat(proc string, pid int) ([][]byte, bool) {
	file, err := os.Open(proc + "/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return nil, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxProcessStats+1))
	if err != nil || len(data) > maxProcessStats {
		return nil, false
	}
	end := bytes.LastIndexByte(data, ')')
	if end < 0 {
		return nil, false
	}
	return bytes.Fields(data[end+1:]), true
}
