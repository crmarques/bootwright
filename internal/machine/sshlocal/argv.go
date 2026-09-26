package sshlocal

import (
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/machine"
)

// descriptorPath names one of the client's own inherited descriptors. The
// client resolves it in its own process, so material never exists under a name
// any other process could open.
func descriptorPath(fd int) string { return "/proc/self/fd/" + strconv.Itoa(fd) }

// paths names where one invocation's material was placed for the client. An
// absent descriptor is zero, which is never a material descriptor.
type paths struct {
	config       int
	identityFile int
	privateKey   int
	knownHosts   int
}

// fixedOptions are the client behaviors no session may vary. They remove every
// ambient source of identity and trust so the only credential offered and the
// only server key accepted are the ones this invocation resolved.
var fixedOptions = []string{
	"GSSAPIAuthentication=no",
	"HostbasedAuthentication=no",
	"ForwardAgent=no",
	"CheckHostIP=no",
	"GlobalKnownHostsFile=none",
	"KnownHostsCommand=none",
	"VerifyHostKeyDNS=no",
	"UpdateHostKeys=no",
	"HashKnownHosts=no",
}

// arguments builds the exact client argument vector for one session. It is
// pure so the vector a session runs is the vector a test can read.
func arguments(session machine.Session, held paths) []string {
	args := []string{"-F", descriptorPath(held.config)}
	if held.identityFile != 0 {
		args = append(args, "-i", descriptorPath(held.identityFile))
	}
	if held.privateKey != 0 {
		args = append(args, "-i", descriptorPath(held.privateKey))
	}
	args = append(args, authentication(session)...)
	if session.Port != 0 && session.Port != 22 {
		args = append(args, "-p", strconv.Itoa(session.Port))
	}
	for _, option := range fixedOptions {
		args = append(args, "-o", option)
	}
	args = append(args,
		"-o", "UserKnownHostsFile="+descriptorPath(held.knownHosts),
		"-o", "StrictHostKeyChecking=yes",
		"-o", "HostKeyAlgorithms="+session.HostKey.Algorithms(),
	)
	if session.User != "" {
		args = append(args, "-l", session.User)
	}
	args = append(args, session.Address)
	for _, word := range session.Command {
		args = append(args, quoteWord(word))
	}
	return args
}

// authentication restricts the client to the one method this identity uses.
// A key arm that also fell back to a password would offer the operator's
// terminal to a prompt the identity never authorized.
func authentication(session machine.Session) []string {
	switch session.Kind {
	case machine.IdentityPassword:
		return []string{
			"-o", "IdentityAgent=none",
			"-o", "PubkeyAuthentication=no",
			"-o", "PreferredAuthentications=password,keyboard-interactive",
		}
	case machine.IdentityKey:
		// IdentitiesOnly confines the client to the keys named above, so the
		// running account's own default identities are never offered with them.
		return []string{
			"-o", "IdentitiesOnly=yes",
			"-o", "IdentityAgent=none",
			"-o", "PreferredAuthentications=publickey",
			"-o", "PasswordAuthentication=no",
			"-o", "KbdInteractiveAuthentication=no",
		}
	default:
		return []string{
			"-o", "IdentityAgent=none",
			"-o", "PreferredAuthentications=publickey",
			"-o", "PasswordAuthentication=no",
			"-o", "KbdInteractiveAuthentication=no",
		}
	}
}

// observation builds the vector that reads a server's host key without
// offering any credential. Authentication is expected to fail: the key is
// recorded during the handshake that precedes it.
func observation(address string, port, config int, knownHosts string, seconds int) []string {
	args := []string{
		"-F", descriptorPath(config),
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=" + knownHosts,
		"-o", "BatchMode=yes",
		"-o", "PubkeyAuthentication=no",
		"-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no",
		"-o", "IdentityAgent=none",
		"-o", "ConnectTimeout=" + strconv.Itoa(seconds),
	}
	for _, option := range fixedOptions {
		args = append(args, "-o", option)
	}
	if port != 0 && port != 22 {
		args = append(args, "-p", strconv.Itoa(port))
	}
	return append(args, address, "true")
}

// quoteWord quotes one argument for the remote shell. The client joins the
// words it is given with spaces and the remote shell splits them again, so a
// value reaches the remote command unchanged only when it is quoted here.
func quoteWord(value string) string {
	if value != "" && !strings.ContainsFunc(value, func(r rune) bool { return !shellSafe(r) }) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func shellSafe(r rune) bool {
	if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
		return true
	}
	return strings.ContainsRune("-_=+:,./@%", r)
}
