package cli

import (
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/spf13/pflag"
)

// undisclosedAnnotation marks the flag of an undisclosedFlag spec.
const undisclosedAnnotation = "bootwright.undisclosed"

// repeatedCommand is the invocation args name, written as a command an
// operator can run again: its command path and every flag it set. A command
// acting in a context names it with --context first, so a copied command never
// acts in whichever context is current later. An undisclosed value is named by
// a placeholder. It is empty for arguments that do not resolve to one command,
// and for operands, which no confirming command takes.
func repeatedCommand(args []string, contextName string) string {
	if len(args) == 0 || rawBounds(args) != "" {
		return ""
	}
	root, err := newCommandTree(New(Config{}))
	if err != nil {
		return ""
	}
	resolved, err := resolveInvocation(root, args)
	if err != nil || resolved.helpTarget != nil {
		return ""
	}
	command := resolved.command
	flags := command.Flags()
	if command.ParseFlags(resolved.arguments) != nil || len(flags.Args()) != 0 {
		return ""
	}
	words := strings.Fields(command.CommandPath())
	if contextName != "" {
		words = append(words, "--context", shellWord(contextName))
	}
	flags.Visit(func(flag *pflag.Flag) {
		name := "--" + flag.Name
		switch {
		case flag.Name == "yes" || flag.Name == "context" && contextName != "":
		case len(flag.Annotations[undisclosedAnnotation]) != 0:
			words = append(words, name, "<"+flag.Name+">")
		case flag.Value.Type() == "bool":
			if flag.Value.String() == "true" {
				words = append(words, name)
			}
		case flag.Value.Type() == "stringArray":
			if values, ok := flag.Value.(pflag.SliceValue); ok {
				for _, value := range values.GetSlice() {
					words = append(words, name, repeatedValue(flag.Name, value))
				}
			}
		default:
			words = append(words, name, shellWord(flag.Value.String()))
		}
	})
	return strings.Join(words, " ")
}

// repeatedValue binds a bare data-loss in a repeated command, which carries
// --yes and so refuses a bare token (Q117); the unquoted placeholder is no shell word.
func repeatedValue(flag, value string) string {
	if flag != "authorize" {
		return shellWord(value)
	}
	members := strings.Split(value, ",")
	bound := false
	for index, member := range members {
		members[index] = strings.TrimSpace(member)
		if members[index] == "data-loss" {
			members[index], bound = "data-loss="+lifecycle.DigestPlaceholder, true
		}
	}
	if !bound {
		return shellWord(value)
	}
	return strings.Join(members, ",")
}

// shellWord quotes one argument for a POSIX shell only when it needs it, so an
// ordinary command reads as typed and every other one still runs as given.
func shellWord(value string) string {
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
