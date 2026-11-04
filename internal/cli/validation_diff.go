package cli

import "github.com/spf13/cobra"

func validateDiffFlags(command *cobra.Command) string {
	flags := command.Flags()
	if stringValue(flags, "tool") == "" {
		return ""
	}
	switch {
	case stringValue(flags, "output-dir") != "":
		return "--tool conflicts with --output-dir"
	case selectedStructured(command):
		return "--tool requires --output text"
	}
	return ""
}

func (r *Runner) diffToolRefusal(command *cobra.Command, path string) string {
	if path != "diff" || stringValue(command.Flags(), "tool") == "" {
		return ""
	}
	if columns := r.config.Presentation.Columns; columns == nil || columns() <= 0 {
		return "--tool requires a terminal on standard output"
	}
	return ""
}
