//go:build linux && amd64

package privilege

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type fakeProcess struct {
	pid       int
	exe, stat string
}

func process(pid int, exe string, parent int) fakeProcess {
	return onTerminal(pid, exe, parent, pid, 0, -1)
}

// onTerminal writes the process group, controlling terminal and that
// terminal's foreground process group into the stat (proc_pid_stat(5)).
func onTerminal(pid int, exe string, parent, group, tty, foreground int) fakeProcess {
	return fakeProcess{pid: pid, exe: exe, stat: strconv.Itoa(pid) + " (" + exe + ") S " + strconv.Itoa(parent) + " " + strconv.Itoa(group) + " " + strconv.Itoa(pid) + " " + strconv.Itoa(tty) + " " + strconv.Itoa(foreground) + " 4194560\n"}
}

// procTree lays out the executables and a procfs of the given processes. An
// empty exe or stat leaves that entry missing.
func procTree(t *testing.T, processes ...fakeProcess) (proc, sudo, self string) {
	t.Helper()
	root := t.TempDir()
	binaries := filepath.Join(root, "bin")
	if err := os.Mkdir(binaries, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sudo", "bootwright", "bash"} {
		if err := os.WriteFile(filepath.Join(binaries, name), []byte(name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	proc = filepath.Join(root, "proc")
	for _, p := range processes {
		directory := filepath.Join(proc, strconv.Itoa(p.pid))
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if p.exe != "" {
			if err := os.Symlink(filepath.Join(binaries, p.exe), filepath.Join(directory, "exe")); err != nil {
				t.Fatal(err)
			}
		}
		if p.stat != "" {
			if err := os.WriteFile(filepath.Join(directory, "stat"), []byte(p.stat), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return proc, filepath.Join(binaries, "sudo"), filepath.Join(binaries, "bootwright")
}

func TestSupervisedChildRecognizesOnlyItsOwnSupervisor(t *testing.T) {
	supervisor := process(10, "bootwright", 1)
	for _, test := range []struct {
		name      string
		parent    int
		processes []fakeProcess
		want      bool
	}{
		{"through sudo and its monitor", 30, []fakeProcess{process(30, "sudo", 20), process(20, "sudo", 10), supervisor}, true},
		{"through sudo alone", 20, []fakeProcess{process(20, "sudo", 10), supervisor}, true},
		{"sudo started from a shell", 20, []fakeProcess{process(20, "sudo", 11), process(11, "bash", 1)}, false},
		{"a parent that is not sudo", 10, []fakeProcess{supervisor}, false},
		{"three sudo hops", 40, []fakeProcess{process(40, "sudo", 30), process(30, "sudo", 20), process(20, "sudo", 10), supervisor}, false},
		{"a missing executable", 20, []fakeProcess{process(20, "sudo", 10), {pid: 10, stat: supervisor.stat}}, false},
		{"a missing stat", 30, []fakeProcess{process(30, "sudo", 20), {pid: 20, exe: "sudo"}, supervisor}, false},
		{"a missing parent", 20, nil, false},
		{"a command name holding a parenthesis", 20, []fakeProcess{{pid: 20, exe: "sudo", stat: "20 (x) 1 (y) S 10 20 20 0 -1 4194560\n"}, supervisor}, true},
		{"an oversized stat", 20, []fakeProcess{{pid: 20, exe: "sudo", stat: "20 (sudo) S 10 " + strings.Repeat("0 ", maxProcessStats) + "\n"}, supervisor}, false},
		{"an unparsable parent", 20, []fakeProcess{{pid: 20, exe: "sudo", stat: "20 (sudo) S ten\n"}, supervisor}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			proc, sudo, self := procTree(t, test.processes...)
			if got := supervisedBy(proc, test.parent, sudo, self); got != test.want {
				t.Fatalf("supervised = %v, want %v", got, test.want)
			}
		})
	}
}

func TestTheChildEscalatesOnlyInTheForegroundOfAPseudoTerminalOfItsOwn(t *testing.T) {
	const supervisorTerminal, ownTerminal = 34816, 34817
	supervisor := onTerminal(10, "bootwright", 1, 10, supervisorTerminal, 10)
	sudo := onTerminal(20, "sudo", 10, 10, supervisorTerminal, 10)
	monitor := onTerminal(30, "sudo", 20, 30, ownTerminal, 40)
	for _, test := range []struct {
		name      string
		processes []fakeProcess
		want      bool
	}{
		{"the foreground of its own pseudo-terminal", []fakeProcess{onTerminal(40, "bootwright", 30, 40, ownTerminal, 40), monitor, sudo, supervisor}, true},
		{"on the supervisor's terminal", []fakeProcess{onTerminal(40, "bootwright", 30, 40, supervisorTerminal, 40), monitor, sudo, supervisor}, false},
		{"in the background of its own pseudo-terminal", []fakeProcess{onTerminal(40, "bootwright", 30, 40, ownTerminal, 30), monitor, sudo, supervisor}, false},
		{"with no terminal", []fakeProcess{onTerminal(40, "bootwright", 30, 40, 0, 40), monitor, sudo, supervisor}, false},
		{"with an unreadable supervisor stat", []fakeProcess{onTerminal(40, "bootwright", 30, 40, ownTerminal, 40), monitor, sudo, {pid: 10, exe: "bootwright"}}, false},
		{"under a parent that is not sudo", []fakeProcess{onTerminal(40, "bootwright", 30, 40, ownTerminal, 40), onTerminal(30, "bash", 10, 30, ownTerminal, 40), supervisor}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			proc, sudoPath, self := procTree(t, test.processes...)
			if got := foregroundOfOwnTerminal(proc, 40, 30, sudoPath, self); got != test.want {
				t.Fatalf("foreground of its own terminal = %v, want %v", got, test.want)
			}
		})
	}
}
