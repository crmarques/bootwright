//go:build linux && amd64

package sshlocal

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/trust"
	"golang.org/x/sys/unix"
)

const (
	// Client is the one SSH client this product runs. It is an absolute path
	// verified before each launch, never a name resolved through an ambient
	// PATH, so the program a session runs is the program it named.
	Client = "/usr/bin/ssh"

	observationTimeout = 10 * time.Second
	terminationGrace   = 5 * time.Second
	maxObservedBytes   = 64 << 10
)

// terminalEnvironment is everything a session's client inherits. A caller
// cannot reach the client through an askpass helper, an agent socket, a
// dynamic loader or a crypto-provider override, because none of those names
// crosses this boundary.
var terminalEnvironment = []string{"TERM", "COLORTERM", "NO_COLOR"}

// Launcher runs the pinned SSH client. Home resolves the invoking account's
// home directory, so an operator-supplied key path expands from the account
// database rather than an ambient HOME, and Owner resolves that account's user
// ID, which an offered key file must be owned by. Each is called only when a
// key is offered, because an invocation that offers none acquires no account
// capability. Scratch parents the short-lived directory an observation records
// into.
type Launcher struct {
	Client     string
	Home       func() (string, error)
	Owner      func() (int, error)
	PolicyPath string
	Scratch    string
	Environ    func() []string
}

func New(home func() (string, error), owner func() (int, error)) Launcher {
	return Launcher{Client: Client, Home: home, Owner: owner, PolicyPath: PolicyPath, Scratch: "/run", Environ: os.Environ}
}

// Run opens one session and returns the client's own exit status. The streams
// belong to the client for its whole duration; this process adds nothing to
// them and waits rather than replacing itself, so the material it holds is
// released only once the session has ended.
func (l Launcher) Run(ctx context.Context, session machine.Session, in io.Reader, out, errOut io.Writer) (int, error) {
	client, err := l.executable()
	if err != nil {
		return 0, err
	}
	if !session.HostKey.Present() {
		return 0, failure("a session requires the host key it pins", "")
	}
	policy, err := l.policy()
	if err != nil {
		return 0, err
	}
	held, files, err := l.materialize(policy, session)
	defer closeAll(files)
	if err != nil {
		return 0, err
	}
	command := exec.CommandContext(ctx, client, arguments(session, held)...)
	command.Stdin, command.Stdout, command.Stderr = in, out, errOut
	command.Env = l.environment()
	command.ExtraFiles = files
	command.Dir = "/"
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
	command.WaitDelay = terminationGrace
	if err := command.Run(); err != nil {
		return exitStatus(err)
	}
	return 0, nil
}

// Observe reads the key a server presents without offering any credential.
// Authentication is expected to fail, so the client's exit status is
// deliberately ignored: the key is recorded during the handshake that precedes
// it, and the recorded file is the only source of truth.
func (l Launcher) Observe(ctx context.Context, address string, port int) (trust.HostKey, error) {
	client, err := l.executable()
	if err != nil {
		return trust.HostKey{}, err
	}
	if address == "" {
		return trust.HostKey{}, failure("a host-key observation names no address", "")
	}
	policy, err := l.policy()
	if err != nil {
		return trust.HostKey{}, err
	}
	directory, err := os.MkdirTemp(l.Scratch, "bootwright-hostkey-")
	if err != nil {
		return trust.HostKey{}, failure("private host-key observation storage is unavailable", "")
	}
	defer os.RemoveAll(directory)
	if err := os.Chmod(directory, 0700); err != nil {
		return trust.HostKey{}, failure("private host-key observation storage is unsafe", "")
	}
	recorded := filepath.Join(directory, "known_hosts")
	config, err := anonymous(directory, policy)
	if err != nil {
		return trust.HostKey{}, err
	}
	defer config.Close()
	observe, cancel := context.WithTimeout(ctx, observationTimeout)
	defer cancel()
	command := exec.CommandContext(observe, client,
		observation(address, port, 3, recorded, int(observationTimeout/time.Second))...)
	command.Env = l.environment()
	command.ExtraFiles = []*os.File{config}
	command.Dir = "/"
	command.Stdin, command.Stdout, command.Stderr = nil, io.Discard, io.Discard
	command.WaitDelay = terminationGrace
	_ = command.Run()
	if err := ctx.Err(); err != nil {
		return trust.HostKey{}, err
	}
	data, err := os.ReadFile(recorded)
	if err != nil || len(data) == 0 {
		return trust.HostKey{}, keyFailure("no SSH host key was observed at "+trust.HostToken(address, port),
			"check that the Machine is running and reachable on that address")
	}
	if len(data) > maxObservedBytes {
		return trust.HostKey{}, keyFailure("the observed SSH host key is not bounded", "")
	}
	return trust.ParseKnownHostsLine(preferred(string(data)), address, port)
}

// preferred selects one observed entry. A client records exactly the key its
// negotiated handshake used, so a second line would be a second connection;
// keeping the first keeps the key the connection actually proved.
func preferred(recorded string) string {
	for _, line := range strings.Split(recorded, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return trimmed
		}
	}
	return ""
}

// IdentityFile resolves an operator-supplied key path. A leading tilde comes
// from the invoking account database rather than an ambient HOME. The file is
// proved here, so a refusal comes before any host key is sought, and proved
// again on the descriptor a session hands the client.
func (l Launcher) IdentityFile(path string) (string, error) {
	raw := strings.TrimSpace(path)
	if raw == "" {
		return "", nil
	}
	if strings.HasPrefix(raw, "~") {
		home := ""
		if l.Home != nil {
			resolved, err := l.Home()
			if err != nil {
				return "", failure("--ssh-id-file names a home directory this invocation cannot resolve",
					"supply an absolute path")
			}
			home = resolved
		}
		if home == "" {
			return "", failure("--ssh-id-file names a home directory this invocation cannot resolve",
				"supply an absolute path")
		}
		raw = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(raw, "~"), "/"))
	}
	resolved, err := filepath.Abs(raw)
	if err != nil {
		return "", failure("--ssh-id-file is not a resolvable path", "")
	}
	file, err := l.openIdentity(resolved)
	if err != nil {
		return "", err
	}
	_ = file.Close()
	return resolved, nil
}

// openIdentity resolves an offered key to a path-only descriptor and proves
// what that descriptor names. Resolving a name this way neither follows a link
// at its last component nor runs a device's or FIFO's open routine, so nothing
// the name reaches is opened for reading before its type is known. Every check
// reads that descriptor rather than the name, and the client opens the key
// through the same descriptor, so the file it reads is the file that was
// proved.
func (l Launcher) openIdentity(path string) (*os.File, error) {
	owner := -1
	if l.Owner != nil {
		if resolved, err := l.Owner(); err == nil {
			owner = resolved
		}
	}
	if owner < 0 {
		return nil, failure("--ssh-id-file "+path+" cannot be offered, because the invoking account cannot be verified",
			"omit --ssh-id-file to use the Machine's own identity")
	}
	held, err := unix.Open(path, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, failure("--ssh-id-file "+path+" cannot be opened", "name an existing private key file")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(held, &stat); err != nil {
		_ = unix.Close(held)
		return nil, failure("--ssh-id-file "+path+" cannot be opened", "name an existing private key file")
	}
	var refusal error
	switch {
	case stat.Mode&unix.S_IFMT == unix.S_IFLNK:
		refusal = failure("--ssh-id-file "+path+" is a symbolic link", "name the key file itself rather than a link to it")
	case stat.Mode&unix.S_IFMT != unix.S_IFREG:
		refusal = failure("--ssh-id-file "+path+" is not a regular file", "name the private key file itself")
	case int(stat.Uid) != owner:
		refusal = failure("--ssh-id-file "+path+" is not owned by the invoking account",
			"offer a key file that account owns")
	case stat.Mode&0o077 != 0:
		refusal = failure("--ssh-id-file is readable by group or other",
			"remove those permissions with chmod 600 "+path)
	}
	if refusal != nil {
		_ = unix.Close(held)
		return nil, refusal
	}
	return os.NewFile(uintptr(held), path), nil
}

// executable refuses anything but the pinned regular executable, so a session
// cannot be redirected to a replacement by a changed path.
func (l Launcher) executable() (string, error) {
	client := l.Client
	if client == "" {
		client = Client
	}
	if !filepath.IsAbs(client) {
		return "", failure("the SSH client is not an absolute executable", "")
	}
	info, err := os.Stat(client)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", failure("the pinned SSH client "+client+" is unavailable",
			"install an OpenSSH client on this host")
	}
	return client, nil
}

func (l Launcher) policy() ([]byte, error) {
	path := l.PolicyPath
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cryptographicPolicy(nil, false)
	}
	if err != nil {
		return nil, failure("the host SSH crypto policy cannot be read", "")
	}
	return cryptographicPolicy(data, true)
}

// materialize places everything the client reads into files it inherits as
// open descriptors. Each is created privately and unlinked before it is
// written, so the material exists under no name at all and an interruption
// leaves no plaintext behind.
func (l Launcher) materialize(policy []byte, session machine.Session) (paths, []*os.File, error) {
	directory, err := os.MkdirTemp(l.Scratch, "bootwright-ssh-")
	if err != nil {
		return paths{}, nil, failure("private session storage is unavailable", "")
	}
	defer os.RemoveAll(directory)
	if err := os.Chmod(directory, 0700); err != nil {
		return paths{}, nil, failure("private session storage is unsafe", "")
	}
	var files []*os.File
	held := paths{}
	config, err := anonymous(directory, policy)
	if err != nil {
		return paths{}, files, err
	}
	files = append(files, config)
	held.config = 2 + len(files)
	if session.IdentityFile != "" {
		offered, err := l.openIdentity(session.IdentityFile)
		if err != nil {
			return paths{}, files, err
		}
		files = append(files, offered)
		held.identityFile = 2 + len(files)
	}
	if session.Kind == machine.IdentityKey {
		if len(session.PrivateKey) == 0 {
			return paths{}, files, failure("the resolved identity carries no private key", "")
		}
		key, err := anonymous(directory, session.PrivateKey)
		if err != nil {
			return paths{}, files, err
		}
		files = append(files, key)
		held.privateKey = 2 + len(files)
	}
	known, err := anonymous(directory, []byte(session.HostKey.Line(session.Address, session.Port)))
	if err != nil {
		return paths{}, files, err
	}
	files = append(files, known)
	held.knownHosts = 2 + len(files)
	return held, files, nil
}

// anonymous creates one private file, removes its name, and returns the open
// handle the child will inherit.
func anonymous(directory string, data []byte) (*os.File, error) {
	file, err := os.CreateTemp(directory, "material-")
	if err != nil {
		return nil, failure("private session material could not be created", "")
	}
	name := file.Name()
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return nil, failure("private session material could not be secured", "")
	}
	if err := os.Remove(name); err != nil {
		file.Close()
		return nil, failure("private session material could not be unlinked", "")
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return nil, failure("private session material could not be written", "")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, failure("private session material could not be rewound", "")
	}
	return file, nil
}

func (l Launcher) environment() []string {
	read := l.Environ
	if read == nil {
		read = os.Environ
	}
	var out []string
	for _, entry := range read() {
		name, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		for _, allowed := range terminalEnvironment {
			if name == allowed {
				out = append(out, entry)
				break
			}
		}
	}
	return out
}

// exitStatus reports what the client's own termination means. A signalled
// client reports the conventional status a shell would, so an interrupted
// session is distinguishable from a command that failed.
func exitStatus(err error) (int, error) {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return 0, failure("the SSH client could not be started", "")
	}
	if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal()), nil
	}
	return exit.ExitCode(), nil
}

func closeAll(files []*os.File) {
	for _, file := range files {
		_ = file.Close()
	}
}
