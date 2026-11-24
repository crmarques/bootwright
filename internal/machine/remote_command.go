package machine

import (
	"errors"
	"fmt"
	"strings"
)

const (
	RemoteShell = "/bin/sh"
	RemoteSudo  = "/usr/bin/sudo"
)

type ShellBinding struct {
	Name  string
	Value string
}

type RemoteCommand struct {
	Words    []string
	Shell    bool
	Sudo     bool
	Bindings []ShellBinding
}

func (c RemoteCommand) Vector() ([]string, error) {
	if len(c.Bindings) > 0 && !c.Shell {
		return nil, errors.New("--var requires --shell: only a script reads variables")
	}
	words := c.Words
	if c.Shell {
		script := strings.Join(c.Words, " ")
		if strings.TrimSpace(script) == "" {
			return nil, errors.New("--shell needs a non-empty script")
		}
		preamble, err := exportBindings(c.Bindings)
		if err != nil {
			return nil, err
		}
		words = []string{RemoteShell, "-c", "--", preamble + script}
	}
	if c.Sudo && len(words) > 0 {
		words = append([]string{RemoteSudo, "-n", "--"}, words...)
	}
	return words, nil
}

func exportBindings(bindings []ShellBinding) (string, error) {
	if len(bindings) == 0 {
		return "", nil
	}
	seen := make(map[string]bool, len(bindings))
	assignments := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		if !ShellName(binding.Name) {
			return "", fmt.Errorf("--var name %q is not a shell variable name: use letters, digits and underscores, not starting with a digit", binding.Name)
		}
		if seen[binding.Name] {
			return "", fmt.Errorf("--var name %q is set twice", binding.Name)
		}
		seen[binding.Name] = true
		assignments = append(assignments, binding.Name+"="+ShellWord(binding.Value))
	}
	return "export " + strings.Join(assignments, " ") + "; ", nil
}

func ShellName(name string) bool {
	if name == "" || name[0] >= '0' && name[0] <= '9' {
		return false
	}
	return !strings.ContainsFunc(name, func(r rune) bool {
		return r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	})
}

// ShellLine quotes a leading word holding = too: unquoted, a shell assigns it.
func ShellLine(words []string) []string {
	line := make([]string, 0, len(words))
	for _, word := range words {
		if len(line) == 0 && strings.ContainsRune(word, '=') {
			line = append(line, quotedWord(word))
			continue
		}
		line = append(line, ShellWord(word))
	}
	return line
}

// ShellWord quotes a word only when a POSIX shell would otherwise change it.
func ShellWord(value string) string {
	if value != "" && !strings.ContainsFunc(value, func(r rune) bool { return !shellSafe(r) }) {
		return value
	}
	return quotedWord(value)
}

func quotedWord(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func shellSafe(r rune) bool {
	if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
		return true
	}
	return strings.ContainsRune("-_=+:,./@%", r)
}
