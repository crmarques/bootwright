//go:build linux

package comparisonlocal

import (
	"context"
	"slices"
	"syscall"
	"testing"
	"time"
)

func TestTheHelperRunsTheProcfsPinnedExecutableAsTheAccount(t *testing.T) {
	command := helperCommand(context.Background(), synthetic, []byte("{}"))
	if command.Path != "/proc/self/exe" || !slices.Equal(command.Args, []string{"/proc/self/exe", HelperMode}) {
		t.Fatalf("helper command = %q %q", command.Path, command.Args)
	}
	attributes := command.SysProcAttr
	if attributes == nil || attributes.Pdeathsig != syscall.SIGKILL || attributes.Credential == nil {
		t.Fatalf("helper process attributes = %#v", attributes)
	}
	if credential := attributes.Credential; credential.Uid != 60001 || credential.Gid != 60002 || !slices.Equal(credential.Groups, []uint32{60002, 60003}) {
		t.Fatalf("helper credential = %#v", credential)
	}
	if !slices.Equal(command.Env, []string{"LANG=C", "LC_ALL=C"}) || command.Dir != "/" || command.Stdin == nil || command.WaitDelay != time.Second {
		t.Fatalf("helper environment = %q in %q", command.Env, command.Dir)
	}
}

func TestTheToolDiesWithItsParentAndDropsToTheAccountOnlyWhenRootServesIt(t *testing.T) {
	environment := toolEnvironment("xterm", "", synthetic.Home)
	for _, delegate := range []bool{true, false} {
		command := toolCommand(context.Background(), identity{account: synthetic, delegate: delegate}, "/usr/bin/meld", "/tmp/bootwright-diff-1", "/tmp/bootwright-diff-1/desired/a", "/tmp/bootwright-diff-1/applied/a", environment)
		if command.SysProcAttr.Pdeathsig != syscall.SIGKILL || (command.SysProcAttr.Credential != nil) != delegate {
			t.Fatalf("delegate %t: attributes %#v", delegate, command.SysProcAttr)
		}
		if !slices.Equal(command.Env, []string{"TERM=xterm", "HOME=/home/synthetic", "PATH=/usr/local/bin:/usr/bin:/bin"}) {
			t.Fatalf("environment %q", command.Env)
		}
	}
}
