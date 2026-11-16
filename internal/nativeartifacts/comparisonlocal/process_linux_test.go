//go:build linux

package comparisonlocal

import (
	"slices"
	"syscall"
	"testing"
	"time"
)

func TestTheHelperRunsTheProcfsPinnedExecutableAsTheAccount(t *testing.T) {
	command := helperCommand(synthetic, []byte("{}"))
	if command.Path != "/proc/self/exe" || !slices.Equal(command.Args, []string{HelperMode}) {
		t.Fatalf("helper command = %q %q", command.Path, command.Args)
	}
	if command.ParentDeath != syscall.SIGKILL || command.Credential == nil {
		t.Fatalf("helper process attributes = %#v", command)
	}
	if credential := command.Credential; credential.Uid != 60001 || credential.Gid != 60002 || !slices.Equal(credential.Groups, []uint32{60002, 60003}) {
		t.Fatalf("helper credential = %#v", credential)
	}
	if !slices.Equal(command.Env, []string{"LANG=C", "LC_ALL=C"}) || command.Dir != "/" || command.Stdin == nil || command.WaitDelay != time.Second {
		t.Fatalf("helper environment = %q in %q", command.Env, command.Dir)
	}
}

func TestTheToolDiesWithItsParentAndDropsToTheAccountOnlyWhenRootServesIt(t *testing.T) {
	environment := toolEnvironment("xterm", "", synthetic.Home)
	for _, delegate := range []bool{true, false} {
		command := toolCommand(identity{account: synthetic, delegate: delegate}, "/usr/bin/meld", "/tmp/bootwright-diff-1", "/tmp/bootwright-diff-1/desired/a", "/tmp/bootwright-diff-1/applied/a", environment)
		if command.ParentDeath != syscall.SIGKILL || !command.Foreground || (command.Credential != nil) != delegate {
			t.Fatalf("delegate %t: attributes %#v", delegate, command)
		}
		if !slices.Equal(command.Env, []string{"TERM=xterm", "HOME=/home/synthetic", "PATH=/usr/local/bin:/usr/bin:/bin"}) {
			t.Fatalf("environment %q", command.Env)
		}
	}
}
