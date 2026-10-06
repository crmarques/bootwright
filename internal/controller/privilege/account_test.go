package privilege

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestLocalAccountAndManualSudoIdentity(t *testing.T) {
	passwd := []byte("root:x:0:0::/root:/bin/sh\noperator:x:1001:1002::/home/operator:/bin/sh\n")
	groups := []byte("operator:x:1002:\nshared:x:1003:operator\n")
	for _, test := range []struct {
		name    string
		request accountRequest
		wantUID int
		invalid bool
	}{
		{"ordinary", accountRequest{uid: 1001, euid: 1001}, 1001, false},
		{"direct root", accountRequest{uid: 0, euid: 0}, 0, false},
		{"manual sudo", accountRequest{uid: 0, euid: 0, sudoUID: "1001", sudoGID: "1002", sudoUser: "operator", trustedParent: true}, 1001, false},
		{"forged environment", accountRequest{uid: 0, euid: 0, sudoUID: "1001", sudoGID: "1002", sudoUser: "operator"}, 0, false},
		{"partial environment", accountRequest{uid: 0, euid: 0, sudoUID: "1001", trustedParent: true}, 0, true},
		{"wrong group", accountRequest{uid: 0, euid: 0, sudoUID: "1001", sudoGID: "1003", sudoUser: "operator", trustedParent: true}, 0, true},
		{"wrong name", accountRequest{uid: 0, euid: 0, sudoUID: "1001", sudoGID: "1002", sudoUser: "another", trustedParent: true}, 0, true},
		{"setuid binary", accountRequest{uid: 1001, euid: 0}, 0, true},
		{"nonroot sudo env ignored", accountRequest{uid: 1001, euid: 1001, sudoUID: "0", sudoGID: "0", sudoUser: "root"}, 1001, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := accountFrom(context.Background(), accountFiles{passwd: passwd, groups: groups}, test.request)
			if (err != nil) != test.invalid {
				t.Fatalf("account %v error %v", got, err)
			}
			if !test.invalid && got.UID != test.wantUID {
				t.Fatalf("account %v", got)
			}
			if !test.invalid && got.UID == 1001 && (!reflect.DeepEqual(got.Groups, []uint32{1002, 1003}) || got.Home != "/home/operator") {
				t.Fatalf("account %v", got)
			}
		})
	}
}

func TestAccountDatabaseAmbiguityAndPathRefusal(t *testing.T) {
	for _, passwd := range []string{
		"operator:x:1001:1002::relative:/bin/sh\n",
		"operator:x:1001:1002::/home/../operator:/bin/sh\n",
		"operator:x:1001:1002::/home/operator:/bin/sh\nother:x:1001:1002::/home/other:/bin/sh\n",
		"operator:x:1001:1002::/home/operator:/bin/sh\noperator:x:1003:1002::/home/other:/bin/sh\n",
		"oper\x00ator:x:1001:1002::/home/operator:/bin/sh\n",
	} {
		if _, err := accountFrom(context.Background(), accountFiles{passwd: []byte(passwd)}, accountRequest{uid: 1001, euid: 1001}); !errors.Is(err, errAccount) {
			t.Fatalf("unsafe account %q: %v", passwd, err)
		}
	}
}

// A sudo -i or sudo -s shell is root already, and the sudo metadata it carries
// came from no verified sudo parent, so it resolves as direct root; only the
// procfs command a Bootwright supervisor hands sudo marks an elevated child
// whose sudo is gone, which refuses.
func TestARootShellOfSudoResolvesAsDirectRoot(t *testing.T) {
	files := accountFiles{
		passwd: []byte("root:x:0:0::/root:/bin/sh\noperator:x:1001:1002::/home/operator:/bin/sh\n"),
		groups: []byte("operator:x:1002:\n"),
	}
	metadata := accountRequest{uid: 0, euid: 0, sudoUID: "1001", sudoGID: "1002", sudoUser: "operator"}
	for _, test := range []struct {
		name          string
		command       string
		trustedParent bool
		uid           int
		home, reason  string
	}{
		{name: "a login shell of sudo -i", command: "/bin/bash", uid: 0, home: "/root"},
		{name: "a shell of sudo -s", command: "/bin/bash -c bootwright status", uid: 0, home: "/root"},
		{name: "metadata without a command", uid: 0, home: "/root"},
		{name: "a re-execution whose sudo is gone", command: "/proc/4242/exe status", reason: "the sudo parent of this re-execution is gone"},
		{name: "a bare re-execution whose sudo is gone", command: "/proc/4242/exe", reason: "the sudo parent of this re-execution is gone"},
		{name: "a procfs-like path that is not a re-execution", command: "/proc/self/exe status", uid: 0, home: "/root"},
		{name: "manual sudo", command: "/usr/local/bin/bootwright status", trustedParent: true, uid: 1001, home: "/home/operator"},
		{name: "a re-execution under its sudo", command: "/proc/4242/exe status", trustedParent: true, uid: 1001, home: "/home/operator"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := metadata
			request.sudoCommand, request.trustedParent = test.command, test.trustedParent
			got, err := accountFrom(context.Background(), files, request)
			if test.reason != "" {
				var refusal accountRefusal
				if !errors.As(err, &refusal) || refusal.reason != test.reason || !errors.Is(err, errAccount) {
					t.Fatalf("account %+v, error %v; want the refusal %q", got, err, test.reason)
				}
				return
			}
			if err != nil || got.UID != test.uid || got.Home != test.home || got.SudoParentPID != 0 {
				t.Fatalf("account %+v, error %v; want UID %d, home %s and no sudo parent", got, err, test.uid, test.home)
			}
		})
	}
}
