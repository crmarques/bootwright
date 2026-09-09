package invocation

import (
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
		{"forged environment", accountRequest{uid: 0, euid: 0, sudoUID: "1001", sudoGID: "1002", sudoUser: "operator"}, 0, true},
		{"partial environment", accountRequest{uid: 0, euid: 0, sudoUID: "1001", trustedParent: true}, 0, true},
		{"wrong group", accountRequest{uid: 0, euid: 0, sudoUID: "1001", sudoGID: "1003", sudoUser: "operator", trustedParent: true}, 0, true},
		{"wrong name", accountRequest{uid: 0, euid: 0, sudoUID: "1001", sudoGID: "1002", sudoUser: "another", trustedParent: true}, 0, true},
		{"setuid binary", accountRequest{uid: 1001, euid: 0}, 0, true},
		{"nonroot sudo env ignored", accountRequest{uid: 1001, euid: 1001, sudoUID: "0", sudoGID: "0", sudoUser: "root"}, 1001, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := accountFrom(passwd, groups, test.request)
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
	} {
		if _, err := accountFrom([]byte(passwd), nil, accountRequest{uid: 1001, euid: 1001}); err == nil {
			t.Fatal("unsafe account accepted")
		}
	}
}
