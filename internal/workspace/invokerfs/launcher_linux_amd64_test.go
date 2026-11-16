//go:build linux && amd64

package invokerfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/nativeexec"
)

func rootOpener(account func(context.Context) (Account, error), launcher nativeexec.Launcher) *Opener {
	opener := New(account)
	opener.launcher = launcher
	return opener
}

func TestARootProcessOpensThroughAHelperUnderTheInvokingAccount(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the helper never serves root")
	}
	var launched []nativeexec.Command
	launcher := nativeexec.Launcher{Starter: func(ctx context.Context, command nativeexec.Command) (*nativeexec.Process, error) {
		launched = append(launched, command)
		return testLauncher(HelperMode).Start(ctx, command)
	}}
	opener := rootOpener(func(context.Context) (Account, error) { return invoker(), nil }, launcher)
	session, err := opener.begin(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, ok := session.link.(*channel); !ok {
		t.Fatalf("root began %#v, want a helper channel", session.link)
	}
	if len(launched) != 1 || launched[0].Credential == nil || launched[0].Credential.Uid != uint32(os.Getuid()) || launched[0].ParentDeath != syscall.SIGKILL {
		t.Fatalf("root launched %+v", launched)
	}
	path := filepath.Join(t.TempDir(), "named")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := session.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
}

func TestARootProcessServingRootOpensDirectly(t *testing.T) {
	forbidden := nativeexec.Launcher{Starter: func(context.Context, nativeexec.Command) (*nativeexec.Process, error) {
		t.Fatal("a helper was started for root")
		return nil, nil
	}}
	session, err := rootOpener(func(context.Context) (Account, error) { return Account{}, nil }, forbidden).begin(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if link, ok := session.link.(direct); !ok || !link.root {
		t.Fatalf("root serving root began %#v", session.link)
	}
}

func TestARootProcessRefusesAnUnusableAccountOrHelper(t *testing.T) {
	forbidden := nativeexec.Launcher{Starter: func(context.Context, nativeexec.Command) (*nativeexec.Process, error) {
		t.Fatal("a helper was started for an unusable account")
		return nil, nil
	}}
	resolved := errors.New("no such account")
	for _, row := range []struct {
		name     string
		account  func(context.Context) (Account, error)
		launcher nativeexec.Launcher
		want     error
	}{
		{"negative uid", func(context.Context) (Account, error) { return Account{UID: -1, GID: 1}, nil }, forbidden, errIdentity},
		{"uid beyond the id space", func(context.Context) (Account, error) { return Account{UID: maxID + 1, GID: 1}, nil }, forbidden, errIdentity},
		{"negative gid", func(context.Context) (Account, error) { return Account{UID: 1000, GID: -1}, nil }, forbidden, errIdentity},
		{"unresolved account", func(context.Context) (Account, error) { return Account{}, resolved }, forbidden, resolved},
		{"no account resolver", nil, forbidden, errIdentity},
		{"helper that cannot start", func(context.Context) (Account, error) { return Account{UID: 1000, GID: 1000}, nil },
			nativeexec.Launcher{Starter: func(context.Context, nativeexec.Command) (*nativeexec.Process, error) {
				return nil, errors.New("fork failed")
			}}, errHelper},
	} {
		t.Run(row.name, func(t *testing.T) {
			session, err := rootOpener(row.account, row.launcher).begin(context.Background(), 0)
			if session != nil {
				session.Close()
			}
			if !errors.Is(err, row.want) {
				t.Fatalf("began %v, %v; want %v", session, err, row.want)
			}
		})
	}
}

func TestACancelledRootSessionReturnsTheCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	launcher := nativeexec.Launcher{Starter: func(context.Context, nativeexec.Command) (*nativeexec.Process, error) {
		cancel()
		return nil, context.Canceled
	}}
	opener := rootOpener(func(context.Context) (Account, error) { return Account{UID: 1000, GID: 1000}, nil }, launcher)
	if _, err := opener.begin(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled begin gave %v", err)
	}
	if _, err := opener.begin(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("a begin under a cancelled context gave %v", err)
	}
}
