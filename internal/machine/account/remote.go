package account

import (
	"bytes"
	"context"
	"encoding/hex"
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
	Entropy  func([]byte) (int, error)
	Hardware string
}

type credentialsKey struct{}
type commandOptions struct{ machine, keyFile string }

func WithKeyFile(ctx context.Context, machineName, keyFile string) context.Context {
	return context.WithValue(ctx, credentialsKey{}, commandOptions{machineName, keyFile})
}

func (r Remote) bootstrap(ctx context.Context, contextName string, place machine.Placement, access machine.Bootstrap,
	bound map[string]secrets.Material, elevate bool, command, probe []string) ([]byte, error) {
	host, err := machine.BoundHostKey(contextName, place.Machine, place.Address, place.Port, place.KnownHostsRef, bound)
	if err != nil {
		return nil, err
	}
	user, err := r.bootstrapUser(ctx, place, access)
	if err != nil {
		return nil, err
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
	var output []byte
	if elevate && access.Sudo {
		args := []string{sudoExecutable, "-H", "-k"}
		var password []byte
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
		args = append(args, "-u", "root", "--")
		output, err = r.bootstrapSudo(ctx, contextName, session, args, probe, password)
	} else {
		output, err = r.run(ctx, session, nil)
	}
	if err != nil {
		return nil, remoteRefusal(err, place, user, "first access as "+user, "check SSH authentication and, for privileged work, that this account can sudo to root")
	}
	return output, nil
}

type terminalTarget struct {
	context, machine, address, user string
	port                            int
}

func (r Remote) bootstrapSudo(ctx context.Context, contextName string, session machine.Session, sudo, probe []string, password []byte) ([]byte, error) {
	scope, _ := ctx.Value(invocationKey{}).(*invocationAccess)
	target := terminalTarget{contextName, session.Machine, session.Address, session.User, session.Port}
	useTTY, known := scope.sudoTerminal(target)
	effect := session.Command
	command := effect
	if len(probe) != 0 && !known {
		command = probe
	}
	session.Command = slices.Concat(sudo, command)
	session.ForceTTY = useTTY
	output, err := r.run(ctx, session, password)
	observing := !known || len(probe) == 0
	if observing && !useTTY && errors.Is(err, errSudoTTYRequired) {
		session.ForceTTY = true
		output, err = r.run(ctx, session, password)
		if err != nil && !errors.Is(err, errBootstrapTTYUnavailable) {
			return nil, errBootstrapTTY
		}
	}
	if err != nil || !observing {
		return output, err
	}
	switch string(bytes.TrimSpace(output)) {
	case "ready", "absent", "partial":
		scope.rememberTerminal(target, session.ForceTTY)
	default:
		return output, nil
	}
	if len(probe) == 0 {
		return output, nil
	}
	session.Command = slices.Concat(sudo, effect)
	return r.run(ctx, session, password)
}

func (r Remote) bootstrapUser(ctx context.Context, place machine.Placement, access machine.Bootstrap) (string, error) {
	user := access.User
	var err error
	if user == "" && r.Caller != nil {
		user, err = r.Caller(ctx)
	}
	if err != nil || !validUser(user) || user == place.User || user == "root" && access.Sudo {
		return "", refused(place, "bootstrap account", "choose a distinct, valid existing remote administrator")
	}
	return user, nil
}

func (r Remote) PromptSSH(ctx context.Context, contextName string, place machine.Placement, access machine.Bootstrap, material map[string]secrets.Material) error {
	if access.Auth != "askPassword" {
		return nil
	}
	if err := place.ProveHostPin(contextName, material); err != nil {
		return err
	}
	user, err := r.bootstrapUser(ctx, place, access)
	if err != nil {
		return err
	}
	password, err := r.password(ctx, place.Machine, user, "SSH", "", material)
	clear(password)
	return err
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
		password = r.prompted(ctx, promptedCredential{user, purpose}, machineName)
	}
	if !validPassword(password) {
		clear(password)
		return nil, fail()
	}
	return password, nil
}

func validPassword(password []byte) bool {
	return len(password) != 0 && len(password) <= 4096 && !bytes.ContainsAny(password, "\x00\r\n")
}

const (
	sudoExecutable = "/usr/bin/sudo"
	sudoChallenge  = "[bootwright remote sudo password]"
	sudoTTYRefusal = "sudo: sorry, you must have a tty to run sudo"
)

var (
	errSudoTTYRequired         = errors.New("remote sudo requires a terminal")
	errBootstrapTTY            = errors.New("bootstrap sudo could not be proved in a terminal")
	errBootstrapTTYUnavailable = errors.New("bootstrap terminal could not be prepared")
)

const prepareTerminal = `if [ ! -t 0 ] || ! /usr/bin/stty raw -echo; then
	printf '%s\n' "$1-denied"
	exit 1
fi
printf '%s\n' "$1" || exit 1
shift
exec "$@"`

func (r Remote) run(ctx context.Context, session machine.Session, password []byte) ([]byte, error) {
	if r.Launcher == nil {
		return nil, fmt.Errorf("SSH launcher unavailable")
	}
	bounded, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	output := nativeexec.Refusing(64 << 10)
	var stdout io.Writer = output
	var stderr io.Writer
	var input io.Reader
	var sudoOutput *nativeexec.Output
	var marker, denial string
	sudo := len(session.Command) > 0 && session.Command[0] == sudoExecutable
	if sudo {
		session.Command = slices.Concat([]string{"/usr/bin/env", "LC_ALL=C"}, session.Command)
	}
	if session.ForceTTY {
		if r.Entropy == nil {
			return nil, errBootstrapTTY
		}
		var nonce [16]byte
		if n, err := r.Entropy(nonce[:]); err != nil || n != len(nonce) {
			return nil, errBootstrapTTY
		}
		token := hex.EncodeToString(nonce[:])
		marker = token + "\n"
		denial = token + "-denied"
		session.Command = slices.Concat([]string{"/bin/sh", "-c", prepareTerminal, "sh", token}, session.Command)
		var exchange io.Writer
		input, exchange = sudoPasswordExchange(bounded, password, marker, sudoChallenge)
		stdout, stderr = io.MultiWriter(output, exchange), io.Discard
	} else {
		input, stderr = sudoPasswordExchange(bounded, password, sudoChallenge)
		if sudo {
			sudoOutput = nativeexec.Tail(4 << 10)
			defer sudoOutput.Clear()
			stderr = io.MultiWriter(stderr, sudoOutput)
		}
	}
	if input != nil {
		piped, release, err := nativeexec.PipeInput(input, cancel)
		if err != nil {
			return nil, err
		}
		defer release()
		input = piped
	}
	code, err := r.Launcher.Run(bounded, session, input, stdout, stderr)
	if err != nil || code != 0 || output.Overflowed() {
		defer output.Clear()
		if err == nil && code == 1 {
			if denial != "" && !output.Overflowed() && !bytes.Contains(output.Bytes(), []byte(marker)) &&
				containsLine(output.Bytes(), denial) {
				return nil, errBootstrapTTYUnavailable
			}
			if sudoOutput != nil {
				stderr := sudoOutput.Bytes()
				if sudoOutput.Overflowed() {
					_, stderr, _ = bytes.Cut(stderr, []byte{'\n'})
				}
				if containsLine(stderr, sudoTTYRefusal) {
					return nil, errSudoTTYRequired
				}
			}
		}
		return nil, fmt.Errorf("SSH command did not complete")
	}
	if marker != "" {
		defer output.Clear()
		_, proof, present := bytes.Cut(output.Bytes(), []byte(marker))
		if !present {
			return nil, errBootstrapTTY
		}
		if _, afterPrompt, prompted := bytes.Cut(proof, []byte(sudoChallenge)); prompted {
			proof = afterPrompt
		}
		return slices.Clone(proof), nil
	}
	return output.Bytes(), nil
}

func containsLine(output []byte, want string) bool {
	for line := range bytes.SplitSeq(output, []byte{'\n'}) {
		if bytes.Equal(bytes.TrimSpace(line), []byte(want)) {
			return true
		}
	}
	return false
}

func remoteRefusal(err error, place machine.Placement, user, step, remedy string) error {
	if errors.Is(err, errBootstrapTTYUnavailable) {
		return machine.EnrollmentSudoTTYUnavailable(place.Machine, user)
	}
	if errors.Is(err, errBootstrapTTY) {
		return machine.EnrollmentRefused("Machine/"+place.Machine+" could not prove remote sudo in a terminal for "+user,
			"check that sshd allows terminal allocation and that this account can sudo to root")
	}
	if errors.Is(err, errSudoTTYRequired) {
		return machine.EnrollmentSudoTTYRefused(place.Machine, user)
	}
	return refused(place, step, remedy)
}

func refused(place machine.Placement, step, remedy string) error {
	return machine.EnrollmentRefused("Machine/"+place.Machine+" could not prove "+step, remedy)
}

type invocationKey struct{}
type promptedCredential struct{ user, purpose string }

type invocationAccess struct {
	lock      sync.Mutex
	values    map[promptedCredential][]byte
	terminals map[terminalTarget]bool
}

func (p *invocationAccess) sudoTerminal(target terminalTarget) (bool, bool) {
	if p == nil {
		return false, false
	}
	p.lock.Lock()
	defer p.lock.Unlock()
	terminal, known := p.terminals[target]
	return terminal, known
}

func (p *invocationAccess) rememberTerminal(target terminalTarget, terminal bool) {
	if p != nil {
		p.lock.Lock()
		defer p.lock.Unlock()
		p.terminals[target] = terminal
	}
}

func BeginInvocation(ctx context.Context) (context.Context, func()) {
	scope := &invocationAccess{values: map[promptedCredential][]byte{}, terminals: map[terminalTarget]bool{}}
	return context.WithValue(ctx, invocationKey{}, scope), func() {
		scope.lock.Lock()
		defer scope.lock.Unlock()
		for _, value := range scope.values {
			clear(value)
		}
		clear(scope.values)
		clear(scope.terminals)
	}
}

func (r Remote) prompted(ctx context.Context, credential promptedCredential, machineName string) []byte {
	scope, ok := ctx.Value(invocationKey{}).(*invocationAccess)
	if !ok || r.Prompt == nil {
		return nil
	}
	scope.lock.Lock()
	defer scope.lock.Unlock()
	if value := scope.values[credential]; value != nil {
		return slices.Clone(value)
	}
	label := fmt.Sprintf("%s password for %s on Machine/%s", credential.purpose, credential.user, machineName)
	var reuse []byte
	if credential.purpose == "remote sudo" {
		reuse = scope.values[promptedCredential{credential.user, "SSH"}]
		if len(reuse) != 0 {
			label += " (Enter to reuse SSH password)"
		}
	}
	buffer := make([]byte, 4097)
	defer clear(buffer)
	n, err := r.Prompt(ctx, label+": ", buffer)
	if err != nil || ctx.Err() != nil || n < 1 || n > len(buffer) {
		return nil
	}
	value := bytes.TrimSuffix(buffer[:n], []byte("\n"))
	if n == 1 && buffer[0] == '\n' {
		value = reuse
	}
	if !validPassword(value) {
		return nil
	}
	scope.values[credential] = slices.Clone(value)
	return slices.Clone(value)
}

type challenge struct {
	ready   chan struct{}
	pending []byte
	tokens  []string
}

func (c *challenge) Write(data []byte) (int, error) {
	for _, b := range data {
		if len(c.tokens) == 0 {
			break
		}
		prefix := []byte(c.tokens[0])
		c.pending = append(c.pending, b)
		if bytes.Equal(c.pending, prefix) {
			c.tokens = c.tokens[1:]
			c.pending = c.pending[:0]
			if len(c.tokens) == 0 {
				close(c.ready)
			}
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

func sudoPasswordExchange(ctx context.Context, password []byte, tokens ...string) (io.Reader, io.Writer) {
	if len(password) == 0 {
		return nil, io.Discard
	}
	exchange := &challenge{ready: make(chan struct{}), tokens: tokens}
	return sudoPasswordReader{ctx, exchange.ready, io.MultiReader(bytes.NewReader(password), bytes.NewReader([]byte{'\n'}))}, exchange
}
