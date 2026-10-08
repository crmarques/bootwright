package privilege

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/crmarques/bootwright/internal/controller"
)

// Command is one invocation of an already selected executable. Elevated marks
// the elevated child, which bounds its own cancellation, so its executor relays
// the operator's signal and sets no deadline of its own; every other command
// is killed once it outlives its context by the relay grace.
type Command struct {
	Executable    string
	Arguments     []string
	Environment   []string
	Input         io.Reader
	Output, Error io.Writer
	Elevated      bool
}

type Timer struct{}

func (Timer) Wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type SudoOptions struct {
	Executable, Sudo string
	Executor         Executor
	Delay            Delay
	NonInteractive   bool
	// Quiet keeps the supervisor's own refresh warning off the error stream,
	// which a JSON invocation leaves empty.
	Quiet bool
	// Terminal is the terminal type an interactive invocation carries. The
	// elevated child runs the operator's own interactive programs, so it needs
	// the terminal identity the caller had; every other ambient value stays out.
	Terminal string
	// Assignments are the exact acquisition-route variables the elevated child
	// needs, which env_reset would otherwise drop. Sudo parses NAME=value only
	// before the option terminator; after it, the assignment becomes the
	// command sudo tries to execute.
	Assignments []string
	// SecretStdin names the Secret whose value the elevated command reads
	// from standard input, which sudo's I/O log would record.
	SecretStdin   string
	Input         io.Reader
	Output, Error io.Writer
}

// InputLogged refuses a standard-input Secret before sudo starts the child,
// because the policy the probe listed sets Option, so sudo's I/O log would
// record the value as it flows.
type InputLogged struct{ Option, Secret string }

func (e *InputLogged) Error() string {
	return "the sudo policy sets " + e.Option + ", which would log the standard input of Secret " + e.Secret
}

type Supervisor struct{ options SudoOptions }

func NewSupervisor(options SudoOptions) *Supervisor { return &Supervisor{options: options} }

// Run keeps all sudo children attached to the original nonroot parent. Refresh
// failures never invalidate or terminate an operation that is already elevated.
// It returns sudo's own status, which is the child's once the child ran: sudo
// relays an interrupt to the child, which reports it and chooses its status,
// so the interrupting signal never replaces that status.
func (s *Supervisor) Run(ctx context.Context, args []string) (int, error) {
	options := s.options
	if options.Executor == nil || options.Delay == nil || options.Executable == "" || options.Sudo == "" {
		return 1, errors.New("sudo invocation boundary is not configured")
	}
	if err := ctx.Err(); err != nil {
		return 1, err
	}
	var outputLock sync.Mutex
	if options.Output != nil && !inheritedDescriptor(options.Output) {
		options.Output = synchronizedWriter{lock: &outputLock, writer: options.Output}
	}
	if options.Error != nil && !inheritedDescriptor(options.Error) {
		options.Error = synchronizedWriter{lock: &outputLock, writer: options.Error}
	}
	environment := []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	if options.Terminal != "" && safeTerminalName(options.Terminal) {
		environment = append(environment, "TERM="+options.Terminal)
	}
	var policy limitedOutput
	probe, cancelProbe := context.WithTimeout(ctx, 2*time.Second)
	code, probeErr := options.Executor.Run(probe, Command{Executable: options.Sudo, Arguments: []string{"-n", "-u", "#0", "-ll"}, Environment: append([]string(nil), environment...), Output: &policy, Error: io.Discard})
	cancelProbe()
	if err := ctx.Err(); err != nil {
		return 1, err
	}
	interval := 30 * time.Second
	if probeErr == nil && code == 0 && !policy.overflow {
		if option := inputLogging(policy.data); option != "" && options.SecretStdin != "" {
			return 1, &InputLogged{Option: option, Secret: options.SecretStdin}
		}
		interval = refreshInterval(policy.data)
	}
	childArgs := []string{"-u", "#0"}
	if options.NonInteractive {
		childArgs = append(childArgs, "-n")
	}
	for _, assignment := range options.Assignments {
		if !safeAssignment(assignment) {
			return 1, errors.New("acquisition route assignment is outside its allowed vocabulary")
		}
		childArgs = append(childArgs, assignment)
	}
	childArgs = append(childArgs, "--", options.Executable)
	childArgs = append(childArgs, args...)
	refreshCtx, stopRefresh := context.WithCancel(ctx)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if interval == 0 {
			return
		}
		for options.Delay.Wait(refreshCtx, interval) == nil {
			refresh, cancel := context.WithTimeout(refreshCtx, 5*time.Second)
			code, err := options.Executor.Run(refresh, Command{Executable: options.Sudo, Arguments: []string{"-n", "-u", "#0", "-v"}, Environment: append([]string(nil), environment...), Output: io.Discard, Error: io.Discard})
			cancel()
			if err != nil || code != 0 {
				if refreshCtx.Err() == nil && options.Error != nil && !options.Quiet {
					// This warning contains no policy contents or captured sudo text.
					io.WriteString(options.Error, "[WARN] sudo credential refresh stopped; the active operation continues.\n")
				}
				return
			}
		}
	}()
	code, err := options.Executor.Run(ctx, Command{Executable: options.Sudo, Arguments: childArgs, Environment: environment, Input: options.Input, Output: options.Output, Error: options.Error, Elevated: true})
	stopRefresh()
	<-finished
	return code, err
}

// safeAssignment admits only the fixed acquisition-route names, so no authored
// value can introduce another variable into the elevated child.
func safeAssignment(value string) bool {
	name, assigned, found := strings.Cut(value, "=")
	if !found || len(value) > 4352 || !slices.Contains(controller.ProxyEnvironmentNames, name) {
		return false
	}
	for _, c := range assigned {
		if c <= 32 || c >= 127 {
			return false
		}
	}
	return true
}

// safeTerminalName admits only what a terminal type may contain, so an
// attacker-chosen environment value cannot become anything else on the way to
// the elevated child.
func safeTerminalName(value string) bool {
	if len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == '+') {
			return false
		}
	}
	return value != ""
}

// A plain -ll report is not an effective-policy API. Only an explicit matching
// value without bound or backend ambiguity is usable. Unknown policy receives
// the documented best-effort cadence, while sudo remains the authority.
func refreshInterval(report []byte) time.Duration {
	const unknown = 30 * time.Second
	text := string(report)
	if strings.Contains(text, "Runas and Command-specific defaults") || strings.Contains(text, "LDAP Role:") {
		return unknown
	}
	start := strings.Index(text, "Matching Defaults entries for ")
	if start < 0 {
		return unknown
	}
	tail := text[start:]
	colon := strings.Index(tail, ":\n")
	if colon < 0 {
		return unknown
	}
	tail = tail[colon+2:]
	end := strings.Index(tail, "\n\n")
	if end < 0 {
		return unknown
	}
	found := false
	var minutes float64
	fields, valid := defaultFields(tail[:end])
	if !valid {
		return unknown
	}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "!timestamp_timeout" {
			minutes, found = 0, true
			continue
		}
		parts := strings.SplitN(field, "=", 2)
		if strings.TrimSpace(parts[0]) != "timestamp_timeout" {
			continue
		}
		if len(parts) != 2 {
			return unknown
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > float64(math.MaxInt64)/float64(time.Minute) {
			return unknown
		}
		minutes, found = value, true
	}
	if !found {
		return unknown
	}
	if minutes <= 0 {
		return 0
	}
	interval := time.Duration(minutes * float64(time.Minute) / 2)
	// Sub-millisecond policies cannot support a bounded external-process loop.
	if interval < time.Millisecond {
		return 0
	}
	return interval
}

// inputLogging names the input-logging option a -ll report sets explicitly:
// in the Defaults that match the invoking user, in a Runas or command-specific
// Defaults line, or in a rule's Options. Sudo lists each as comma-separated
// options (display_defaults, display_bound_defaults_by_type and
// display_cmndspec_long in plugins/sudoers). A bound line prints its members
// before its first option with only a blank between them, and a command member
// carries its arguments after blanks too, so an option is the last word of its
// comma field: the first option of a bound line is the last word of the field
// that ends its members. A negated option, another option containing the word
// and an unbalanced list name nothing.
func inputLogging(report []byte) string {
	for _, list := range optionLists(string(report)) {
		fields, valid := defaultFields(list)
		if !valid {
			continue
		}
		for _, field := range fields {
			words := strings.Fields(field)
			if len(words) == 0 {
				continue
			}
			if option := words[len(words)-1]; option == "log_input" || option == "log_stdin" {
				return option
			}
		}
	}
	return ""
}

func optionLists(text string) []string {
	var lists []string
	if start := strings.Index(text, "Matching Defaults entries for "); start >= 0 {
		if colon := strings.Index(text[start:], ":\n"); colon >= 0 {
			block := text[start+colon+2:]
			if end := strings.Index(block, "\n\n"); end >= 0 {
				block = block[:end]
			}
			lists = append(lists, block)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed != line && strings.HasPrefix(trimmed, "Defaults"):
			lists = append(lists, trimmed)
		case strings.HasPrefix(trimmed, "Options:"):
			lists = append(lists, strings.TrimPrefix(trimmed, "Options:"))
		}
	}
	return lists
}

// Commas inside a quoted option value are not Defaults separators.
func defaultFields(text string) ([]string, bool) {
	var fields []string
	start := 0
	quoted, escaped := false, false
	for index, char := range text {
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if char == '"' {
			quoted = !quoted
			continue
		}
		if char == ',' && !quoted {
			fields = append(fields, text[start:index])
			start = index + 1
		}
	}
	if quoted || escaped {
		return nil, false
	}
	return append(fields, text[start:]), true
}

type limitedOutput struct {
	data     []byte
	overflow bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	length := len(p)
	remaining := 64*1024 - len(b.data)
	if length > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return length, nil
}

// A file reaches the child as its own descriptor. Wrapping it would copy the
// stream through this process and turn an inherited terminal into a pipe.
func inheritedDescriptor(writer io.Writer) bool {
	_, ok := writer.(*os.File)
	return ok
}

type synchronizedWriter struct {
	lock   *sync.Mutex
	writer io.Writer
}

func (w synchronizedWriter) Write(data []byte) (int, error) {
	w.lock.Lock()
	defer w.lock.Unlock()
	return w.writer.Write(data)
}
