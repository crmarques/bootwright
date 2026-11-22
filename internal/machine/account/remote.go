package account

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/nativeexec"
	"github.com/crmarques/bootwright/internal/secrets"
)

type Remote struct {
	Launcher Launcher
	Caller   func(context.Context) (string, error)
	Prompt   func(context.Context, string, []byte) (int, error)
	Hardware string
}

type credentialsKey struct{}
type commandOptions struct{ machine, keyFile string }

func WithKeyFile(ctx context.Context, machineName, keyFile string) context.Context {
	return context.WithValue(ctx, credentialsKey{}, commandOptions{machineName, keyFile})
}

func (r Remote) bootstrap(ctx context.Context, contextName string, place machine.Placement, access machine.Bootstrap,
	bound map[string]secrets.Material, elevate bool, command []string) ([]byte, error) {
	host, err := machine.BoundHostKey(contextName, place.Machine, place.Address, place.Port, place.KnownHostsRef, bound)
	if err != nil {
		return nil, err
	}
	user := access.User
	if user == "" && r.Caller != nil {
		user, err = r.Caller(ctx)
	}
	if err != nil || !validUser(user) || user == place.User || user == "root" && access.Sudo {
		return nil, refused(place, "bootstrap account", "choose a distinct, valid existing remote administrator")
	}
	session := machine.Session{Machine: place.Machine, Address: place.Address, Port: place.Port, HostKey: host,
		Identity: machine.Identity{User: user}, Command: command, Batch: true}
	if r.Launcher == nil {
		return nil, refused(place, "first access", "run this command on the configured controller")
	}
	if err := r.sshCredential(ctx, &session, access, bound); err != nil {
		return nil, err
	}
	defer clear(session.Password)
	defer clear(session.PrivateKey)
	var password []byte
	if elevate && access.Sudo {
		args := []string{sudoExecutable, "-H", "-k"}
		if access.SudoPrompt || access.SudoRef != "" {
			password, err = r.password(ctx, place.Machine, user, "remote sudo", access.SudoRef, bound)
			if err != nil {
				return nil, err
			}
			defer clear(password)
			args = append(args, "-S", "-p", sudoChallenge)
		} else {
			args = append(args, "-n")
		}
		session.Command = append(append(args, "-u", "root", "--"), command...)
	}
	output, err := r.run(ctx, session, password)
	if err != nil {
		return nil, remoteRefusal(err, place, user, "first access as "+user, "check SSH authentication and, for privileged work, that this account can sudo to root")
	}
	return output, nil
}

func (r Remote) sshCredential(ctx context.Context, session *machine.Session, access machine.Bootstrap, bound map[string]secrets.Material) error {
	options, offered := ctx.Value(credentialsKey{}).(commandOptions)
	override := offered && options.machine == session.Machine && options.keyFile != ""
	if override && access.Auth != "operatorIdentity" {
		return machine.EnrollmentRefused("Machine/"+session.Machine+" selects a different bootstrap authentication method", "omit --ssh-id-file or declare operatorIdentity bootstrap")
	}
	switch access.Auth {
	case "operatorIdentity":
		session.Kind = machine.IdentityOperator
		if override {
			file, err := r.Launcher.IdentityFile(ctx, options.keyFile)
			if err != nil {
				return err
			}
			session.Kind, session.IdentityFile = machine.IdentityOfferedKey, file
		}
	case "privateKeyRef":
		session.Kind = machine.IdentityKey
		session.PrivateKey, _ = bound[access.AuthRef].Part(secrets.PrivateKeyPart)
	case "askPassword", "passwordRef":
		password, err := r.password(ctx, session.Machine, session.User, "SSH", access.AuthRef, bound)
		if err != nil {
			return err
		}
		session.Kind, session.Password, session.Batch = machine.IdentityPassword, password, false
	}
	return nil
}

func (r Remote) password(ctx context.Context, machineName, user, purpose, reference string, bound map[string]secrets.Material) ([]byte, error) {
	fail := func() error {
		return machine.EnrollmentRefused("Machine/"+machineName+" needs a "+purpose+" password for "+user,
			"run the enrollment in a terminal, or bind a usernamePassword Secret for this account")
	}
	var password []byte
	if reference != "" {
		username, _ := bound[reference].Part(secrets.UsernamePart)
		defer clear(username)
		if string(username) != user {
			return nil, fail()
		}
		password, _ = bound[reference].Part(secrets.PasswordPart)
	} else {
		password = r.prompted(ctx, fmt.Sprintf("%s password for %s on Machine/%s: ", purpose, user, machineName))
	}
	if len(password) == 0 || len(password) > 4096 || bytes.ContainsAny(password, "\x00\r\n") {
		clear(password)
		return nil, fail()
	}
	return password, nil
}

const (
	sudoExecutable = "/usr/bin/sudo"
	sudoChallenge  = "[bootwright remote sudo password]"
	sudoTTYRefusal = "sudo: sorry, you must have a tty to run sudo"
)

var errSudoTTYRequired = errors.New("remote sudo requires a terminal")

func (r Remote) run(ctx context.Context, session machine.Session, password []byte) ([]byte, error) {
	if r.Launcher == nil {
		return nil, fmt.Errorf("SSH launcher unavailable")
	}
	bounded, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	output := nativeexec.Refusing(64 << 10)
	input, stderr := sudoPasswordExchange(bounded, password)
	var sudoOutput *nativeexec.Output
	if len(session.Command) > 0 && session.Command[0] == sudoExecutable {
		sudoOutput = nativeexec.Tail(4 << 10)
		defer sudoOutput.Clear()
		stderr = io.MultiWriter(stderr, sudoOutput)
	}
	if input != nil {
		piped, release, err := nativeexec.PipeInput(input, cancel)
		if err != nil {
			return nil, err
		}
		defer release()
		input = piped
	}
	code, err := r.Launcher.Run(bounded, session, input, output, stderr)
	if err != nil || code != 0 || output.Overflowed() {
		output.Clear()
		if err == nil && code == 1 && sudoOutput != nil {
			stderr := sudoOutput.Bytes()
			if sudoOutput.Overflowed() {
				_, stderr, _ = bytes.Cut(stderr, []byte{'\n'})
			}
			for line := range bytes.SplitSeq(stderr, []byte{'\n'}) {
				if bytes.Equal(bytes.TrimSpace(line), []byte(sudoTTYRefusal)) {
					return nil, errSudoTTYRequired
				}
			}
		}
		return nil, fmt.Errorf("SSH command did not complete")
	}
	return output.Bytes(), nil
}

func remoteRefusal(err error, place machine.Placement, user, step, remedy string) error {
	if errors.Is(err, errSudoTTYRequired) {
		return machine.EnrollmentRefused("Machine/"+place.Machine+" remote sudo requires a terminal for "+user+" (requiretty)",
			"have the host administrator authorize sudo without a terminal for this account, then repeat the operation")
	}
	return refused(place, step, remedy)
}

func refused(place machine.Placement, step, remedy string) error {
	return machine.EnrollmentRefused("Machine/"+place.Machine+" could not prove "+step, remedy)
}

type promptKey struct{}
type invocationPrompts struct {
	lock   sync.Mutex
	values map[string][]byte
}

func BeginInvocation(ctx context.Context) (context.Context, func()) {
	scope := &invocationPrompts{values: map[string][]byte{}}
	return context.WithValue(ctx, promptKey{}, scope), func() {
		scope.lock.Lock()
		defer scope.lock.Unlock()
		for _, value := range scope.values {
			clear(value)
		}
		clear(scope.values)
	}
}

func (r Remote) prompted(ctx context.Context, label string) []byte {
	scope, ok := ctx.Value(promptKey{}).(*invocationPrompts)
	if !ok || r.Prompt == nil {
		return nil
	}
	scope.lock.Lock()
	defer scope.lock.Unlock()
	if value := scope.values[label]; value != nil {
		return slices.Clone(value)
	}
	buffer := make([]byte, 4097)
	defer clear(buffer)
	n, err := r.Prompt(ctx, label, buffer)
	if err != nil || n < 1 || n > len(buffer) {
		return nil
	}
	value := bytes.TrimSuffix(buffer[:n], []byte("\n"))
	scope.values[label] = slices.Clone(value)
	return slices.Clone(value)
}

type challenge struct {
	ready   chan struct{}
	pending []byte
	match   sync.Once
}

func (c *challenge) Write(data []byte) (int, error) {
	prefix := []byte(sudoChallenge)
	for _, b := range data {
		c.pending = append(c.pending, b)
		if bytes.Equal(c.pending, prefix) {
			c.match.Do(func() { close(c.ready) })
			c.pending = c.pending[:0]
		}
		for len(c.pending) > 0 && !bytes.HasPrefix(prefix, c.pending) {
			c.pending = c.pending[1:]
		}
	}
	return len(data), nil
}

type sudoPasswordReader struct {
	ctx    context.Context
	ready  <-chan struct{}
	secret io.Reader
}

func (r sudoPasswordReader) Read(dst []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, io.EOF
	case <-r.ready:
		return r.secret.Read(dst)
	}
}

func sudoPasswordExchange(ctx context.Context, password []byte) (io.Reader, io.Writer) {
	if len(password) == 0 {
		return nil, io.Discard
	}
	exchange := &challenge{ready: make(chan struct{}), pending: make([]byte, 0, len(sudoChallenge))}
	return sudoPasswordReader{ctx, exchange.ready, io.MultiReader(bytes.NewReader(password), bytes.NewReader([]byte{'\n'}))}, exchange
}
