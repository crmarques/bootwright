//go:build linux && amd64

package privilege

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestSudoDiscardsOnlyTheExactTerminalColorHint(t *testing.T) {
	for _, supervised := range []bool{false, true} {
		for _, value := range []string{"", "truecolor", "24bit", "GODEBUG=x509sha1=1\nANSIBLE_CONFIG=/tmp/foreign", "\x1b[31m"} {
			environ := []string{"SUDO_COMMAND=/proc/4242/exe version", "PATH=/usr/bin", "TERM=xterm", "COLORTERM=" + value}
			if supervised {
				if refusal := confirmChild(environ, "/proc/self/exe", "/proc/self/exe"); refusal != nil {
					t.Fatalf("supervised hint refused: %+v", refusal)
				}
			}
			var removed []string
			if refusal := confirmSudoEnvironment(environ, true, func(name string) error {
				removed = append(removed, name)
				return nil
			}); refusal != nil {
				t.Fatalf("supervised %t: hint refused: %+v", supervised, refusal)
			}
			if !slices.Equal(removed, []string{"COLORTERM"}) {
				t.Fatalf("supervised %t: removed %v, want only COLORTERM", supervised, removed)
			}
		}
	}
}

func TestSudoColorHintDoesNotAdmitForeignVariables(t *testing.T) {
	for _, foreign := range []string{"GODEBUG=x509sha1=1", "ANSIBLE_CONFIG=/tmp/foreign", "COLORTERM_EXTRA=truecolor", "colorterm=truecolor", "NO_COLOR=1"} {
		for _, hintFirst := range []bool{false, true} {
			environ := []string{"SUDO_COMMAND=/proc/4242/exe version", foreign, "COLORTERM=truecolor"}
			if hintFirst {
				environ[1], environ[2] = environ[2], environ[1]
			}
			name, _, _ := strings.Cut(foreign, "=")
			refusal := confirmChild(environ, "/proc/self/exe", "/proc/self/exe")
			if refusal == nil || refusal.Code != "runtime.privilege" || !strings.Contains(refusal.Message, "variable "+name+",") {
				t.Fatalf("supervised entry: %s refusal = %+v", name, refusal)
			}
			removed := false
			refusal = confirmSudoEnvironment(environ, true, func(string) error { removed = true; return nil })
			if refusal == nil || refusal.Code != "runtime.privilege" || !strings.Contains(refusal.Message, "variable "+name+",") || removed {
				t.Fatalf("sudo entry: %s refusal = %+v, removed %t", name, refusal, removed)
			}
		}
	}
}

func TestSudoRefusesATerminalColorHintItCannotRemove(t *testing.T) {
	refusal := confirmSudoEnvironment([]string{"COLORTERM=private-hint"}, true, func(string) error {
		return errors.New("private-error")
	})
	if refusal == nil || refusal.Code != "runtime.privilege" || !strings.Contains(refusal.Message, "COLORTERM cannot be removed") ||
		strings.Contains(refusal.Message, "private") {
		t.Fatalf("unset failure = %+v", refusal)
	}
	if refusal := confirmSudoEnvironment([]string{"COLORTERM=truecolor"}, false, func(string) error {
		t.Fatal("a process without a proved sudo parent changed its environment")
		return nil
	}); refusal != nil {
		t.Fatalf("a process without a proved sudo parent refused: %+v", refusal)
	}
}

func TestSudoColorHintDoesNotReachSubprocesses(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "truecolor"} {
		child := exec.Command(self, "__bootwright_discard_terminal_hint")
		child.Env = []string{"PATH=/usr/bin", "COLORTERM=" + value}
		output, err := child.CombinedOutput()
		if err != nil || string(output) != "hint absent\n" {
			t.Fatalf("guarded subprocess: %v, output %q", err, output)
		}
	}
}
