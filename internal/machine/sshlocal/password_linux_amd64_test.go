package sshlocal

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/machine"
)

func TestOnlyAnonymousPrivatePasswordMaterialReachesAskpass(t *testing.T) {
	for _, test := range []struct {
		name, value string
		valid       bool
	}{
		{"one password", "fixture-secret", true},
		{"line injection", "fixture-secret\nsecond", false},
		{"empty", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, err := anonymous(t.TempDir(), []byte(test.value))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			var out bytes.Buffer
			err = sendPassword(descriptorPath(int(file.Fd())), &out)
			if test.valid && (err != nil || out.String() != test.value+"\n") {
				t.Fatal("qualified password was not delivered")
			}
			if !test.valid && (err == nil || out.Len() != 0) {
				t.Fatal("invalid password material was offered")
			}
			env := strings.Join(passwordEnvironment(file), "\n")
			if test.value != "" && strings.Contains(env, test.value) {
				t.Fatal("the password entered the SSH environment")
			}
		})
	}
	persistent := filepath.Join(t.TempDir(), "named")
	if err := os.WriteFile(persistent, []byte("fixture-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := sendPassword(persistent, &out); err == nil || out.Len() != 0 {
		t.Fatal("the askpass worker read a named file")
	}
}

func TestAskpassMustBelongToTheLaunchingSSHProcess(t *testing.T) {
	proc := t.TempDir()
	parent := filepath.Join(proc, "42")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	ssh := stubClient(t, "exit 0")
	if err := os.Symlink(ssh, filepath.Join(parent, "exe")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "status"), []byte("Name: ssh\nPPid:\t18\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if !startedByClient("/proc/18/fd/4", proc, 42, ssh) || startedByClient("/proc/19/fd/4", proc, 42, ssh) {
		t.Fatal("the worker accepted material outside the client's launcher")
	}
	t.Setenv(passwordDescriptor, "/proc/18/fd/4")
	if handled, code := ServePassword([]string{"fixture", "extra"}); !handled || code == 0 {
		t.Fatal("a malformed askpass invocation was admitted")
	}
	t.Setenv(passwordDescriptor, "")
	if handled, _ := ServePassword([]string{"machine"}); handled {
		t.Fatal("an ordinary invocation was treated as askpass")
	}
}

func TestTheIsolatedSSHClientReceivesOnlyThePasswordHandle(t *testing.T) {
	client := stubClient(t, `test "$SSH_ASKPASS_REQUIRE" = force && test -n "$BOOTWRIGHT_SSH_PASSWORD_FD"`)
	s := session(machine.IdentityPassword)
	s.Password = []byte("fixture-secret")
	code, err := launcher(t, client).Run(context.Background(), s, nil, io.Discard, io.Discard)
	if err != nil || code != 0 {
		t.Fatalf("the client did not receive its isolated askpass configuration (%d, %v)", code, err)
	}
}

func TestAskpassQualificationRejectsForeignOrUnreadableProcess(t *testing.T) {
	proc := t.TempDir()
	other := stubClient(t, "exit 0")
	if startedByClient("/proc/10/fd/2", proc, 42, other) {
		t.Fatal("unreadable parent qualified")
	}
	parent := filepath.Join(proc, "42")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	ssh := stubClient(t, "exit 0")
	if err := os.Symlink(ssh, filepath.Join(parent, "exe")); err != nil {
		t.Fatal(err)
	}
	if startedByClient("/proc/10/fd/2", proc, 42, other) || startedByClient("/proc/10/fd/2", proc, 42, ssh) {
		t.Fatal("unknown or foreign launcher qualified")
	}
	if err := os.WriteFile(filepath.Join(parent, "status"), []byte("Name: ssh\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if startedByClient("/proc/10/fd/2", proc, 42, ssh) {
		t.Fatal("process without a proved parent qualified")
	}
}
