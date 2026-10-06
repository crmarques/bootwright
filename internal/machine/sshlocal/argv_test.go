package sshlocal

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/trust"
)

func hostKey() trust.HostKey {
	return trust.HostKey{Type: "ssh-ed25519", PublicKey: "AAAAC3NzaC1lZDI1NTE5AAAAICG+RadYRVwHqO7OQsOpyh8SDf1tbTQ8PUmgn7UEmou/"}
}

func session(kind machine.IdentityKind) machine.Session {
	return machine.Session{
		Machine: "rhel-01", Address: "198.51.100.11", Port: 22,
		Identity: machine.Identity{Kind: kind, User: "bootwright"},
		HostKey:  hostKey(),
	}
}

const fixedSuffix = "-o GSSAPIAuthentication=no -o HostbasedAuthentication=no -o ForwardAgent=no " +
	"-o CheckHostIP=no -o GlobalKnownHostsFile=none -o KnownHostsCommand=none -o VerifyHostKeyDNS=no " +
	"-o UpdateHostKeys=no -o HashKnownHosts=no"

func TestTheKeyArmOffersExactlyTheResolvedCredential(t *testing.T) {
	got := strings.Join(arguments(session(machine.IdentityKey), paths{config: 3, privateKey: 4, knownHosts: 5}), " ")
	want := "-F /proc/self/fd/3 -i /proc/self/fd/4 -o IdentitiesOnly=yes -o IdentityAgent=none " +
		"-o PreferredAuthentications=publickey -o PasswordAuthentication=no -o KbdInteractiveAuthentication=no " +
		fixedSuffix + " -o UserKnownHostsFile=/proc/self/fd/5 -o StrictHostKeyChecking=yes " +
		"-o HostKeyAlgorithms=ssh-ed25519 -l bootwright 198.51.100.11"
	if got != want {
		t.Fatalf("arguments =\n%s\nwant\n%s", got, want)
	}
}

// An offered key is preferred, and the declared credential stays behind it as
// the fallback rather than being dropped. The client reads the offered key
// through its own descriptor, never by the name it was offered as.
func TestAnOfferedKeyPrecedesTheDeclaredCredential(t *testing.T) {
	with := session(machine.IdentityKey)
	with.IdentityFile = "/home/operator/.ssh/id_ed25519"
	got := strings.Join(arguments(with, paths{config: 3, identityFile: 4, privateKey: 5, knownHosts: 6}), " ")
	if !strings.Contains(got, "-i /proc/self/fd/4 -i /proc/self/fd/5 -o IdentitiesOnly=yes") {
		t.Fatalf("arguments = %s", got)
	}
	if strings.Contains(got, with.IdentityFile) {
		t.Fatalf("the client was handed the offered key by name: %s", got)
	}
}

func TestTheOperatorArmOffersNoMaterialFromThisContext(t *testing.T) {
	got := strings.Join(arguments(session(machine.IdentityOperator), paths{config: 3, knownHosts: 4}), " ")
	if strings.Contains(got, "/proc/self/fd/4 -o IdentitiesOnly") || strings.Contains(got, "-i ") {
		t.Fatalf("the operator arm offered a credential: %s", got)
	}
	if !strings.Contains(got, "-o UserKnownHostsFile=/proc/self/fd/4") {
		t.Fatalf("arguments = %s", got)
	}
	if !strings.Contains(got, "-o IdentityAgent=none") {
		t.Fatalf("the operator arm admitted an agent: %s", got)
	}
}

// The operator arm with no authored user names no account and no key: the
// client logs in as the account it runs as, with that account's default
// identity files and no agent, which is what the machines spec documents for
// auth.operatorIdentity.
func TestTheOperatorArmWithNoUserLogsInAsTheClientAccount(t *testing.T) {
	unnamed := session(machine.IdentityOperator)
	unnamed.User = ""
	args := arguments(unnamed, paths{config: 3, knownHosts: 4})
	got := strings.Join(args, " ")
	for _, absent := range []string{"-l", "-i"} {
		if slices.Contains(args, absent) {
			t.Fatalf("the operator arm passed %s: %s", absent, got)
		}
	}
	if strings.Contains(got, "IdentitiesOnly") {
		t.Fatalf("the operator arm confined the client's default identities: %s", got)
	}
	for _, required := range []string{"-o IdentityAgent=none", "-o PreferredAuthentications=publickey"} {
		if !strings.Contains(got, required) {
			t.Fatalf("arguments = %s, want %s", got, required)
		}
	}
	if args[len(args)-1] != unnamed.Address {
		t.Fatalf("arguments = %s", got)
	}
}

func TestThePasswordArmLetsTheClientAskAndOffersNoKey(t *testing.T) {
	got := strings.Join(arguments(session(machine.IdentityPassword), paths{config: 3, knownHosts: 4}), " ")
	if !strings.Contains(got, "-o PubkeyAuthentication=no -o PreferredAuthentications=password,keyboard-interactive") {
		t.Fatalf("arguments = %s", got)
	}
}

func TestAPinnedRSAKeyAdmitsTheSignaturesItsServerMayOffer(t *testing.T) {
	with := session(machine.IdentityKey)
	with.HostKey = trust.HostKey{Type: "ssh-rsa", PublicKey: "AAAA"}
	got := strings.Join(arguments(with, paths{config: 3, privateKey: 4, knownHosts: 5}), " ")
	if !strings.Contains(got, "-o HostKeyAlgorithms=rsa-sha2-512,rsa-sha2-256,ssh-rsa") {
		t.Fatalf("arguments = %s", got)
	}
}

func TestANonDefaultPortIsNamedAndTheDefaultIsNot(t *testing.T) {
	with := session(machine.IdentityOperator)
	with.Port = 2222
	if got := strings.Join(arguments(with, paths{config: 3, knownHosts: 4}), " "); !strings.Contains(got, "-p 2222") {
		t.Fatalf("arguments = %s", got)
	}
	if got := strings.Join(arguments(session(machine.IdentityOperator), paths{config: 3, knownHosts: 4}), " "); strings.Contains(got, "-p ") {
		t.Fatalf("the default port was named: %s", got)
	}
}

// The client joins command words with spaces and the remote shell splits them
// again, so every word has to survive that round trip unchanged.
func TestACommandReachesTheRemoteShellAsTheExactWordsGiven(t *testing.T) {
	with := session(machine.IdentityOperator)
	with.Command = []string{"systemctl", "status", "sshd.service", "a b", "", "$(payload)", "it's"}
	got := arguments(with, paths{config: 3, knownHosts: 4})
	tail := got[len(got)-7:]
	want := []string{"systemctl", "status", "sshd.service", "'a b'", "''", "'$(payload)'", `'it'\''s'`}
	for i := range want {
		if tail[i] != want[i] {
			t.Fatalf("word %d = %q, want %q", i, tail[i], want[i])
		}
	}
}

// An observation proves a key and nothing else: it must offer no credential,
// because a key that has not been trusted yet cannot authorize one.
func TestAnObservationOffersNoCredential(t *testing.T) {
	got := strings.Join(observation("198.51.100.11", 2222, 3, "/tmp/scratch/known_hosts", 10), " ")
	for _, required := range []string{
		"-F /proc/self/fd/3",
		"-o StrictHostKeyChecking=accept-new",
		"-o UserKnownHostsFile=/tmp/scratch/known_hosts",
		"-o BatchMode=yes",
		"-o PubkeyAuthentication=no",
		"-o PasswordAuthentication=no",
		"-o IdentityAgent=none",
		"-o ConnectTimeout=10",
		"-p 2222",
	} {
		if !strings.Contains(got, required) {
			t.Fatalf("observation missing %q: %s", required, got)
		}
	}
	if strings.Contains(got, "-i ") || strings.Contains(got, "StrictHostKeyChecking=yes") {
		t.Fatalf("observation = %s", got)
	}
	if !strings.HasSuffix(got, "198.51.100.11 true") {
		t.Fatalf("observation = %s", got)
	}
}

func trustless() trust.HostKey { return trust.HostKey{} }
