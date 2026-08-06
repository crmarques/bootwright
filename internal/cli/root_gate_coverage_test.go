package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

var gateRootlessCommands = map[string]bool{
	"version":        true,
	"example init":   true,
	"context init":   true,
	"context update": true,
	"context delete": true,
	"secret set":     true,
}

func TestLocalRootGateClassifiesEveryRunnableCommand(t *testing.T) {
	root := newRootCmd(strings.NewReader(""), io.Discard, io.Discard)
	for _, cmd := range runnableCommands(root) {
		path := strings.TrimPrefix(cmd.CommandPath(), root.Name()+" ")
		args := strings.Fields(path)
		if cmd.Flags().Lookup("name") != nil {
			args = append(args, "--name", "probe")
		}
		want := !gateRootlessCommands[path]
		if got := argsNeedLocalRoot(args); got != want {
			t.Errorf("argsNeedLocalRoot(%v) = %v, want %v", args, got, want)
		}
	}
}

func runnableCommands(cmd *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	if cmd.Runnable() && cmd.HasParent() && !cmd.HasSubCommands() {
		out = append(out, cmd)
	}
	for _, child := range cmd.Commands() {
		out = append(out, runnableCommands(child)...)
	}
	return out
}
