//go:build linux && amd64

package sshlocal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/nativeexec"
)

func TestOperatorHandoffDropsCredentialsAndKeepsTheEndpoint(t *testing.T) {
	config, err := anonymous(t.TempDir(), []byte("HostKeyAlgorithms ssh-ed25519\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer config.Close()
	known, err := anonymous(t.TempDir(), []byte("pinned host key\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer known.Close()
	logon := session(machine.IdentityOperator)
	logon.User = ""
	logon.Port = 2222
	command := operatorCommand(nativeexec.Command{Path: Client}, logon, logon.HostKey.Algorithms(),
		nativeexec.Account{UID: 1234, GID: 2345, Groups: []uint32{3456}}, []*os.File{config, known})
	if command.Path != nativeexec.SelfExecutable || command.Credential.Uid != 1234 || command.Credential.Gid != 2345 ||
		!slices.Equal(command.Credential.Groups, []uint32{3456}) || len(command.ExtraFiles) != 2 {
		t.Fatalf("handoff = %+v", command)
	}
	for _, arg := range []string{operatorMode, "UserKnownHostsFile=4", "2222", logon.Address} {
		if !slices.Contains(command.Args, arg) {
			t.Fatalf("handoff is missing %q: %v", arg, command.Args)
		}
	}
	if slices.Contains(command.Args, "-i") || slices.Contains(command.Args, "-l") {
		t.Fatalf("SSH should choose the dropped account's own login and keys: %v", command.Args)
	}
}

func TestOperatorClientReadsPolicyThroughTheNewOwnerDescriptors(t *testing.T) {
	realClient(t)
	for _, invalid := range []bool{false, true} {
		config, err := anonymous(t.TempDir(), []byte(realClientPolicy))
		if err != nil {
			t.Fatal(err)
		}
		known, err := anonymous(t.TempDir(), []byte("pinned host key\n"))
		if err != nil {
			config.Close()
			t.Fatal(err)
		}
		session := session(machine.IdentityOperator)
		session.User = "ops"
		session.Port = 2222
		command := operatorCommand(nativeexec.Command{}, session, session.HostKey.Algorithms(), nativeexec.Account{}, []*os.File{config, known})
		args := slices.Insert(command.Args[1:], 2, "-G")
		if invalid {
			index := slices.Index(args, "UserKnownHostsFile=4")
			args[index] = "UserKnownHostsFile=unexpected"
		}
		var out bytes.Buffer
		status := runOperator(args, config, known, nil, &out, io.Discard)
		if invalid {
			if status != 255 || out.Len() != 0 {
				t.Fatalf("invalid host pin handoff gave status %d and output %q", status, out.String())
			}
			continue
		}
		if status != 0 || !strings.Contains(out.String(), "ciphers aes128-ctr\n") ||
			!strings.Contains(out.String(), "user ops\n") || !strings.Contains(out.String(), "port 2222\n") ||
			!strings.Contains(out.String(), "userknownhostsfile /proc/") {
			t.Fatalf("ssh -G status %d, output %q", status, out.String())
		}
	}
}

func TestOperatorHandoffRefusesUnknownAccountsAndKeepsDirectRoot(t *testing.T) {
	launcher := Launcher{}
	command := nativeexec.Command{Path: Client}
	session := session(machine.IdentityOperator)
	ctx := context.Background()
	if _, err := launcher.invokingCommand(ctx, command, session, "", nil); err == nil {
		t.Fatal("an unavailable account resolver silently kept root")
	}
	launcher.Account = func(context.Context) (nativeexec.Account, error) {
		return nativeexec.Account{}, errors.New("unavailable")
	}
	if _, err := launcher.invokingCommand(ctx, command, session, "", nil); err == nil {
		t.Fatal("an unverified account silently kept root")
	}
	launcher.Account = func(context.Context) (nativeexec.Account, error) { return nativeexec.Account{}, nil }
	if got, err := launcher.invokingCommand(ctx, command, session, "", nil); err != nil || got.Path != Client || got.Credential != nil {
		t.Fatalf("a verified direct-root invocation changed: %+v, %v", got, err)
	}
	launcher.Account = func(context.Context) (nativeexec.Account, error) {
		return nativeexec.Account{UID: 1234, GID: 2345}, nil
	}
	if got, err := launcher.invokingCommand(ctx, command, session, "", nil); err != nil || got.Credential == nil || got.Credential.Uid != 1234 {
		t.Fatalf("a verified operator was not selected: %+v, %v", got, err)
	}
	if handled, _ := ServeOperator(nil); handled {
		t.Fatal("the helper intercepted an ordinary invocation")
	}
	if handled, status := ServeOperator([]string{operatorMode}); !handled || status != 255 {
		t.Fatal("the helper accepted missing arguments")
	}
}
