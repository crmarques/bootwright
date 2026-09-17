//go:build linux && amd64

package sshlocal

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
)

// stubClient stands in for the pinned SSH client. It is a real executable, so
// the descriptors, environment and exit status a session produces are the ones
// a child process actually receives.
func stubClient(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func launcher(t *testing.T, client string) Launcher {
	t.Helper()
	home := t.TempDir()
	return Launcher{
		Client: client, Home: func() (string, error) { return home, nil }, PolicyPath: "", Scratch: t.TempDir(),
		Environ: func() []string {
			return []string{"TERM=xterm-256color", "SSH_AUTH_SOCK=/run/agent", "LD_PRELOAD=/evil.so"}
		},
	}
}

func keySession() machine.Session {
	s := session(machine.IdentityKey)
	s.PrivateKey = []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nmaterial\n")
	return s
}

// The client reads its material through its own descriptors, so the key never
// exists under a name another process could open.
func TestTheClientReadsItsMaterialThroughItsOwnDescriptors(t *testing.T) {
	client := stubClient(t, `cat /proc/self/fd/4; cat /proc/self/fd/5; exit 7`)
	var out bytes.Buffer
	code, err := launcher(t, client).Run(context.Background(), keySession(), nil, &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	if code != 7 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "OPENSSH PRIVATE KEY") {
		t.Fatalf("the client could not read its key: %q", out.String())
	}
	if !strings.Contains(out.String(), "198.51.100.11 ssh-ed25519 ") {
		t.Fatalf("the client could not read its pinned host key: %q", out.String())
	}
}

func TestSessionMaterialExistsUnderNoName(t *testing.T) {
	scratch := t.TempDir()
	client := stubClient(t, `exit 0`)
	l := launcher(t, client)
	l.Scratch = scratch
	if _, err := l.Run(context.Background(), keySession(), nil, os.Stderr, os.Stderr); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(scratch)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("session material remained on disk: %+v", entries)
	}
}

// A caller-selected askpass helper, agent or loader override must not cross
// into the client, while the terminal identity it needs does.
func TestTheClientInheritsOnlyTerminalEnvironment(t *testing.T) {
	client := stubClient(t, `env; exit 0`)
	var out bytes.Buffer
	if _, err := launcher(t, client).Run(context.Background(), keySession(), nil, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "TERM=xterm-256color") {
		t.Fatalf("environment = %q", out.String())
	}
	for _, refused := range []string{"SSH_AUTH_SOCK", "LD_PRELOAD"} {
		if strings.Contains(out.String(), refused) {
			t.Fatalf("%s crossed into the client: %q", refused, out.String())
		}
	}
}

func TestTheSessionStreamsAndExitStatusAreTheClients(t *testing.T) {
	client := stubClient(t, `cat; echo "out"; echo "err" >&2; exit 3`)
	var out, errOut bytes.Buffer
	code, err := launcher(t, client).Run(context.Background(), keySession(), strings.NewReader("in\n"), &out, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	if code != 3 {
		t.Fatalf("exit = %d", code)
	}
	if out.String() != "in\nout\n" || errOut.String() != "err\n" {
		t.Fatalf("out = %q err = %q", out.String(), errOut.String())
	}
}

func TestASignalledClientReportsTheConventionalStatus(t *testing.T) {
	client := stubClient(t, `kill -TERM $$`)
	code, err := launcher(t, client).Run(context.Background(), keySession(), nil, os.Stderr, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	if code != 143 {
		t.Fatalf("exit = %d, want 143", code)
	}
}

func TestASessionRefusesWithoutItsPinnedClientOrHostKey(t *testing.T) {
	missing := launcher(t, filepath.Join(t.TempDir(), "absent"))
	if _, err := missing.Run(context.Background(), keySession(), nil, os.Stderr, os.Stderr); err == nil {
		t.Fatal("an absent client opened a session")
	} else if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Code != "access.unavailable" {
		t.Fatalf("diagnostic = %+v", reported)
	}
	unpinned := keySession()
	unpinned.HostKey = trustless()
	client := stubClient(t, `exit 0`)
	if _, err := launcher(t, client).Run(context.Background(), unpinned, nil, os.Stderr, os.Stderr); err == nil {
		t.Fatal("a session opened without a pinned host key")
	}
	empty := keySession()
	empty.PrivateKey = nil
	if _, err := launcher(t, client).Run(context.Background(), empty, nil, os.Stderr, os.Stderr); err == nil {
		t.Fatal("a key identity opened a session with no key")
	}
}

func TestAnOfferedIdentityFileIsResolvedAgainstTheInvokingAccount(t *testing.T) {
	home := t.TempDir()
	l := launcher(t, stubClient(t, `exit 0`))
	l.Home = func() (string, error) { return home, nil }
	key := filepath.Join(home, "id_ed25519")
	if err := os.WriteFile(key, []byte("material"), 0600); err != nil {
		t.Fatal(err)
	}
	resolved, err := l.IdentityFile("~/id_ed25519")
	if err != nil || resolved != key {
		t.Fatalf("resolved = %q (%v)", resolved, err)
	}
	if empty, err := l.IdentityFile("  "); err != nil || empty != "" {
		t.Fatalf("an unset flag resolved to %q (%v)", empty, err)
	}
	if err := os.Chmod(key, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := l.IdentityFile(key); err == nil {
		t.Fatal("a key readable by group or other was offered")
	}
	if _, err := l.IdentityFile(filepath.Join(home, "absent")); err == nil {
		t.Fatal("an absent key was offered")
	}
	if _, err := l.IdentityFile(home); err == nil {
		t.Fatal("a directory was offered as a key")
	}
}

func TestAnObservationReportsWhatItCouldNotProve(t *testing.T) {
	// A client that records nothing leaves the endpoint unknown, which is never
	// an absent or an untrusted key.
	client := stubClient(t, `exit 255`)
	l := launcher(t, client)
	_, err := l.Observe(context.Background(), "198.51.100.11", 22)
	if err == nil {
		t.Fatal("an unreachable endpoint produced a host key")
	}
	if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Code != "trust.identity" {
		t.Fatalf("diagnostic = %+v", reported)
	}
	if _, err := l.Observe(context.Background(), "", 22); err == nil {
		t.Fatal("an unnamed address was observed")
	}
}

func TestAnObservationReadsTheKeyTheHandshakeRecorded(t *testing.T) {
	// The client records the key it negotiated; its own exit status is ignored
	// because authentication is expected to fail.
	client := stubClient(t, `for a in "$@"; do case "$a" in UserKnownHostsFile=*) f="${a#UserKnownHostsFile=}";; esac; done
printf '%s\n' "198.51.100.11 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICG+RadYRVwHqO7OQsOpyh8SDf1tbTQ8PUmgn7UEmou/" > "$f"
exit 255`)
	key, err := launcher(t, client).Observe(context.Background(), "198.51.100.11", 22)
	if err != nil {
		t.Fatal(err)
	}
	if key.Type != "ssh-ed25519" || key.Fingerprint() != "SHA256:8oT3qNBeFE7en6Ce9uTOcz+5ihUnqv435ifHqGftBJA" {
		t.Fatalf("observed = %+v", key)
	}
}

func TestAnObservationRefusesAKeyForAnotherEndpoint(t *testing.T) {
	client := stubClient(t, `for a in "$@"; do case "$a" in UserKnownHostsFile=*) f="${a#UserKnownHostsFile=}";; esac; done
printf '%s\n' "elsewhere.test ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICG+RadYRVwHqO7OQsOpyh8SDf1tbTQ8PUmgn7UEmou/" > "$f"
exit 255`)
	if _, err := launcher(t, client).Observe(context.Background(), "198.51.100.11", 22); err == nil {
		t.Fatal("a key recorded for another endpoint was accepted")
	}
}
