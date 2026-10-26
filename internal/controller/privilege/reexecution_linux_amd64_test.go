//go:build linux && amd64

package privilege

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// rootOwnedSystemFile is an executable root installed below directories root
// owns on every supported host, standing in for an installed Bootwright.
const rootOwnedSystemFile = "/usr/bin/env"

// A file root owns below directories root owns is re-executed by its absolute
// path, which a sudoers rule naming the installed binary matches; the invoking
// account's own build output goes through its procfs executable.
func TestARootOwnedInstallReExecutesByItsPath(t *testing.T) {
	if !rootOwned(rootOwnedSystemFile) {
		t.Skip(rootOwnedSystemFile + " is not root-owned on this host")
	}
	if got := reexecution(rootOwnedSystemFile, 4242); got != rootOwnedSystemFile {
		t.Fatalf("a root-owned install re-executes through %q", got)
	}
	build := filepath.Join(t.TempDir(), "bootwright")
	if err := os.WriteFile(build, []byte("#!"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := reexecution(build, 4242); got != "/proc/4242/exe" {
		t.Fatalf("an account's own build re-executes through %q", got)
	}
}

// The supervised child refuses a variable neither sudo, a PAM session nor the
// route sets, naming it, a file other than the one its supervisor runs, and a
// path re-execution sudo was not named by the root-owned path of that file.
func TestTheElevatedChildRefusesAForeignVariableAndAnotherFile(t *testing.T) {
	sudoSet := []string{"HOME=/root", "LOGNAME=root", "USER=root", "MAIL=/var/mail/root", "SHELL=/bin/bash", "PATH=/usr/sbin:/usr/bin",
		"TERM=xterm", "LANG=C", "LC_ALL=C", "SUDO_USER=operator", "SUDO_UID=1001", "SUDO_GID=1001", "XDG_SESSION_ID=3"}
	procfs := append([]string{"SUDO_COMMAND=/proc/4242/exe status", "HTTPS_PROXY=http://proxy:3128", "NO_PROXY=.lab"}, sudoSet...)
	if refusal := confirmChild(procfs, "/proc/self/exe", "/proc/self/exe"); refusal != nil {
		t.Fatalf("a procfs re-execution with sudo's and the route's variables refused: %+v", refusal)
	}
	for _, foreign := range []string{"GODEBUG=x509sha1=1", "LD_LIBRARY_PATH=/tmp", "NO_COLOR=1", "XDG_CONFIG_HOME=/tmp"} {
		refusal := confirmChild(append([]string{foreign}, procfs...), "/proc/self/exe", "/proc/self/exe")
		name, _, _ := strings.Cut(foreign, "=")
		if refusal == nil || refusal.Code != "runtime.privilege" || !strings.Contains(refusal.Message, "variable "+name+",") || strings.Contains(refusal.Message, "=") {
			t.Fatalf("%s: refusal = %+v", foreign, refusal)
		}
	}
	other := filepath.Join(t.TempDir(), "other")
	if err := os.WriteFile(other, []byte("#!"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, supervising := range map[string]string{"no proved supervisor": "", "a supervisor running another file": other} {
		if refusal := confirmChild(procfs, "/proc/self/exe", supervising); refusal == nil || refusal.Code != "runtime.privilege" {
			t.Fatalf("%s: refusal = %+v", name, refusal)
		}
	}
	if !rootOwned(rootOwnedSystemFile) {
		t.Skip(rootOwnedSystemFile + " is not root-owned on this host")
	}
	installed := append([]string{"SUDO_COMMAND=" + rootOwnedSystemFile + " status"}, sudoSet...)
	if refusal := confirmChild(installed, rootOwnedSystemFile, rootOwnedSystemFile); refusal != nil {
		t.Fatalf("the installed binary refused itself: %+v", refusal)
	}
	for name, test := range map[string]struct{ command, self, supervising string }{
		"another file than sudo ran":  {rootOwnedSystemFile, other, other},
		"another file than it runs":   {rootOwnedSystemFile, rootOwnedSystemFile, other},
		"a file root does not own":    {other, other, other},
		"a relative path":             {"bootwright", rootOwnedSystemFile, rootOwnedSystemFile},
		"a path cut short by a space": {"/usr/bin/e nv", rootOwnedSystemFile, rootOwnedSystemFile},
	} {
		if refusal := confirmChild(append([]string{"SUDO_COMMAND=" + test.command + " status"}, sudoSet...), test.self, test.supervising); refusal == nil || refusal.Code != "runtime.privilege" {
			t.Fatalf("%s: refusal = %+v", name, refusal)
		}
	}
}

// Every root invocation sudo started, run by hand as much as supervised,
// refuses a variable sudo, a PAM session or the route did not set, before it
// unsets anything; admitted session variables are unset before any child
// starts, and a root process sudo did not start is left alone.
func TestEverySudoStartedInvocationRefusesAForeignVariableAndDropsTheSession(t *testing.T) {
	session := []string{"KRB5CCNAME=KCM:", "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/0/bus", "XDG_RUNTIME_DIR=/run/user/0",
		"XDG_SESSION_CLASS=user", "XDG_SESSION_TYPE=tty", "XDG_SEAT=seat0", "XDG_VTNR=2"}
	environ := append([]string{"SUDO_COMMAND=/usr/local/bin/bootwright apply", "SUDO_USER=operator", "PATH=/usr/bin", "LANG=C"}, session...)
	var unset []string
	record := func(name string) error { unset = append(unset, name); return nil }
	if refusal := confirmSudoEnvironment(environ, true, record); refusal != nil {
		t.Fatalf("sudo's and the session's variables refused: %+v", refusal)
	}
	var want []string
	for _, entry := range session {
		name, _, _ := strings.Cut(entry, "=")
		want = append(want, name)
	}
	if !slices.Equal(unset, want) {
		t.Fatalf("unset %v, want the session's %v", unset, want)
	}
	for _, foreign := range []string{"GODEBUG=x509sha1=1", "ANSIBLE_CONFIG=/tmp/a.cfg", "XDG_DATA_DIRS=/tmp"} {
		unset = nil
		refusal := confirmSudoEnvironment(append(slices.Clone(environ), foreign), true, record)
		name, _, _ := strings.Cut(foreign, "=")
		if refusal == nil || !strings.Contains(refusal.Message, "variable "+name+",") || !strings.Contains(refusal.Remediation, "/etc/environment") ||
			!strings.Contains(refusal.Remediation, "pam_env") || len(unset) != 0 {
			t.Fatalf("%s: refusal = %+v, unset %v", foreign, refusal, unset)
		}
	}
	unset = nil
	if refusal := confirmSudoEnvironment(append(slices.Clone(environ), "GODEBUG=x509sha1=1"), false, record); refusal != nil || len(unset) != 0 {
		t.Fatalf("a root process sudo did not start was held: %+v, unset %v", refusal, unset)
	}
}
