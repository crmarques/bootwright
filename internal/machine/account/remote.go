package account

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
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
		return nil, bootstrapRefusal(err, place, user)
	}
	return output, nil
}

func bootstrapRefusal(err error, place machine.Placement, user string) error {
	var known *cause
	if errors.As(err, &known) {
		names := strings.NewReplacer("{login}", user, "{managed}", place.User)
		return machine.EnrollmentRefused("Machine/"+place.Machine+" "+names.Replace(known.reason), names.Replace(known.remedy))
	}
	return remoteRefusal(err, place, user, "first access as "+user, "check SSH authentication and, for privileged work, that this account can sudo to root")
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
		var known *cause
		if err != nil && !errors.Is(err, errBootstrapTTYUnavailable) && !errors.As(err, &known) {
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

func (r Remote) PromptSSH(ctx context.Context, _ string, place machine.Placement, access machine.Bootstrap, material map[string]secrets.Material) error {
	if access.Auth != "askPassword" {
		return nil
	}
	user, err := r.bootstrapUser(ctx, place, access)
	if err != nil {
		return err
	}
	password, err := r.password(ctx, place.Machine, user, "SSH", "", material)
	clear(password)
	return err
}

func (r Remote) collect(ctx context.Context, contextName string, request Request, material map[string]secrets.Material) error {
	place, access := request.Placement, request.Bootstrap
	if err := r.PromptSSH(ctx, contextName, place, access, material); err != nil || !access.SudoPrompt {
		return err
	}
	user, err := r.bootstrapUser(ctx, place, access)
	if err != nil {
		return err
	}
	password, err := r.password(ctx, place.Machine, user, "remote sudo", "", material)
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
	errIncomplete              = errors.New("SSH command did not complete")
)

type cause struct{ reason, remedy string }

func (c *cause) Error() string { return c.reason }

var (
	errSSHSession = &cause{"SSH session as {login} did not open or ended early",
		"check that the host is reachable, that its pinned host key is current, and this account's SSH credential"}
	errSSHDropped = &cause{"SSH session as {login} ended before the remote command finished",
		"check the connection to the host, then repeat the operation"}
	errSudoPasswordRefused = &cause{"remote sudo did not accept the password for {login}",
		"check this account's remote sudo password; sudo asked again, which also happens when it needs a second authentication factor"}
	errSudoPasswordRequired = &cause{"remote sudo requires a password for {login}",
		"declare privilege.sudo.askPassword or privilege.sudo.passwordRef in the Machine's bootstrap, or grant this account passwordless sudo"}
	errSudoDenied = &cause{"does not authorize {login} to run the enrollment as root through sudo",
		"grant this account sudo to root on the host, or declare another bootstrap user"}
	errSudoFailed = &cause{"remote sudo reported an error for {login} and the enrollment did not complete",
		"run sudo -v as this account on the host and resolve what it reports; if sudo works there, inspect the account {managed}, /etc/sudoers.d/90-bootwright-{managed} and /home/{managed}-*"}
	errSessionStalled = &cause{"did not reach the remote sudo prompt for {login} within 90 seconds",
		"check that the host is reachable and that this account's login starts a command without waiting for input"}
	errSudoPromptStalled = &cause{"remote sudo did not ask {login} for its password within 90 seconds",
		"run sudo -v as this account on the host; a different prompt or another authentication step is not supported"}
	errRemoteStalled = &cause{"did not finish the remote command for {login} within 90 seconds",
		"check that the host is reachable and responsive, then repeat the operation"}
)

var sudoRefusals = []struct {
	phrase string
	cause  error
}{
	{"is not in the sudoers file", errSudoDenied},
	{"is not allowed to execute", errSudoDenied},
	{"may not run sudo", errSudoDenied},
	{"sudo: a password is required", errSudoPasswordRequired},
	{"incorrect password attempt", errSudoPasswordRefused},
	{"sudo: no password was provided", errSudoPasswordRefused},
}

var sudoWarnings = []string{"sudo: unable to resolve host", "sudo: setrlimit("}

type sudoReport struct {
	line   []byte
	named  error
	failed bool
}

func (s *sudoReport) Write(data []byte) (int, error) {
	for _, b := range data {
		switch {
		case b == '\n':
			s.judge()
		case len(s.line) < 256:
			s.line = append(s.line, b)
		}
	}
	return len(data), nil
}

func (s *sudoReport) judge() {
	line := strings.TrimSpace(strings.ReplaceAll(string(s.line), sudoChallenge, ""))
	clear(s.line)
	s.line = s.line[:0]
	for _, refusal := range sudoRefusals {
		if strings.Contains(line, refusal.phrase) {
			if s.named == nil {
				s.named = refusal.cause
			}
			return
		}
	}
	warned := slices.ContainsFunc(sudoWarnings, func(warning string) bool { return strings.HasPrefix(line, warning) })
	s.failed = s.failed || strings.HasPrefix(line, "sudo: ") && !warned
}

func (s *sudoReport) cause() error {
	s.judge()
	switch {
	case s.named != nil:
		return s.named
	case s.failed:
		return errSudoFailed
	}
	return nil
}

func interrupted(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	case <-time.After(200 * time.Millisecond):
		return false
	}
}

const remoteSessionBound = 90 * time.Second

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
	bounded, cancel := context.WithTimeout(ctx, remoteSessionBound)
	defer cancel()
	seen := observation{output: nativeexec.Refusing(64 << 10), report: &sudoReport{line: make([]byte, 0, 256)}}
	defer seen.report.judge()
	seen.sudo = len(session.Command) > 0 && session.Command[0] == sudoExecutable
	if seen.sudo {
		session.Command = slices.Concat([]string{"/usr/bin/env", "LC_ALL=C"}, session.Command)
	}
	var stdout, stderr io.Writer
	var input io.Reader
	if session.ForceTTY {
		token, err := r.terminalToken()
		if err != nil {
			return nil, err
		}
		seen.marker, seen.denial = token+"\n", token+"-denied"
		session.Command = slices.Concat([]string{"/bin/sh", "-c", prepareTerminal, "sh", token}, session.Command)
		input, seen.exchange = sudoPasswordExchange(bounded, password, seen.marker, sudoChallenge)
		stdout, stderr = io.MultiWriter(seen.exchange, seen.output), io.Discard
		if seen.sudo {
			stdout = io.MultiWriter(seen.exchange, seen.report, seen.output)
		}
	} else {
		input, seen.exchange = sudoPasswordExchange(bounded, password, sudoChallenge)
		stdout, stderr = seen.output, seen.exchange
		if seen.sudo {
			seen.sudoOutput = nativeexec.Tail(4 << 10)
			defer seen.sudoOutput.Clear()
			stderr = io.MultiWriter(seen.exchange, seen.report, seen.sudoOutput)
		}
	}
	seen.exchange.refuse = cancel
	if input != nil {
		piped, release, err := nativeexec.PipeInput(input, cancel)
		if err != nil {
			return nil, err
		}
		defer release()
		input = piped
	}
	code, err := r.Launcher.Run(bounded, session, input, stdout, stderr)
	if err != nil || code != 0 || seen.output.Overflowed() {
		defer seen.output.Clear()
		return nil, seen.failure(ctx, bounded, code, err)
	}
	return seen.proof()
}

func (r Remote) terminalToken() (string, error) {
	var nonce [16]byte
	if r.Entropy == nil {
		return "", errBootstrapTTY
	}
	if n, err := r.Entropy(nonce[:]); err != nil || n != len(nonce) {
		return "", errBootstrapTTY
	}
	return hex.EncodeToString(nonce[:]), nil
}

type observation struct {
	sudo           bool
	marker, denial string
	output         *nativeexec.Output
	sudoOutput     *nativeexec.Output
	exchange       *challenge
	report         *sudoReport
}

func (o observation) proof() ([]byte, error) {
	if o.marker == "" {
		return o.output.Bytes(), nil
	}
	defer o.output.Clear()
	_, proof, present := bytes.Cut(o.output.Bytes(), []byte(o.marker))
	if !present {
		return nil, errBootstrapTTY
	}
	if _, afterPrompt, prompted := bytes.Cut(proof, []byte(sudoChallenge)); prompted {
		proof = afterPrompt
	}
	return slices.Clone(proof), nil
}

func (o observation) failure(ctx, bounded context.Context, code int, err error) error {
	switch {
	case o.exchange.refused:
		return errSudoPasswordRefused
	case ctx.Err() == nil && errors.Is(bounded.Err(), context.DeadlineExceeded):
		return o.exchange.stalled()
	}
	if refusal := o.terminalRefusal(code, err); refusal != nil {
		return refusal
	}
	if err != nil || ctx.Err() != nil || o.output.Overflowed() {
		return errIncomplete
	}
	switch refused := o.report.cause(); {
	case code == 255 && interrupted(ctx):
	case code == 255 && o.exchange.started:
		return errSSHDropped
	case code == 255:
		return errSSHSession
	case o.sudo && (code == 124 || code == 137):
		return errRemoteStalled
	case o.sudo && code == 3:
		return o.workerRefusal()
	case o.sudo && refused != nil:
		return refused
	case o.sudo && code >= 125 && code <= 127:
		return errProgramUnstartable
	case o.sudo:
		return errRemoteFailed
	}
	return errIncomplete
}

func (o observation) terminalRefusal(code int, err error) error {
	if err != nil || code != 1 {
		return nil
	}
	if o.denial != "" && !o.output.Overflowed() && !bytes.Contains(o.output.Bytes(), []byte(o.marker)) &&
		containsLine(o.output.Bytes(), o.denial) {
		return errBootstrapTTYUnavailable
	}
	if o.sudoOutput == nil {
		return nil
	}
	stderr := o.sudoOutput.Bytes()
	if o.sudoOutput.Overflowed() {
		_, stderr, _ = bytes.Cut(stderr, []byte{'\n'})
	}
	if containsLine(stderr, sudoTTYRefusal) {
		return errSudoTTYRequired
	}
	return nil
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
	if _, held := ctx.Value(invocationKey{}).(*invocationAccess); held {
		return ctx, func() {}
	}
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
	title, reuse := "SSH", []byte(nil)
	if credential.purpose == "remote sudo" {
		title, reuse = "Sudo", scope.values[promptedCredential{credential.user, "SSH"}]
	}
	label := fmt.Sprintf("%s password for %s on Machine/%s", title, credential.user, machineName)
	if len(reuse) != 0 {
		label += " (Enter reuses the SSH password)"
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
	again   string
	refuse  func()
	refused bool
	started bool
}

func (c *challenge) Write(data []byte) (int, error) {
	for _, b := range data {
		want := c.again
		if len(c.tokens) != 0 {
			want = c.tokens[0]
		}
		if want == "" || c.refused {
			break
		}
		c.pending = append(c.pending, b)
		for len(c.pending) > 0 && !strings.HasPrefix(want, string(c.pending)) {
			c.pending = c.pending[1:]
		}
		if string(c.pending) != want {
			continue
		}
		c.pending = c.pending[:0]
		if len(c.tokens) == 0 {
			c.refused = true
			if c.refuse != nil {
				c.refuse()
			}
			break
		}
		if c.tokens = c.tokens[1:]; len(c.tokens) == 0 {
			close(c.ready)
		}
		c.started = true
	}
	return len(data), nil
}

func (c *challenge) stalled() error {
	switch {
	case len(c.tokens) == 0:
		return errRemoteStalled
	case c.started:
		return errSudoPromptStalled
	}
	return errSessionStalled
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

func sudoPasswordExchange(ctx context.Context, password []byte, tokens ...string) (io.Reader, *challenge) {
	if len(password) == 0 {
		return nil, &challenge{}
	}
	exchange := &challenge{ready: make(chan struct{}), tokens: tokens, again: tokens[len(tokens)-1]}
	return sudoPasswordReader{ctx, exchange.ready, io.MultiReader(bytes.NewReader(password), bytes.NewReader([]byte{'\n'}))}, exchange
}
