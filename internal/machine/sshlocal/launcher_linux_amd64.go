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
)

const (
	// Client is the one SSH client this product runs. It is an absolute path
	// verified before each launch, never a name resolved through an ambient
	// PATH, so the program a session runs is the program it named.
	Client = "/usr/bin/ssh"

	observationTimeout = 10 * time.Second
	terminationGrace   = 5 * time.Second
	maxObservedBytes   = 64 << 10
	maxOfferedKeyBytes = 64 << 10
)

// terminalEnvironment is everything a session's client inherits. A caller
// cannot reach the client through an askpass helper, an agent socket, a
// dynamic loader or a crypto-provider override, because none of those names
// crosses this boundary.
var terminalEnvironment = []string{"TERM", "COLORTERM", "NO_COLOR"}

// Files begins one session's access to an operator-named key under the
// invoking account's credentials.
type Files interface {
	Begin(context.Context) (FileSession, error)
}

// FileSession opens one absolute path without following a link at its final
// component, and opens nothing but a regular file for reading.
type FileSession interface {
	OpenFile(string) (*os.File, error)
	Close() error
}

// deniedToRoot is what an opener's failure reports when the denied open ran
// with root's credentials.
type deniedToRoot interface{ DeniedToRoot() bool }

// Launcher runs the pinned SSH client. Home resolves the invoking account's
// home directory, so an operator-supplied key path expands from the account
// database rather than an ambient HOME, and Owner resolves that account's user
// ID, which an offered key file must be owned by. Each is called only when a
// key is offered, because an invocation that offers none acquires no account
// capability. Files opens an offered key with that account's credentials, so
// this process never opens the operator's path itself. Scratch parents the
// short-lived directory an observation records into.
type Launcher struct {
	Client     string
	Home       func() (string, error)
	Owner      func() (int, error)
	Files      Files
	PolicyPath string
	Scratch    string
	Environ    func() []string
}

func New(home func() (string, error), owner func() (int, error), files Files) Launcher {
	return Launcher{Client: Client, Home: home, Owner: owner, Files: files, PolicyPath: PolicyPath, Scratch: "/run", Environ: os.Environ}
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
	held, files, err := l.materialize(ctx, policy, session)
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
// again on the descriptor the session copies it from.
func (l Launcher) IdentityFile(ctx context.Context, path string) (string, error) {
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
	file, err := l.openIdentity(ctx, resolved)
	if err != nil {
		return "", err
	}
	_ = file.Close()
	return resolved, nil
}

// openIdentity receives an offered key from the invoking account's opener and
// proves what the received descriptor names. The opener follows no link at the
// last component and opens nothing but a regular file for reading, so a
// device's or FIFO's open routine never runs; every check here reads the
// descriptor rather than the name, so the bytes a session copies are the bytes
// of the file that was proved.
func (l Launcher) openIdentity(ctx context.Context, path string) (*os.File, error) {
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
	if l.Files == nil {
		return nil, failure("the --ssh-id-file opener is not configured", "")
	}
	held, err := l.receive(ctx, path)
	if err != nil {
		return nil, err
	}
	info, err := held.Stat()
	if err != nil {
		_ = held.Close()
		return nil, failure("--ssh-id-file "+path+" cannot be opened", "name an existing private key file")
	}
	stat, described := info.Sys().(*syscall.Stat_t)
	var refusal error
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		refusal = failure("--ssh-id-file "+path+" is a symbolic link", "name the key file itself rather than a link to it")
	case !info.Mode().IsRegular():
		refusal = failure("--ssh-id-file "+path+" is not a regular file", "name the private key file itself")
	case !described || int(stat.Uid) != owner:
		refusal = failure("--ssh-id-file "+path+" is not owned by the invoking account",
			"offer a key file that account owns")
	case stat.Mode&0o077 != 0:
		refusal = failure("--ssh-id-file is readable by group or other",
			"remove those permissions with chmod 600 "+path)
	}
	if refusal != nil {
		_ = held.Close()
		return nil, refusal
	}
	return held, nil
}

// receive takes the key's descriptor from the invoking account's opener and
// ends that session; the descriptor outlives it.
func (l Launcher) receive(ctx context.Context, path string) (*os.File, error) {
	session, err := l.Files.Begin(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, failure("--ssh-id-file "+path+" cannot be opened under the invoking account", "")
	}
	file, err := session.OpenFile(path)
	_ = session.Close()
	if err == nil {
		return file, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var denied deniedToRoot
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, failure("--ssh-id-file "+path+" cannot be opened", "name an existing private key file")
	case errors.Is(err, os.ErrPermission) && errors.As(err, &denied) && denied.DeniedToRoot():
		return nil, failure("root cannot read --ssh-id-file "+path+" (a network home with root squash?)",
			"copy it to a local directory and name the copy")
	case errors.Is(err, os.ErrPermission):
		return nil, failure("the invoking account cannot read --ssh-id-file "+path+" (permission denied)",
			"give the invoking account read access to it, or offer a key that account can read")
	}
	return nil, failure("--ssh-id-file "+path+" cannot be opened", "name an existing private key file")
}

// offeredCopy reads a proved key through its received descriptor into private
// session material. The client runs as root and would reopen a handed
// descriptor through /proc/self/fd, which a root-squashed home refuses, so it
// is given this copy instead.
func (l Launcher) offeredCopy(ctx context.Context, directory, path string) (*os.File, error) {
	held, err := l.openIdentity(ctx, path)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(held, maxOfferedKeyBytes+1))
	_ = held.Close()
	defer clear(data)
	if err != nil {
		return nil, failure("--ssh-id-file "+path+" cannot be read", "name a readable private key file")
	}
	if len(data) > maxOfferedKeyBytes {
		return nil, failure("--ssh-id-file "+path+" exceeds 64 KiB, the bound of a private key file",
			"name the private key file itself")
	}
	return anonymous(directory, data)
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
func (l Launcher) materialize(ctx context.Context, policy []byte, session machine.Session) (paths, []*os.File, error) {
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
		offered, err := l.offeredCopy(ctx, directory, session.IdentityFile)
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
