package cli

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func structuredFormat(format string) bool { return format == "json" || format == "yaml" }

func selectedStructured(command *cobra.Command) bool {
	return structuredFormat(stringValue(command.Flags(), "output"))
}

// ResolvePresentation parses arguments without acquiring process capabilities.
func ResolvePresentation(args []string, base PresentationOptions) PresentationOptions {
	base = normalizePresentation(base)
	if rawBounds(args) != "" {
		return base
	}
	root, err := newCommandTree(New(Config{}))
	if err != nil {
		return base
	}
	resolved, err := resolveInvocation(root, args)
	base.Command = strings.TrimSpace(strings.TrimPrefix(resolved.command.CommandPath(), "bootwright"))
	if err != nil || resolved.command.ParseFlags(resolved.arguments) != nil {
		base.Format = syntaxFormat(resolved.command, resolved.arguments)
		return base
	}
	return commandPresentation(resolved.command, base)
}

func commandPresentation(command *cobra.Command, options PresentationOptions) PresentationOptions {
	flags := command.Flags()
	options.Command = strings.TrimSpace(strings.TrimPrefix(command.CommandPath(), "bootwright"))
	for _, field := range []struct {
		name  string
		value *string
	}{
		{"output", &options.Format}, {"color", &options.Color}, {"detail", &options.Detail}, {"progress", &options.Progress},
	} {
		if flags.Changed(field.name) {
			*field.value = stringValue(flags, field.name)
		}
	}
	if options.Format == "" {
		options.Format = "text"
	}
	if flags.Changed("ascii") {
		options.ASCII = boolValue(flags, "ascii")
	}
	if flags.Changed("width") {
		options.Width, _ = strconv.Atoi(stringValue(flags, "width"))
	}
	return options
}

func (r *Runner) bindPresentation(options PresentationOptions) {
	if !structuredFormat(options.Format) {
		options.Format = "text"
	}
	r.config.Presentation = options
	r.config.Out = WithPresentation(r.config.Out, options)
	errorOptions := options
	errorOptions.Columns = r.config.ErrorColumns
	r.config.ErrOut = WithPresentation(r.config.ErrOut, errorOptions)
}

// CarryPresentation carries NO_COLOR across elevation without changing original arguments.
func CarryPresentation(args []string, base PresentationOptions) []string {
	carried := append([]string(nil), args...)
	if !base.NoColor || rawBounds(args) != "" {
		return carried
	}
	root, err := newCommandTree(New(Config{}))
	if err != nil {
		return carried
	}
	resolved, err := resolveInvocation(root, args)
	if err != nil || resolved.command.ParseFlags(resolved.arguments) != nil {
		return carried
	}
	command := resolved.command
	path := strings.TrimSpace(strings.TrimPrefix(command.CommandPath(), "bootwright"))
	if validateInvocation(command, path) != "" || commandPresentation(command, normalizePresentation(base)).Color != "auto" {
		return carried
	}
	// Args is the original payload suffix; ArgsLenAtDash distinguishes a separator from a flag value.
	position := len(args) - len(command.Flags().Args())
	if command.Flags().ArgsLenAtDash() >= 0 && position > 0 && args[position-1] == "--" {
		position--
	}
	carried = append(carried, "")
	copy(carried[position+1:], carried[position:])
	carried[position] = "--color=never"
	// A presentation preference must never make an otherwise admitted command
	// fail the elevated child's argument limits.
	if rawBounds(carried) != "" {
		return append([]string(nil), args...)
	}
	return carried
}
