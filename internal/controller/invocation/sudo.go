package invocation

import (
	"context"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Command is one bounded invocation of an already selected executable.
type Command struct {
	Executable    string
	Arguments     []string
	Environment   []string
	Input         io.Reader
	Output, Error io.Writer
}

type Executor interface {
	Run(context.Context, Command) (int, error)
}

type Delay interface {
	Wait(context.Context, time.Duration) error
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
	Input            io.Reader
	Output, Error    io.Writer
}

type Supervisor struct{ options SudoOptions }

func NewSupervisor(options SudoOptions) *Supervisor { return &Supervisor{options: options} }

// Run keeps all sudo children attached to the original nonroot parent. Refresh
// failures never invalidate or terminate an operation that is already elevated.
func (s *Supervisor) Run(ctx context.Context, args []string) (int, error) {
	options := s.options
	if options.Executor == nil || options.Delay == nil || options.Executable == "" || options.Sudo == "" {
		return 1, errors.New("sudo invocation boundary is not configured")
	}
	if err := ctx.Err(); err != nil {
		return 1, err
	}
	var outputLock sync.Mutex
	if options.Output != nil {
		options.Output = synchronizedWriter{lock: &outputLock, writer: options.Output}
	}
	if options.Error != nil {
		options.Error = synchronizedWriter{lock: &outputLock, writer: options.Error}
	}
	environment := []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	var policy limitedOutput
	probe, cancelProbe := context.WithTimeout(ctx, 2*time.Second)
	code, probeErr := options.Executor.Run(probe, Command{Executable: options.Sudo, Arguments: []string{"-n", "-u", "#0", "-ll"}, Environment: append([]string(nil), environment...), Output: &policy, Error: io.Discard})
	cancelProbe()
	if err := ctx.Err(); err != nil {
		return 1, err
	}
	interval := 30 * time.Second
	if probeErr == nil && code == 0 && !policy.overflow {
		interval = refreshInterval(policy.data)
	}
	childArgs := []string{"-u", "#0"}
	if options.NonInteractive {
		childArgs = append(childArgs, "-n")
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
				if refreshCtx.Err() == nil && options.Error != nil {
					// This warning contains no policy contents or captured sudo text.
					io.WriteString(options.Error, "[WARN] sudo credential refresh stopped; the active operation continues.\n")
				}
				return
			}
		}
	}()
	code, err := options.Executor.Run(ctx, Command{Executable: options.Sudo, Arguments: childArgs, Environment: environment, Input: options.Input, Output: options.Output, Error: options.Error})
	stopRefresh()
	<-finished
	return ExitCode(ctx, code), err
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

type synchronizedWriter struct {
	lock   *sync.Mutex
	writer io.Writer
}

func (w synchronizedWriter) Write(data []byte) (int, error) {
	w.lock.Lock()
	defer w.lock.Unlock()
	return w.writer.Write(data)
}
