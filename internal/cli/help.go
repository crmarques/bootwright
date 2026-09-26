package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

// controllerContextUsage describes --context for the two commands that never
// fall back to the current context.
func controllerContextUsage(path string) (string, bool) {
	switch path {
	case "setup":
		return "Ignored: setup selects no context", true
	case "preflight controller":
		return "Select explicit Environment requirements (default: baseline)", true
	}
	return "", false
}

func writeHelp(w io.Writer, command *cobra.Command) error {
	if usage, ok := controllerContextUsage(command.Annotations["bootwright.command"]); ok {
		if flag := command.InheritedFlags().Lookup("context"); flag != nil {
			previous := flag.Usage
			flag.Usage = usage
			defer func() { flag.Usage = previous }()
		}
	}
	var text strings.Builder
	description := command.Long
	if description == "" {
		description = command.Short
	}
	fmt.Fprintln(&text, description)
	fmt.Fprintln(&text)
	text.WriteString(command.UsageString())
	_, err := io.WriteString(w, text.String())
	return err
}

func writeConciseHelp(w io.Writer, command *cobra.Command) error {
	_, err := fmt.Fprintf(w, "Usage: %s\nRun 'bootwright help' for available commands.\n", command.UseLine())
	return err
}
