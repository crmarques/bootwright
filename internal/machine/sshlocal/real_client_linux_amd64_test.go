//go:build linux && amd64

package sshlocal

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const realClientPolicy = "Ciphers aes128-ctr\nKexAlgorithms curve25519-sha256\n"

// realClient returns the host's OpenSSH client, or skips where none is
// installed. `ssh -G` prints the configuration it resolved and exits before it
// loads identities or connects, so this proves only that the client, after
// closing its inherited descriptors, can still open each held path.
func realClient(t *testing.T) string {
	t.Helper()
	info, err := os.Stat(Client)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		t.Skipf("no executable OpenSSH client at %s", Client)
	}
	return Client
}

func resolvedConfiguration(t *testing.T, client string, args []string) (string, string, error) {
	t.Helper()
	command := exec.Command(client, append([]string{"-G"}, args...)...)
	command.Env = []string{}
	command.Dir = "/"
	var out, errOut bytes.Buffer
	command.Stdout, command.Stderr = &out, &errOut
	err := command.Run()
	return out.String(), errOut.String(), err
}

func requireResolved(t *testing.T, resolved string, want ...string) {
	t.Helper()
	lines := strings.Split(resolved, "\n")
	for _, line := range want {
		if !slices.Contains(lines, line) {
			t.Fatalf("ssh -G did not resolve %q:\n%s", line, resolved)
		}
	}
}

func selfDescriptor(args []string) []string {
	rewritten := slices.Clone(args)
	index := slices.Index(rewritten, "-F")
	fd := rewritten[index+1][strings.LastIndex(rewritten[index+1], "/")+1:]
	rewritten[index+1] = "/proc/self/fd/" + fd
	return rewritten
}

func requireUnreadableConfig(t *testing.T, client string, args []string) {
	t.Helper()
	resolved, stderr, err := resolvedConfiguration(t, client, selfDescriptor(args))
	if err == nil || !strings.Contains(stderr, "Can't open user config file /proc/self/fd/") {
		t.Fatalf("a descriptor the client closed was readable: %v\n%s%s", err, stderr, resolved)
	}
}

func TestTheRealClientReadsSessionMaterialThroughLauncherDescriptors(t *testing.T) {
	client := realClient(t)
	l := launcher(t, client)
	s := keySession()
	s.IdentityFile = offeredKey(t, "OFFERED KEY\n")
	held, files, err := l.materialize(context.Background(), []byte(realClientPolicy), s)
	defer closeAll(files)
	if err != nil {
		t.Fatal(err)
	}
	args := arguments(s, held)
	resolved, stderr, err := resolvedConfiguration(t, client, args)
	if err != nil {
		t.Fatalf("ssh -G = %v\n%s", err, stderr)
	}
	requireResolved(t, resolved,
		"ciphers aes128-ctr",
		"kexalgorithms curve25519-sha256",
		"identityfile "+descriptorPath(held.identityFile),
		"identityfile "+descriptorPath(held.privateKey),
		"userknownhostsfile "+descriptorPath(held.knownHosts),
		"stricthostkeychecking true",
	)
	requireUnreadableConfig(t, client, args)
}

func TestTheRealClientReadsObservationPolicyThroughALauncherDescriptor(t *testing.T) {
	client := realClient(t)
	directory := t.TempDir()
	config, err := anonymous(directory, []byte(realClientPolicy))
	if err != nil {
		t.Fatal(err)
	}
	defer config.Close()
	recorded := directory + "/known_hosts"
	args := observation("198.51.100.11", 2222, int(config.Fd()), recorded, 10)
	resolved, stderr, err := resolvedConfiguration(t, client, args)
	if err != nil {
		t.Fatalf("ssh -G = %v\n%s", err, stderr)
	}
	requireResolved(t, resolved,
		"ciphers aes128-ctr",
		"kexalgorithms curve25519-sha256",
		"userknownhostsfile "+recorded,
		"stricthostkeychecking accept-new",
		"port "+strconv.Itoa(2222),
	)
	requireUnreadableConfig(t, client, args)
}
