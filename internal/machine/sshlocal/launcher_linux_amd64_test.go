//go:build linux && amd64

package sshlocal

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"golang.org/x/sys/unix"
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
		Client: client, Home: func() (string, error) { return home, nil },
		Owner: func() (int, error) { return os.Getuid(), nil }, PolicyPath: "", Scratch: t.TempDir(),
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
	absent := filepath.Join(home, "absent")
	_, err = l.IdentityFile(absent)
	refusedAccess(t, err, "an absent key")
	namesTheFile(t, err, "cannot be opened", absent)
	_, err = l.IdentityFile(home)
	refusedAccess(t, err, "a directory")
	namesTheFile(t, err, "is not a regular file", home)
}

// offeredKey writes one private key file the invoking account owns.
func offeredKey(t *testing.T, content string) string {
	t.Helper()
	key := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(key, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return key
}

// refusedAccess requires the refusal every unusable offered key reports.
func refusedAccess(t *testing.T, err error, reason string) {
	t.Helper()
	if err == nil {
		t.Fatal(reason + " was offered")
	}
	if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Code != "access.unavailable" {
		t.Fatalf("diagnostic = %+v", reported)
	}
}

// namesTheFile requires a refusal to name the offered file and what to offer
// instead, so the operator can tell which file was refused and why.
func namesTheFile(t *testing.T, err error, reason, path string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) == 0 || !strings.Contains(reported[0].Message, reason) ||
		!strings.Contains(reported[0].Message, path) || reported[0].Remediation == "" {
		t.Fatalf("diagnostic = %+v", reported)
	}
}

// A link at the offered name is refused rather than followed, so the name the
// operator passed cannot be redirected to a file they did not name, including
// one the invoking account could not otherwise have offered.
func TestAnOfferedIdentityFileThatIsALinkIsRefused(t *testing.T) {
	l := launcher(t, stubClient(t, `exit 0`))
	key := offeredKey(t, "material")
	link := filepath.Join(t.TempDir(), "id_link")
	if err := os.Symlink(key, link); err != nil {
		t.Fatal(err)
	}
	_, err := l.IdentityFile(link)
	refusedAccess(t, err, "a link to a key")
	if reported := diagnostics.Of(err); !strings.Contains(reported[0].Message, "symbolic link") ||
		!strings.Contains(reported[0].Message, link) || reported[0].Remediation == "" {
		t.Fatalf("diagnostic = %+v", reported)
	}
}

// A FIFO is not a key. Resolving it does not wait for a writer, so the refusal
// comes at once rather than after a session that never starts.
func TestAnOfferedIdentityFileThatIsNotARegularFileIsRefused(t *testing.T) {
	l := launcher(t, stubClient(t, `exit 0`))
	fifo := filepath.Join(t.TempDir(), "id_fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	refused := make(chan error, 1)
	go func() {
		_, err := l.IdentityFile(fifo)
		refused <- err
	}()
	select {
	case err := <-refused:
		refusedAccess(t, err, "a FIFO")
		namesTheFile(t, err, "is not a regular file", fifo)
	case <-time.After(5 * time.Second):
		t.Fatal("opening an offered FIFO waited for a writer")
	}
}

// unopenableDevice creates a character device node that no driver answers, so
// every open of it for reading fails at once. It skips where this account
// cannot create device nodes.
func unopenableDevice(t *testing.T) string {
	t.Helper()
	device := filepath.Join(t.TempDir(), "id_device")
	// The major numbers Linux reserves for local and experimental use.
	for _, major := range []uint32{60, 61, 62, 63, 120, 121, 122, 123, 124, 125, 126, 127} {
		if err := unix.Mknod(device, unix.S_IFCHR|0600, int(unix.Mkdev(major, 0))); err != nil {
			t.Skipf("this account cannot create a device node: %v", err)
		}
		file, err := os.OpenFile(device, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return device
		}
		_ = file.Close()
		if err := os.Remove(device); err != nil {
			t.Fatal(err)
		}
	}
	t.Skip("a driver answers every major number reserved for local use")
	return ""
}

// A device is not a key, and its driver is never asked to open it: a device
// such as a watchdog acts on being opened, whatever is read. The device here
// fails every open for reading, so a refusal naming its type rather than an
// unopenable file proves the type was read without such an open.
func TestAnOfferedIdentityFileThatIsADeviceIsRefusedWithoutBeingOpened(t *testing.T) {
	l := launcher(t, stubClient(t, `exit 0`))
	device := unopenableDevice(t)
	_, err := l.IdentityFile(device)
	refusedAccess(t, err, "a character device")
	namesTheFile(t, err, "is not a regular file", device)
}

// A key another account owns is not the invoking account's to offer, even to a
// process privileged enough to read it.
func TestAnOfferedIdentityFileOwnedByAnotherAccountIsRefused(t *testing.T) {
	key := offeredKey(t, "material")
	other := launcher(t, stubClient(t, `exit 0`))
	other.Owner = func() (int, error) { return os.Getuid() + 1, nil }
	_, err := other.IdentityFile(key)
	refusedAccess(t, err, "a key another account owns")
	if reported := diagnostics.Of(err); !strings.Contains(reported[0].Message, "not owned by the invoking account") {
		t.Fatalf("diagnostic = %+v", reported)
	}
	unverified := launcher(t, stubClient(t, `exit 0`))
	unverified.Owner = func() (int, error) { return 0, errors.New("account") }
	_, err = unverified.IdentityFile(key)
	refusedAccess(t, err, "a key whose owner could not be verified")
	namesTheFile(t, err, "invoking account cannot be verified", key)
	unverified.Owner = nil
	_, err = unverified.IdentityFile(key)
	refusedAccess(t, err, "a key with no owner to verify it against")
	namesTheFile(t, err, "invoking account cannot be verified", key)
}

// The client reads the offered key through the descriptor it was proved on,
// never through its name, so replacing the name after the key was resolved
// changes nothing the client can read.
func TestTheClientReadsTheOfferedKeyThroughTheDescriptorItWasProvedOn(t *testing.T) {
	client := stubClient(t, `echo "$@"; cat /proc/self/fd/4; exit 0`)
	l := launcher(t, client)
	key := offeredKey(t, "OFFERED KEY\n")
	resolved, err := l.IdentityFile(key)
	if err != nil {
		t.Fatal(err)
	}
	offered := session(machine.IdentityOperator)
	offered.IdentityFile = resolved
	var out bytes.Buffer
	if _, err := l.Run(context.Background(), offered, nil, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "-i /proc/self/fd/4 ") || strings.Contains(out.String(), key) {
		t.Fatalf("the client was not handed the held key: %q", out.String())
	}
	if !strings.Contains(out.String(), "OFFERED KEY") {
		t.Fatalf("the client could not read the offered key: %q", out.String())
	}
	// A name swapped for a link between resolution and the session is refused
	// when the session opens it, rather than followed to what it now names.
	elsewhere := offeredKey(t, "ANOTHER KEY\n")
	if err := os.Remove(key); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, key); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	_, err = l.Run(context.Background(), offered, nil, &out, &out)
	refusedAccess(t, err, "a key swapped for a link after it was resolved")
	if strings.Contains(out.String(), "ANOTHER KEY") {
		t.Fatalf("the client read the file a swapped link names: %q", out.String())
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
