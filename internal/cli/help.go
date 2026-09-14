package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

func writeHelp(w io.Writer, command *cobra.Command) error {
	if path := command.Annotations["bootwright.command"]; path == "setup" || path == "preflight controller" {
		if flag := command.InheritedFlags().Lookup("context"); flag != nil {
			previous := flag.Usage
			flag.Usage = "Select explicit Environment requirements (default: baseline)"
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
